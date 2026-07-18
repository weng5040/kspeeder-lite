package registry

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/kspeeder/kspeeder-lite/internal/downloader"
)

// serveBlob blob 下载入口
// 解析 name/digest/range → 调用 downloader.Download → 流式写入 ResponseWriter
func (h *Handler) serveBlob(w http.ResponseWriter, r *http.Request, name, digest string) {
	// HEAD 请求：确认 blob 是否存在
	if r.Method == http.MethodHead {
		h.headBlob(w, r, name, digest)
		return
	}

	// GET 请求：多源并发下载
	dlReq := downloader.DownloadRequest{
		Name:     name,
		Digest:   digest,
		Registry: "dockerhub",
	}

	// 解析 Range header
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		parsedRange, err := downloader.ParseRange(rangeHeader, 0)
		if err != nil {
			slog.Warn("invalid range header", "range", rangeHeader, "error", err)
		} else {
			dlReq.Range = parsedRange
		}
	}

	// 多源下载
	body, contentLength, err := h.downloader.Download(r.Context(), dlReq)
	if err != nil {
		slog.Error("blob download failed", "name", name, "digest", digest, "error", err)
		http.Error(w, "download failed", http.StatusBadGateway)
		return
	}
	defer body.Close()

	// 设置响应头
	w.Header().Set("Content-Type", "application/octet-stream")
	if contentLength > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
	}
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Accept-Ranges", "bytes")

	if dlReq.Range != nil {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	// 流式写入
	buf := make([]byte, 32*1024)
	io.CopyBuffer(w, body, buf)
}

// headBlob HEAD 请求处理
func (h *Handler) headBlob(w http.ResponseWriter, r *http.Request, name, digest string) {
	nodes := h.nodeMgr.SelectForBlob("dockerhub", 0, 1)
	if len(nodes) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	node := nodes[0]

	blobURL := fmt.Sprintf("%s/v2/%s/blobs/%s", node.URL, name, digest)

	req, err := http.NewRequestWithContext(r.Context(), "HEAD", blobURL, nil)
	if err != nil {
		slog.Error("create head blob request", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("head blob failed", "node", node.URL, "error", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer resp.Body.Close()

	transparentHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
}
