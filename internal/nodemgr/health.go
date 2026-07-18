package nodemgr

import (
	"context"
	"time"
)

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

// StartProbeLoop 启动后台健康探测循环
func (h *HealthChecker) StartProbeLoop(ctx context.Context, mgr *Manager) {
	ticker := time.NewTicker(time.Duration(h.probeInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodes := mgr.List()
			for _, node := range nodes {
				healthy := h.Probe(node)
				node.mu.Lock()
				if healthy {
					node.FailCount = 0
					node.Healthy = true
				} else {
					node.FailCount++
					if node.FailCount >= 3 {
						node.Healthy = false
					}
				}
				node.LastCheck = time.Now()
				node.mu.Unlock()
			}
		}
	}
}
