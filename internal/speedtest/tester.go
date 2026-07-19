package speedtest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Tester periodically measures node download speeds using a small manifest download.
type Tester struct {
	client   *http.Client
	interval time.Duration
}

// Result is the outcome of a single speed test.
type Result struct {
	NodeURL   string
	LatencyMs int64  // time to first byte
	SpeedKBps int64  // actual download speed (bytes / duration)
	Bytes     int64  // total bytes downloaded
	Error     string // non-empty if test failed
}

// New creates a speed tester that runs every interval.
func New(interval time.Duration) *Tester {
	return &Tester{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives: true, // fresh connection each test
			},
		},
		interval: interval,
	}
}

// Run starts the periodic speed test loop. Call in a goroutine.
// onResult is called with each test result for recording.
func (t *Tester) Run(ctx context.Context, nodes []NodeInfo, onResult func(Result)) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	// Run immediately on start
	t.testAll(nodes, onResult)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.testAll(nodes, onResult)
		}
	}
}

// NodeInfo is the minimal info needed to test a node.
type NodeInfo struct {
	URL   string
	Token string // optional auth token
}

// TestImage is the image used for speed testing (alpine:latest manifest, ~2KB).
const TestImage = "library/alpine"

func (t *Tester) TestOne(node NodeInfo) Result {
	manifestURL := node.URL + "/v2/" + TestImage + "/manifests/latest"
	slog.Debug("speedtest: testing node", "url", manifestURL[:min(len(manifestURL), 60)])

	req, err := http.NewRequest("GET", manifestURL, nil)
	if err != nil {
		return Result{NodeURL: node.URL, Error: err.Error()}
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	if node.Token != "" {
		req.Header.Set("Authorization", "Bearer "+node.Token)
	}

	start := time.Now()
	resp, err := t.client.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return Result{NodeURL: node.URL, LatencyMs: latencyMs, Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Result{NodeURL: node.URL, LatencyMs: latencyMs, Error: "HTTP " + resp.Status}
	}

	// Measure actual download speed by reading the full body
	readStart := time.Now()
	n, err := io.Copy(io.Discard, resp.Body)
	readDuration := time.Since(readStart).Milliseconds()

	if err != nil {
		return Result{NodeURL: node.URL, LatencyMs: latencyMs, Error: "read: " + err.Error()}
	}

	speedKBps := int64(0)
	totalMs := latencyMs + readDuration
	if totalMs > 0 && n > 0 {
		speedKBps = n * 1000 / (totalMs * 1024) // bytes→KB, ms→s
	}

	return Result{
		NodeURL:   node.URL,
		LatencyMs: latencyMs,
		SpeedKBps: speedKBps,
		Bytes:     n,
	}
}

func (t *Tester) testAll(nodes []NodeInfo, onResult func(Result)) {
	slog.Info("speedtest: starting round", "nodes", len(nodes))
	for _, n := range nodes {
		r := t.TestOne(n)
		onResult(r)
		slog.Info("speedtest: node result", "url", r.NodeURL[:min(len(r.NodeURL), 50)],
			"latency_ms", r.LatencyMs, "speed_kbps", r.SpeedKBps, "bytes", r.Bytes, "error", r.Error)
	}
	slog.Info("speedtest: round complete", "nodes", len(nodes))
}
