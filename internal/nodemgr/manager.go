package nodemgr

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/pullfusion/pullfusion/internal/config"
)

// Manager manages registry mirror nodes with smart selection.
type Manager struct {
	mu     sync.RWMutex
	nodes  []*Node
	scorer *Scorer
}

// NewManager creates a node manager with the default scoring weights.
func NewManager(cfg *config.Config) *Manager {
	m := &Manager{
		scorer: NewScorer(DefaultWeights()),
	}
	m.initNodes(cfg)
	slog.Info("node manager initialized", "nodes", len(m.nodes))
	return m
}

// List returns a copy of all nodes.
func (m *Manager) List() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Node, len(m.nodes))
	copy(result, m.nodes)
	return result
}

// AddNode adds a new node (for externally fetched nodes, e.g. status.anye.xyz).
func (m *Manager) AddNode(node *Node) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.nodes {
		if n.URL == node.URL {
			return // duplicate
		}
	}
	node.Healthy = true // new nodes start healthy
	m.nodes = append(m.nodes, node)
}

// SelectBest returns the best available node for the given registry.
// Uses the scorer's idle-priority algorithm.
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
		slog.Info("node selected", "name", selected.DisplayName, "url", selected.URL[:min(len(selected.URL), 50)],
			"score", selected.Score, "inflight", selected.InFlight, "latency_ms", selected.LatencyMs)
	}
	return selected
}

// ReleaseNode decrements a node's in-flight count. Called after download completes.
func (m *Manager) ReleaseNode(node *Node, success bool, latencyMs, speedKBps int64) {
	if node == nil {
		return
	}
	node.DecrInflight()
	if success {
		node.RecordSuccess(latencyMs, speedKBps)
		slog.Info("node success", "name", node.DisplayName, "latency_ms", latencyMs, "speed_kbps", speedKBps,
			"inflight", node.InFlight, "score", node.Score)
	} else {
		unhealthy := node.RecordFailure()
		slog.Warn("node failure", "name", node.DisplayName, "fail_count", node.FailCount,
			"unhealthy", unhealthy, "latency_ms", latencyMs)
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

// ReloadNodes reloads nodes from config (hot-reload).
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

// GetNodesForScoring returns all nodes with computed scores (for admin API).
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
