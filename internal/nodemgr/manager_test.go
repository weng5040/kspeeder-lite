package nodemgr

import (
	"testing"
)

func TestBalancerScore(t *testing.T) {
	b := NewDefaultBalancer()

	node := &Node{
		URL:      "https://test.example.com",
		Priority: 1,
		Enabled:  true,
		Healthy:  true,
		Speed:    50 * 1024, // 50 MB/s in KB/s
	}

	score := b.Score(node)
	if score <= 0 {
		t.Errorf("expected positive score, got %f", score)
	}
}

func TestBalancerTopK(t *testing.T) {
	b := NewDefaultBalancer()

	nodes := []*Node{
		{URL: "node1", Priority: 1, Enabled: true, Healthy: true, Speed: 100 * 1024},
		{URL: "node2", Priority: 2, Enabled: true, Healthy: true, Speed: 50 * 1024},
		{URL: "node3", Priority: 3, Enabled: true, Healthy: true, Speed: 10 * 1024},
	}

	result := b.TopK(nodes, 2)
	if len(result) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(result))
	}
	// node1 should have highest score (highest speed, highest priority)
	if result[0].URL != "node1" {
		t.Errorf("expected node1 as top, got %s", result[0].URL)
	}
}

func TestNodeMarkFailed(t *testing.T) {
	mgr := NewManager(nil)
	node := &Node{
		URL:    "https://test.example.com",
		Type:   NodeTypeMirror,
		Enabled: true,
		Healthy: true,
	}

	// Mark failed 3 times -> unhealthy
	mgr.MarkFailed(node)
	mgr.MarkFailed(node)
	if node.FailCount != 2 {
		t.Errorf("expected failcount 2, got %d", node.FailCount)
	}
	mgr.MarkFailed(node)
	if node.Healthy {
		t.Error("expected node to be unhealthy after 3 failures")
	}

	// Mark success -> reset
	mgr.MarkSuccess(node)
	if !node.Healthy {
		t.Error("expected node to be healthy after success")
	}
	if node.FailCount != 0 {
		t.Errorf("expected failcount 0, got %d", node.FailCount)
	}
}

func TestHealthStatus(t *testing.T) {
	mgr := NewManager(nil)
	mgr.nodes = []*Node{
		{URL: "n1", Enabled: true, Healthy: true},
		{URL: "n2", Enabled: true, Healthy: false},
		{URL: "n3", Enabled: true, Healthy: true},
	}

	total, healthy := mgr.GetHealthStatus()
	if total != 3 {
		t.Errorf("expected 3 total, got %d", total)
	}
	if healthy != 2 {
		t.Errorf("expected 2 healthy, got %d", healthy)
	}
}
