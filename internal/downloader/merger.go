package downloader

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
)

// mergeChunks 按 offset 排序后顺序合并分块，写入 w。
func mergeChunks(ctx context.Context, chunks []*Chunk, w io.Writer) error {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Start < chunks[j].Start
	})

	for _, c := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.Err != nil {
			return c.Err
		}
		if c.Reader != nil {
			if _, err := io.Copy(w, c.Reader); err != nil {
				return err
			}
			c.Reader.Close()
		}
	}
	return nil
}

// mergeChunksStream 流式有序合并：从 results 通道中接收已完成的分块，
// 按 index 顺序写入 pw；失败分块调用 retryChunk 重试；支持 context 取消。
func mergeChunksStream(
	ctx context.Context,
	total int,
	results <-chan *Chunk,
	pw *io.PipeWriter,
	nodeMgr *nodemgr.Manager,
	req DownloadRequest,
	maxRetries int,
) error {
	received := make(map[int]*Chunk)
	nextIdx := 0

	for i := 0; i < total; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch, ok := <-results:
			if !ok {
				return fmt.Errorf("result channel closed unexpectedly at chunk %d/%d", i, total)
			}

			received[ch.Index] = ch

			// 尽可能多地写出已就绪的连续分块
			for {
				c, exists := received[nextIdx]
				if !exists {
					break
				}
				delete(received, nextIdx)
				nextIdx++

				// 失败分块重试
				if c.Err != nil {
					retryChunk(ctx, c, nodeMgr, req, maxRetries)
					if c.Err != nil {
						return fmt.Errorf("chunk %d failed after retries: %w", c.Index, c.Err)
					}
				}

				// context 取消检查
				if err := ctx.Err(); err != nil {
					return err
				}

				// 流式写出
				if c.Reader != nil {
					if _, err := io.Copy(pw, c.Reader); err != nil {
						c.Reader.Close()
						return fmt.Errorf("write chunk %d: %w", c.Index, err)
					}
					c.Reader.Close()
				}
			}
		}
	}

	return nil
}
