package nodemgr

import "sync/atomic"

// Node represents a registry mirror or proxy node.
// Runtime state uses atomic operations for concurrent access.
type Node struct {
	URL         string   `json:"url"`
	DisplayName string   `json:"display_name"`
	Enabled     bool     `json:"enabled"`
	Targets     []string `json:"targets"`
	Token       string   `json:"token,omitempty"`

	// Runtime scoring state (not persisted to nodes table, sourced from metrics)
	LatencyMs int64 `json:"latency_ms"` // last measured latency in ms (atomic)

	// Transient (not persisted)
	InFlight int32 `json:"inflight"` // current active downloads (atomic)
	Score    int32 `json:"score"`    // computed score × 10000 (atomic)
	Healthy  bool  `json:"healthy"`  // current health status
}

func (n *Node) IsIdle() bool             { return atomic.LoadInt32(&n.InFlight) == 0 }
func (n *Node) IncrInflight()             { atomic.AddInt32(&n.InFlight, 1) }
func (n *Node) DecrInflight()             { atomic.AddInt32(&n.InFlight, -1) }

func (n *Node) RecordSuccess(latencyMs int64) {
	atomic.StoreInt64(&n.LatencyMs, latencyMs)
	n.Healthy = true
}

func (n *Node) RecordFailure(latencyMs int64) {
	if latencyMs > 0 {
		atomic.StoreInt64(&n.LatencyMs, latencyMs)
	}
	n.Healthy = false
}
