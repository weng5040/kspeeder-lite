package nodemgr

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pullfusion/pullfusion/internal/config"
	"github.com/pullfusion/pullfusion/internal/store"
)

// Manager manages registry mirror nodes with smart selection and metric persistence.
type Manager struct {
	mu     sync.RWMutex
	nodes  []*Node
	scorer *Scorer
	store  *store.MetricStore
}

// NewManager creates a node manager, loading nodes from config and warming up metrics.
func NewManager(cfg *config.Config) *Manager {
	return NewManagerWithStore(cfg, nil)
}

// NewManagerWithStore creates a node manager with optional metric persistence.
func NewManagerWithStore(cfg *config.Config, ms *store.MetricStore) *Manager {
	m := &Manager{
		scorer: NewScorer(DefaultWeights()),
		store:  ms,
	}
	m.initNodes(cfg)
	if ms != nil {
		m.warmupFromStore()
	}
	slog.Info("node manager initialized", "nodes", len(m.nodes))
	return m
}

// warmupFromStore loads the latest metrics from SQLite and applies them to nodes.
func (m *Manager) warmupFromStore() {
	if m.store == nil {
		return
	}
	latest, err := m.store.LoadLatest()
	if err != nil {
		slog.Warn("warmup: failed to load metrics", "error", err)
		return
	}
	if len(latest) == 0 {
		slog.Info("warmup: no historical metrics found")
		return
	}

	for _, n := range m.nodes {
		r, ok := latest[n.URL]
		if !ok {
			continue
		}
		atomic.StoreInt32(&n.FailCount, r.FailCount)
		atomic.StoreInt64(&n.SuccessCount, atomic.LoadInt64(&n.SuccessCount)) // keep 0 since we only have latest
		atomic.StoreInt64(&n.LatencyMs, r.LatencyMs)
		atomic.StoreInt64(&n.SpeedKBps, r.SpeedKBps)
		n.Healthy = r.Healthy
		m.scorer.Score(n)
	}
	slog.Info("warmup: restored metrics from store", "nodes", len(latest))
}

// List returns a copy of all nodes.
func (m *Manager) List() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Node, len(m.nodes))
	copy(result, m.nodes)
	return result
}

// AddNode adds a new node (for externally fetched nodes).
func (m *Manager) AddNode(node *Node) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.nodes {
		if n.URL == node.URL {
			return
		}
	}
	node.Healthy = true
	m.nodes = append(m.nodes, node)
}

// SelectBest returns the best available node for the given registry.
func (m *Manager) SelectBest(registry string) *Node {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var candidates []*Node
	for _, n := range m.nodes {
		if !n.Enabled || !n.Healthy {
			continue
		}
		targetMatch := len(n.Targets) == 0
		for _, t := range n.Targets {
			if t == registry {
				targetMatch = true
				break
			}
		}
		if targetMatch {
			candidates = append(candidates, n)
		}
	}

	selected := m.scorer.SelectBest(candidates)
	if selected != nil {
		selected.IncrInflight()
		slog.Info("node selected",
			"name", selected.DisplayName,
			"score", selected.Score,
			"inflight", selected.InFlight,
			"latency_ms", selected.LatencyMs,
			"fail_count", selected.FailCount,
		)
	}
	return selected
}

// ReleaseNode decrements a node's in-flight count and records metrics.
func (m *Manager) ReleaseNode(node *Node, success bool, latencyMs, speedKBps int64) {
	if node == nil {
		return
	}
	node.DecrInflight()

	if success {
		node.RecordSuccess(latencyMs, speedKBps)
	} else {
		node.RecordFailure()
	}
	m.scorer.Score(node)

	// Persist metric to SQLite
	if m.store != nil {
		r := store.MetricRecord{
			NodeURL:   node.URL,
			Timestamp: time.Now(),
			LatencyMs: latencyMs,
			SpeedKBps: speedKBps,
			Success:   success,
			FailCount: atomic.LoadInt32(&node.FailCount),
			Score:     atomic.LoadInt32(&node.Score),
			InFlight:  atomic.LoadInt32(&node.InFlight),
			Healthy:   node.Healthy,
		}
		if err := m.store.Insert(r); err != nil {
			slog.Warn("failed to persist metric", "node", node.DisplayName, "error", err)
		}
	}

	if success {
		slog.Info("node success",
			"name", node.DisplayName,
			"latency_ms", latencyMs,
			"speed_kbps", speedKBps,
			"score", node.Score,
			"inflight", node.InFlight,
		)
	} else {
		slog.Warn("node failure",
			"name", node.DisplayName,
			"fail_count", node.FailCount,
			"healthy", node.Healthy,
			"score", node.Score,
		)
	}
}

// GetHealthStatus returns total and healthy node counts.
func (m *Manager) GetHealthStatus() (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := len(m.nodes)
	healthy := 0
	for _, n := range m.nodes {
		if n.Healthy {
			healthy++
		}
	}
	return total, healthy
}

// Get7DayStats returns 7-day aggregated node statistics from the metric store.
func (m *Manager) Get7DayStats() (map[string]store.NodeStats, error) {
	if m.store == nil {
		return nil, fmt.Errorf("no metric store configured")
	}
	return m.store.Stats7Day()
}

// ReloadNodes reloads nodes from config.
func (m *Manager) ReloadNodes(rawCfg interface{}) {
	cfg, ok := rawCfg.(*config.Config)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes = nil
	m.initNodes(cfg)
	slog.Info("nodes reloaded", "count", len(m.nodes))
}

// GetScoredNodes returns all nodes with computed scores.
func (m *Manager) GetScoredNodes() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		m.scorer.Score(n)
	}
	result := make([]*Node, len(m.nodes))
	copy(result, m.nodes)
	return result
}

func (m *Manager) initNodes(cfg *config.Config) {
	for _, mirror := range cfg.Mirrors.Dockerhub {
		n := &Node{
			URL:         mirror.URL,
			DisplayName: mirror.DisplayName,
			Type:        NodeTypeMirror,
			Priority:    mirror.Priority,
			Enabled:     true,
			Healthy:     true,
			Targets:     []string{"dockerhub"},
			Token:       mirror.Token,
		}
		m.nodes = append(m.nodes, n)
	}
	for _, mirror := range cfg.Mirrors.Ghcr {
		n := &Node{
			URL:         mirror.URL,
			DisplayName: mirror.DisplayName,
			Type:        NodeTypeMirror,
			Priority:    mirror.Priority,
			Enabled:     true,
			Healthy:     true,
			Targets:     []string{"ghcr"},
			Token:       mirror.Token,
		}
		m.nodes = append(m.nodes, n)
	}
	slog.Info(fmt.Sprintf("loaded %d nodes from config", len(m.nodes)))
}
