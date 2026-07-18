package nodemgr

// HealthChecker 健康检查器
type HealthChecker struct {
	probeInterval int // 探活间隔（秒）
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(intervalSec int) *HealthChecker {
	if intervalSec <= 0 {
		intervalSec = 30
	}
	return &HealthChecker{probeInterval: intervalSec}
}

// Probe 探活单个节点
func (h *HealthChecker) Probe(node *Node) bool {
	// 实现：尝试 HEAD 请求到节点
	// 阶段二实现
	return true
}
