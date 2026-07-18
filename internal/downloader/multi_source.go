package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
)

// Range 下载区间
type Range struct {
	Start int64
	End   int64 // exclusive, -1 表示到末尾
}

// DownloadRequest 下载请求
type DownloadRequest struct {
	Name         string
	Digest       string
	ExpectedSize int64
	Range        *Range
	Registry     string
}

// Chunk 分块
type Chunk struct {
	Index  int
	Start  int64
	End    int64 // exclusive
	Node   *nodemgr.Node
	Reader io.ReadCloser
	Offset int64
	Err    error
}

// MultiSourceDownloader 多源并发下载器
type MultiSourceDownloader struct {
	nodeMgr         *nodemgr.Manager
	maxConcurrent   int
	maxGlobal       int
	chunkMinSize    int64
	chunkMaxSize    int64
	failThreshold   int
}

// NewMultiSourceDownloader 创建下载器
func NewMultiSourceDownloader(nodeMgr *nodemgr.Manager, maxConcurrent, maxGlobal int, chunkMin, chunkMax int64, failThreshold int) *MultiSourceDownloader {
	return &MultiSourceDownloader{
		nodeMgr:       nodeMgr,
		maxConcurrent: maxConcurrent,
		maxGlobal:     maxGlobal,
		chunkMinSize:  chunkMin,
		chunkMaxSize:  chunkMax,
		failThreshold: failThreshold,
	}
}

// Download 多源下载 blob（阶段二实现核心逻辑）
func (d *MultiSourceDownloader) Download(ctx context.Context, req DownloadRequest) (io.ReadCloser, int64, error) {
	nodes := d.nodeMgr.SelectForBlob(req.Registry, req.ExpectedSize, d.maxConcurrent)
	if len(nodes) == 0 {
		return nil, 0, fmt.Errorf("no nodes available")
	}

	// 阶段一简化实现：单节点透传
	return d.singleNodeDownload(ctx, req, nodes[0])
}

// singleNodeDownload 单节点下载（阶段一）
func (d *MultiSourceDownloader) singleNodeDownload(ctx context.Context, req DownloadRequest, node *nodemgr.Node) (io.ReadCloser, int64, error) {
	blobURL := fmt.Sprintf("%s/v2/%s/blobs/%s", node.URL, req.Name, req.Digest)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return nil, 0, err
	}

	if req.Range != nil {
		httpReq.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", req.Range.Start, req.Range.End-1))
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		d.nodeMgr.MarkFailed(node)
		return nil, 0, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		d.nodeMgr.MarkFailed(node)
		return nil, 0, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	d.nodeMgr.MarkSuccess(node)
	return resp.Body, resp.ContentLength, nil
}

// mergeChunks 按 offset 顺序流式合并（阶段二实现）
func mergeChunks(ctx context.Context, chunks []*Chunk, w io.Writer) error {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Start < chunks[j].Start
	})

	for _, c := range chunks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.Err != nil {
			return c.Err
		}
		if _, err := io.Copy(w, c.Reader); err != nil {
			return err
		}
		c.Reader.Close()
	}
	return nil
}
