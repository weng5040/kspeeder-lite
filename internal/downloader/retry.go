package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
)

// retryChunk 为重试下载分块，切换到健康节点并基于 Range 从 chunk.Start 续传。
// 最多重试 maxRetries 次。
func retryChunk(ctx context.Context, ch *Chunk, nodeMgr *nodemgr.Manager, req DownloadRequest, maxRetries int) {
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			ch.Err = err
			return
		}

		// 选择健康节点（排除原有节点以提高成功率）
		nodes := nodeMgr.SelectForBlob(req.Registry, req.ExpectedSize, 2)
		if len(nodes) == 0 {
			ch.Err = fmt.Errorf("chunk %d: no healthy node for retry attempt %d", ch.Index, attempt+1)
			return
		}

		newNode := nodes[0]
		if len(nodes) > 1 && newNode == ch.Node {
			newNode = nodes[1]
		}

		slog.Info("retrying chunk",
			"index", ch.Index,
			"node", newNode.URL,
			"attempt", attempt+1,
			"range", fmt.Sprintf("bytes=%d-%d", ch.Start, ch.End-1),
		)

		// 下载前关闭旧 Reader
		if ch.Reader != nil {
			io.Copy(io.Discard, ch.Reader)
			ch.Reader.Close()
			ch.Reader = nil
		}

		ch.Node = newNode
		nodeMgr.IncrInflight(newNode)
		if err := downloadChunkData(ctx, ch, req, newNode); err != nil {
			nodeMgr.DecrInflight(newNode)
			nodeMgr.MarkFailed(newNode)
			slog.Warn("chunk retry failed",
				"index", ch.Index,
				"attempt", attempt+1,
				"error", err,
			)
			continue
		}

		nodeMgr.MarkSuccess(newNode)
		ch.Err = nil
		return
	}

	if ch.Err == nil {
		ch.Err = fmt.Errorf("chunk %d: exhausted retries (%d)", ch.Index, maxRetries)
	}
}

// downloadChunkData 从指定节点按 Range 下载分块数据，结果写入 ch.Reader。
func downloadChunkData(ctx context.Context, ch *Chunk, req DownloadRequest, node *nodemgr.Node) error {
	blobURL := fmt.Sprintf("%s/v2/%s/blobs/%s", node.URL, req.Name, req.Digest)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if ch.End > 0 {
		httpReq.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", ch.Start, ch.End-1))
	} else {
		httpReq.Header.Set("Range", fmt.Sprintf("bytes=%d-", ch.Start))
	}

	// 如果节点有 token，添加认证头
	if node.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+node.Token)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	ch.Reader = resp.Body
	return nil
}
