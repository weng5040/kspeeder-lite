package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/pullfusion/pullfusion/internal/nodemgr"
)

// MultiSourceDownloader handles blob downloads with smart node selection.
type MultiSourceDownloader struct {
	nodeMgr *nodemgr.Manager
}

// DownloadRequest represents a blob download request.
type DownloadRequest struct {
	Name     string
	Digest   string
	Registry string
	Token    string // injected registry token
}

// NewMultiSourceDownloader creates a new downloader.
func NewMultiSourceDownloader(nodeMgr *nodemgr.Manager) *MultiSourceDownloader {
	return &MultiSourceDownloader{nodeMgr: nodeMgr}
}

// Download fetches a blob using the best available node.
func (d *MultiSourceDownloader) Download(ctx context.Context, req DownloadRequest) (io.ReadCloser, int64, int, error) {
	node := d.nodeMgr.SelectBest(req.Registry)
	if node == nil {
		return nil, 0, 0, fmt.Errorf("no healthy node available")
	}

	startTime := time.Now()
	blobURL := fmt.Sprintf("%s/v2/%s/blobs/%s", node.URL, req.Name, req.Digest)
	slog.Info("downloading blob", "url", blobURL[:min(len(blobURL), 60)], "node", node.DisplayName, "score", node.Score)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		d.nodeMgr.ReleaseNode(node, false, 0, 0)
		return nil, 0, 0, err
	}

	// Token injection: request-level token takes priority, then node-level
	if req.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	} else if node.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+node.Token)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	latencyMs := time.Since(startTime).Milliseconds()

	if err != nil {
		d.nodeMgr.ReleaseNode(node, false, latencyMs, 0)
		return nil, 0, 0, fmt.Errorf("download from %s: %w", node.DisplayName, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		d.nodeMgr.ReleaseNode(node, false, latencyMs, 0)
		return nil, 0, 0, fmt.Errorf("upstream %s returned %d", node.DisplayName, resp.StatusCode)
	}

	// Calculate speed from Content-Length and total time
	speedKBps := int64(0)
	if resp.ContentLength > 0 && latencyMs > 0 {
		speedKBps = resp.ContentLength / latencyMs // approximate KB/s (bytes/ms ≈ KB/s rough)
	}
	d.nodeMgr.ReleaseNode(node, true, latencyMs, speedKBps)

	return resp.Body, resp.ContentLength, 1, nil
}
