package nodemgr

import (
	"sort"
	"time"
)

// Balancer 负载均衡器
type Balancer struct {
	weights BalanceWeights
}

// BalanceWeights 评分权重
type BalanceWeights struct {
	Priority float64
	Speed    float64
	Health   float64
	Load     float64
}

// NewDefaultBalancer 创建默认权重均衡器
func NewDefaultBalancer() *Balancer {
	return &Balancer{
		weights: BalanceWeights{
			Priority: 0.3,
			Speed:    0.4,
			Health:   0.2,
			Load:     0.1,
		},
	}
}

// Score 计算节点评分
func (b *Balancer) Score(node *Node) float64 {
	node.mu.Lock()
	defer node.mu.Unlock()

	priorityScore := 1.0 / float64(max(node.Priority, 1))
	speedScore := normalizeSpeed(node.Speed)
	healthScore := 1.0 / (1.0 + float64(node.FailCount))
	loadScore := 1.0 / (1.0 + float64(node.InFlight))

	return b.weights.Priority*priorityScore +
		b.weights.Speed*speedScore +
		b.weights.Health*healthScore +
		b.weights.Load*loadScore
}

// TopK 选出得分最高的 k 个节点
func (b *Balancer) TopK(nodes []*Node, k int) []*Node {
	type scored struct {
		node  *Node
		score float64
	}

	scoredList := make([]scored, len(nodes))
	for i, n := range nodes {
		scoredList[i] = scored{node: n, score: b.Score(n)}
	}

	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	if k > len(scoredList) {
		k = len(scoredList)
	}

	result := make([]*Node, k)
	for i := 0; i < k; i++ {
		result[i] = scoredList[i].node
	}
	return result
}

// normalizeSpeed 归一化速度
func normalizeSpeed(speed float64) float64 {
	if speed <= 0 {
		return 0
	}
	// 假设 100 MB/s 为满分
	maxSpeed := 100.0 * 1024 // 100 MB/s in KB/s
	if speed > maxSpeed {
		return 1.0
	}
	return speed / maxSpeed
}

// Node 统计快照（用于管理 API）
type NodeSnapshot struct {
	URL         string    `json:"url"`
	DisplayName string    `json:"display_name"`
	Type        NodeType  `json:"type"`
	Priority    int       `json:"priority"`
	Enabled     bool      `json:"enabled"`
	Speed       float64   `json:"speed_mbps"`
	Healthy     bool      `json:"healthy"`
	FailCount   int       `json:"fail_count"`
	InFlight    int32     `json:"in_flight"`
	LastCheck   time.Time `json:"last_check"`
}

// Snapshot 生成节点快照
func Snapshot(node *Node) NodeSnapshot {
	node.mu.Lock()
	defer node.mu.Unlock()
	return NodeSnapshot{
		URL:         node.URL,
		DisplayName: node.DisplayName,
		Type:        node.Type,
		Priority:    node.Priority,
		Enabled:     node.Enabled,
		Speed:       node.Speed,
		Healthy:     node.Healthy,
		FailCount:   node.FailCount,
		InFlight:    atomicInt32(node.InFlight),
		LastCheck:   node.LastCheck,
	}
}

// 辅助函数
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
