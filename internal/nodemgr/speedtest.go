package nodemgr

// SpeedTester 测速器
type SpeedTester struct {
	testURL      string
	testInterval int // 秒
}

// NewSpeedTester 创建测速器
func NewSpeedTester(testURL string, intervalSec int) *SpeedTester {
	return &SpeedTester{
		testURL:      testURL,
		testInterval: intervalSec,
	}
}

// Test 测速单个节点
func (s *SpeedTester) Test(node *Node) float64 {
	// 实现：下载测速文件，计算速度
	// 阶段三实现
	return 0
}
