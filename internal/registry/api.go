package registry

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kspeeder/kspeeder-lite/internal/config"
	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
)

// Handler 实现 Docker Registry HTTP API V2
type Handler struct {
	cfg     *config.Config
	nodeMgr *nodemgr.Manager
}

// NewHandler 创建 registry handler
func NewHandler(cfg *config.Config, mgr *nodemgr.Manager) *Handler {
	return &Handler{cfg: cfg, nodeMgr: mgr}
}

// V2Ping GET/HEAD /v2/ — 版本握手
func (h *Handler) V2Ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	w.WriteHeader(http.StatusOK)
}

// GetManifest GET/HEAD /v2/{name}/manifests/{reference}
func (h *Handler) GetManifest(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	reference := chi.URLParam(r, "reference")

	slog.Info("manifest request", "name", name, "ref", reference)

	// 单节点代理：选一个健康节点
	nodes := h.nodeMgr.SelectForBlob("dockerhub", 0, 1)
	if len(nodes) == 0 {
		http.Error(w, "no healthy nodes available", http.StatusBadGateway)
		return
	}

	node := nodes[0]
	manifestURL := strings.TrimRight(node.URL, "/") + "/v2/" + name + "/manifests/" + reference

	// 转发请求
	req, err := http.NewRequestWithContext(r.Context(), r.Method, manifestURL, nil)
	if err != nil {
		slog.Error("create manifest request", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 透传关键 headers
	copyHeader(req.Header, r.Header, "Accept")
	copyHeader(req.Header, r.Header, "Authorization")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("manifest fetch failed", "node", node.URL, "error", err)
		h.nodeMgr.MarkFailed(node)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
		slog.Error("manifest upstream error", "node", node.URL, "status", resp.StatusCode)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	h.nodeMgr.MarkSuccess(node)

	// 透传 headers 和 body
	transparentHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// GetBlob GET/HEAD /v2/{name}/blobs/{digest}
func (h *Handler) GetBlob(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	digest := chi.URLParam(r, "digest")

	slog.Info("blob request", "name", name, "digest", digest)

	if r.Method == http.MethodHead {
		// HEAD 请求：确认 blob 存在
		nodes := h.nodeMgr.SelectForBlob("dockerhub", 0, 1)
		if len(nodes) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		node := nodes[0]
		blobURL := strings.TrimRight(node.URL, "/") + "/v2/" + name + "/blobs/" + digest

		req, _ := http.NewRequestWithContext(r.Context(), "HEAD", blobURL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		defer resp.Body.Close()
		transparentHeaders(w, resp)
		w.WriteHeader(resp.StatusCode)
		return
	}

	// GET 请求：多源并发下载（阶段一先用单节点透传）
	nodes := h.nodeMgr.SelectForBlob("dockerhub", 0, 1)
	if len(nodes) == 0 {
		http.Error(w, "no healthy nodes available", http.StatusBadGateway)
		return
	}

	node := nodes[0]
	blobURL := strings.TrimRight(node.URL, "/") + "/v2/" + name + "/blobs/" + digest

	req, err := http.NewRequestWithContext(r.Context(), "GET", blobURL, nil)
	if err != nil {
		slog.Error("create blob request", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 透传 Range header
	copyHeader(req.Header, r.Header, "Range")
	copyHeader(req.Header, r.Header, "Authorization")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("blob fetch failed", "node", node.URL, "error", err)
		h.nodeMgr.MarkFailed(node)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		slog.Error("blob upstream error", "node", node.URL, "status", resp.StatusCode)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	h.nodeMgr.MarkSuccess(node)

	// 流式透传
	transparentHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)

	buf := make([]byte, 32*1024) // 32KB buffer
	io.CopyBuffer(w, resp.Body, buf)
}

// copyHeader 复制指定 header
func copyHeader(dst, src http.Header, key string) {
	if v := src.Get(key); v != "" {
		dst.Set(key, v)
	}
}

// transparentHeaders 透传响应头
func transparentHeaders(w http.ResponseWriter, resp *http.Response) {
	transparent := []string{
		"Content-Type", "Content-Length", "Content-Range",
		"Docker-Content-Digest", "Docker-Distribution-API-Version",
		"Etag", "Cache-Control", "Accept-Ranges", "Last-Modified",
	}
	for _, h := range transparent {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
}
