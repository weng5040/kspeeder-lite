package nodemgr

import "sync/atomic"

// NodeType defines the type of a node.
type NodeType string

const (
	NodeTypeMirror NodeType = "mirror"
	NodeTypeSocks5 NodeType = "socks5"
	NodeTypeHTTP   NodeType = "http"
)

// Node represents a registry mirror or proxy node.
// All state fields are safe for concurrent access: Score/Health use atomic,
// Latency/Fail/Success use mutex, InFlight uses atomic int32.
type Node struct {
	URL         string   `json:"url"`
	DisplayName string   `json:"display_name"`
	Type        NodeType `json:"type"`
	Priority    int      `json:"priority"`
	Enabled     bool     `json:"enabled"`
	Targets     []string `json:"targets"`
	Token       string   `json:"token,omitempty"`

	// Runtime state — persisted across restarts for historical tracking
	FailCount    int32 `json:"fail_count"`    // total consecutive failures (reset on success)
	SuccessCount int64 `json:"success_count"` // total successful downloads
	LatencyMs    int64 `json:"latency_ms"`    // last measured latency in ms (atomic)
	SpeedKBps    int64 `json:"speed_kbps"`    // last measured speed in KB/s (atomic)

	// Transient runtime state — NOT persisted
	InFlight int32 `json:"inflight"` // current active downloads (atomic)
	Score    int32 `json:"score"` // computed score × 10000 (atomic, for sorting)
	Healthy  bool  `json:"healthy"` // current health status
}

// IsIdle returns true if the node has no active connections.
func (n *Node) IsIdle() bool {
	return atomic.LoadInt32(&n.InFlight) == 0
}

// IncrInflight atomically increments the in-flight count.
func (n *Node) IncrInflight() {
	atomic.AddInt32(&n.InFlight, 1)
}

// DecrInflight atomically decrements the in-flight count.
func (n *Node) DecrInflight() {
	atomic.AddInt32(&n.InFlight, -1)
}

// RecordSuccess records a successful download with latency and speed.
func (n *Node) RecordSuccess(latencyMs, speedKBps int64) {
	atomic.StoreInt32(&n.FailCount, 0) // reset failure streak
	atomic.AddInt64(&n.SuccessCount, 1)
	atomic.StoreInt64(&n.LatencyMs, latencyMs)
	if speedKBps > 0 {
		atomic.StoreInt64(&n.SpeedKBps, speedKBps)
	}
	n.Healthy = true
}

// RecordFailure records a failed attempt. Returns true if node should be marked unhealthy.
func (n *Node) RecordFailure() bool {
	c := atomic.AddInt32(&n.FailCount, 1)
	if c >= 5 {
		n.Healthy = false
		return true
	}
	return false
}
