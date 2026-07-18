package nodemgr

import (
	"sync"
	"sync/atomic"
	"time"
)

// NodeType 节点类型
type NodeType string

const (
	NodeTypeMirror NodeType = "mirror"
	NodeTypeSocks5 NodeType = "socks5"
	NodeTypeHTTP   NodeType = "http"
)

// Node 节点定义
type Node struct {
	URL         string
	DisplayName string
	Type        NodeType
	Priority    int
	Enabled     bool
	Targets     []string

	Speed     float64
	FailCount int
	InFlight  int32 // atomic
	LastCheck time.Time
	Healthy   bool

	mu sync.Mutex
}

// Manager 节点管理器
type Manager struct {
	nodes    []*Node
	mu       sync.RWMutex
	cfg      interface{} // *config.Config, 避免循环依赖
	balancer *Balancer
}

// NewManager 创建节点管理器
func NewManager(cfg interface{}) *Manager {
	m := &Manager{
		cfg:      cfg,
		balancer: NewDefaultBalancer(),
		nodes:    make([]*Node, 0),
	}
	m.initNodes(cfg)
	return m
}

// initNodes 从配置初始化节点列表
func (m *Manager) initNodes(cfg interface{}) {
	// 节点初始化由外部调用 ReloadNodes 完成
	m.ReloadNodes(cfg)
}

// ReloadNodes 重新加载节点配置
func (m *Manager) ReloadNodes(cfg interface{}) {
	// 这里需要通过类型断言获取 Config
	// 由于避免循环依赖，使用 interface{}
	m.mu.Lock()
	defer m.mu.Unlock()

	// 实现将在后续完善，暂时保留为空
	// 实际应该从 config.Config 中提取 mirror 和 proxy 节点
}

// List 返回所有节点
func (m *Manager) List() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Node, len(m.nodes))
	copy(result, m.nodes)
	return result
}

// SelectForBlob 为 blob 下载选择 k 个最佳节点
func (m *Manager) SelectForBlob(registry string, expectedSize int64, k int) []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var candidates []*Node
	for _, n := range m.nodes {
		if !n.Enabled || !n.Healthy {
			continue
		}
		// 检查 targets 是否匹配
		targetMatch := len(n.Targets) == 0
		for _, t := range n.Targets {
			if t == registry {
				targetMatch = true
				break
			}
		}
		if !targetMatch {
			continue
		}
		candidates = append(candidates, n)
	}

	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) <= k {
		return candidates
	}

	return m.balancer.TopK(candidates, k)
}

// MarkFailed 标记节点失败
func (m *Manager) MarkFailed(node *Node) {
	node.mu.Lock()
	defer node.mu.Unlock()
	node.FailCount++
	if node.FailCount >= 3 {
		node.Healthy = false
	}
}

// MarkSuccess 标记节点成功
func (m *Manager) MarkSuccess(node *Node) {
	node.mu.Lock()
	defer node.mu.Unlock()
	node.FailCount = 0
	node.Healthy = true
}

// IncrInflight 增加并发计数
func (m *Manager) IncrInflight(node *Node) {
	atomic.AddInt32(&node.InFlight, 1)
}

// DecrInflight 减少并发计数
func (m *Manager) DecrInflight(node *Node) {
	atomic.AddInt32(&node.InFlight, -1)
}

// StartSpeedTest 启动后台测速
func (m *Manager) StartSpeedTest(ctx interface{}) {
	// 后台测速实现将在阶段三完成
}

// GetHealthStatus 获取健康状态统计
func (m *Manager) GetHealthStatus() (total, healthy int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		total++
		if n.Healthy {
			healthy++
		}
	}
	return
}
