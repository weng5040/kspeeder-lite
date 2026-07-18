package nodemgr

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// SpeedTester 测速器
type SpeedTester struct {
	testURL      string
	testInterval time.Duration
	windowSize   int
	mu           sync.Mutex
	histories    map[string]*nodeSpeedHistory
}

// SpeedResult 单次测速结果
type SpeedResult struct {
	Timestamp time.Time
	Speed     float64 // KB/s
}

// nodeSpeedHistory 每个节点的测速历史（滑动窗口）
type nodeSpeedHistory struct {
	mu      sync.Mutex
	results []SpeedResult
}

// NewSpeedTester 创建测速器
func NewSpeedTester(testURL string, intervalSec int) *SpeedTester {
	if intervalSec <= 0 {
		intervalSec = 300
	}
	return &SpeedTester{
		testURL:      testURL,
		testInterval: time.Duration(intervalSec) * time.Second,
		windowSize:   5, // 滑动窗口保留最近 5 次结果
		histories:    make(map[string]*nodeSpeedHistory),
	}
}

// Start 启动逐节点定时测速
func (s *SpeedTester) Start(ctx context.Context, nodes []*Node) {
	if s.testURL == "" {
		slog.Warn("speed test disabled, no test URL configured")
		return
	}

	s.mu.Lock()
	for _, n := range nodes {
		if _, ok := s.histories[n.URL]; !ok {
			s.histories[n.URL] = &nodeSpeedHistory{}
		}
	}
	s.mu.Unlock()

	ticker := time.NewTicker(s.testInterval)
	defer ticker.Stop()

	slog.Info("speed test loop started", "interval", s.testInterval, "nodes", len(nodes))

	// 立即执行一次
	s.testAll(nodes)

	for {
		select {
		case <-ctx.Done():
			slog.Info("speed test loop stopped")
			return
		case <-ticker.C:
			s.testAll(nodes)
		}
	}
}

// testAll 对当前在线节点执行一轮测速
func (s *SpeedTester) testAll(nodes []*Node) {
	for _, node := range nodes {
		speed := s.singleTest(node)
		s.updateSpeed(node, speed)
	}
}

// testSingle 对单个节点测速并更新，供 Manager.TestSingleNode 调用
func (s *SpeedTester) testSingle(node *Node, mgr *Manager) {
	// socks5 代理节点不直接测速
	if node.Type == NodeTypeSocks5 {
		return
	}

	speed := s.singleTest(node)
	s.updateSpeed(node, speed)

	// 如果测速成功，标记节点恢复健康
	if speed > 0 {
		mgr.MarkSuccess(node)
	}
}

// updateSpeed 更新节点的滑动窗口平均速度
func (s *SpeedTester) updateSpeed(node *Node, speed float64) {
	s.mu.Lock()
	h, ok := s.histories[node.URL]
	if !ok {
		h = &nodeSpeedHistory{}
		s.histories[node.URL] = h
	}
	s.mu.Unlock()

	h.mu.Lock()
	h.results = append(h.results, SpeedResult{
		Timestamp: time.Now(),
		Speed:     speed,
	})
	if len(h.results) > s.windowSize {
		h.results = h.results[len(h.results)-s.windowSize:]
	}

	var total float64
	for _, r := range h.results {
		total += r.Speed
	}
	avgSpeed := total / float64(len(h.results))
	h.mu.Unlock()

	node.mu.Lock()
	node.Speed = avgSpeed
	node.LastCheck = time.Now()
	node.mu.Unlock()

	slog.Debug("speed test result", "node", node.DisplayName, "speed_kbps", avgSpeed)
}

// singleTest 对单个节点执行测速
func (s *SpeedTester) singleTest(node *Node) float64 {
	// socks5 代理节点不通过 HTTP 直接测速
	if node.Type == NodeTypeSocks5 {
		return 0
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	start := time.Now()
	resp, err := client.Get(s.testURL)
	if err != nil {
		slog.Debug("speed test request failed", "node", node.DisplayName, "error", err)
		return 0
	}
	defer resp.Body.Close()

	// 下载最多 512KB 估算速度
	limited := io.LimitReader(resp.Body, 512*1024)
	downloaded, _ := io.Copy(io.Discard, limited)

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0
	}

	// 返回 KB/s
	return float64(downloaded) / 1024.0 / elapsed
}
