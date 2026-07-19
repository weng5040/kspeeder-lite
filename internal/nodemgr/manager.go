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

// Manager manages registry mirror nodes with SQLite-backed persistence.
type Manager struct {
	mu     sync.RWMutex
	nodes  []*Node
	scorer *Scorer
	db     *store.DB
}

// NewManager creates a node manager with optional SQLite backing.
func NewManager(cfg *config.Config) *Manager {
	return NewManagerWithStore(cfg, nil)
}

// NewManagerWithStore creates a node manager with SQLite persistence.
func NewManagerWithStore(cfg *config.Config, database *store.DB) *Manager {
	m := &Manager{
		scorer: NewScorer(DefaultWeights()),
		db:     database,
	}
	m.initNodes(cfg)
	if database != nil {
		m.loadFromDB()
		m.warmupFromMetrics()
		m.saveToDB() // ensure new config nodes are in DB
	}
	slog.Info("node manager initialized", "nodes", len(m.nodes))
	return m
}

// loadFromDB loads node state from SQLite, merging with config nodes.
func (m *Manager) loadFromDB() {
	dbNodes, err := m.db.LoadNodes()
	if err != nil {
		slog.Warn("failed to load nodes from db", "error", err)
		return
	}
	if len(dbNodes) == 0 {
		return
	}

	// Build index of existing config nodes
	existing := make(map[string]*Node)
	for _, n := range m.nodes {
		existing[n.URL] = n
	}

	for _, dn := range dbNodes {
		if n, ok := existing[dn.URL]; ok {
			// Restore persisted state onto config node
			n.FailCount = dn.FailCount
			n.SuccessCount = dn.SuccessCnt
			n.LatencyMs = dn.LatencyMs
			n.SpeedKBps = dn.SpeedKBps
		} else {
			// Add DB-only nodes (e.g. fetched nodes)
			n := &Node{
				URL:         dn.URL,
				DisplayName: dn.DisplayName,
				Type:        NodeType(dn.Type),
				Priority:    dn.Priority,
				Enabled:     dn.Enabled,
				Healthy:     dn.FailCount < 5,
				Targets:     dn.Targets,
				Token:       dn.Token,
				FailCount:   dn.FailCount,
				SuccessCount: dn.SuccessCnt,
				LatencyMs:   dn.LatencyMs,
				SpeedKBps:   dn.SpeedKBps,
			}
			m.nodes = append(m.nodes, n)
		}
	}
	slog.Info("loaded node state from database", "db_nodes", len(dbNodes))
}

// warmupFromMetrics restores scoring metrics from the time-series table.
func (m *Manager) warmupFromMetrics() {
	latest, err := m.db.LoadLatestMetrics()
	if err != nil {
		slog.Warn("warmup metrics failed", "error", err)
		return
	}
	for _, n := range m.nodes {
		r, ok := latest[n.URL]
		if !ok {
			continue
		}
		atomic.StoreInt32(&n.FailCount, r.FailCount)
		atomic.StoreInt64(&n.LatencyMs, r.LatencyMs)
		atomic.StoreInt64(&n.SpeedKBps, r.SpeedKBps)
		n.Healthy = r.Healthy
		m.scorer.Score(n)
	}
	if len(latest) > 0 {
		slog.Info("warmup: restored metrics", "nodes", len(latest))
	}
}

// saveToDB persists current node state to SQLite.
func (m *Manager) saveToDB() {
	var records []store.NodeRecord
	for _, n := range m.List() {
		records = append(records, store.NodeRecord{
			URL:         n.URL,
			DisplayName: n.DisplayName,
			Type:        string(n.Type),
			Priority:    n.Priority,
			Enabled:     n.Enabled,
			Targets:     n.Targets,
			Token:       n.Token,
			FailCount:   atomic.LoadInt32(&n.FailCount),
			SuccessCnt:  atomic.LoadInt64(&n.SuccessCount),
			LatencyMs:   atomic.LoadInt64(&n.LatencyMs),
			SpeedKBps:   atomic.LoadInt64(&n.SpeedKBps),
		})
	}
	if err := m.db.SaveNodes(records); err != nil {
		slog.Warn("failed to save nodes to db", "error", err)
	} else {
		slog.Info("nodes saved to database", "count", len(records))
	}
}

// List returns a copy of all nodes.
func (m *Manager) List() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Node, len(m.nodes))
	copy(result, m.nodes)
	return result
}

// AddNode adds a new node (deduplicated by URL).
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

// PersistNodes triggers an immediate save to SQLite.
func (m *Manager) PersistNodes() {
	if m.db != nil {
		m.saveToDB()
	}
}

// SelectBest returns the best idle node for the given registry.
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

// ReleaseNode decrements in-flight count and records metrics.
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

	// Persist to SQLite
	if m.db != nil {
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
		if err := m.db.InsertMetric(r); err != nil {
			slog.Warn("metric insert failed", "node", node.DisplayName, "error", err)
		}

		// Periodic save of node state (throttled)
		m.saveToDB()
	}

	if success {
		slog.Info("node success",
			"name", node.DisplayName, "latency_ms", latencyMs,
			"speed_kbps", speedKBps, "score", node.Score, "inflight", node.InFlight,
		)
	} else {
		slog.Warn("node failure",
			"name", node.DisplayName, "fail_count", node.FailCount,
			"healthy", node.Healthy, "score", node.Score,
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

// GetStats returns multi-period aggregated statistics.
func (m *Manager) GetStats() (map[string][]store.AggregatedStats, error) {
	if m.db == nil {
		return nil, fmt.Errorf("no database configured")
	}
	return m.db.GetStats()
}

// GetStatsForPeriod returns aggregated stats for one period.
func (m *Manager) GetStatsForPeriod(period string) ([]store.AggregatedStats, error) {
	if m.db == nil {
		return nil, fmt.Errorf("no database configured")
	}
	return m.db.GetStatsForPeriod(period)
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
		m.nodes = append(m.nodes, &Node{
			URL: mirror.URL, DisplayName: mirror.DisplayName, Type: NodeTypeMirror,
			Priority: mirror.Priority, Enabled: true, Healthy: true,
			Targets: []string{"dockerhub"}, Token: mirror.Token,
		})
	}
	for _, mirror := range cfg.Mirrors.Ghcr {
		m.nodes = append(m.nodes, &Node{
			URL: mirror.URL, DisplayName: mirror.DisplayName, Type: NodeTypeMirror,
			Priority: mirror.Priority, Enabled: true, Healthy: true,
			Targets: []string{"ghcr"}, Token: mirror.Token,
		})
	}
	slog.Info(fmt.Sprintf("loaded %d nodes from config", len(m.nodes)))
}
