package registry

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"

)


// serveBlob blob 下载入口。
// registry 参数用于选择节点来源（dockerhub/ghcr）。
func (h *Handler) serveBlob(w http.ResponseWriter, r *http.Request, name, digest, registry string) {
	// HEAD 请求：确认 blob 是否存在
	if r.Method == http.MethodHead {
		h.headBlob(w, r, name, digest, registry)
		return
	}

	// Proxy blob through docker.1ms.run (fast, handles auth)
	blobURL := "https://docker.1ms.run/v2/" + name + "/blobs/" + digest
	req, _ := http.NewRequestWithContext(r.Context(), r.Method, blobURL, nil)
	if h.tokenSvc != nil {
		if tok, err := h.tokenSvc.GetToken(r.Context(), registry, name); err == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("blob proxy failed", "error", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	transparentHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// headBlob HEAD 请求处理
func (h *Handler) headBlob(w http.ResponseWriter, r *http.Request, name, digest, registry string) {
	nodes := h.nodeMgr.SelectForBlob(registry, 0, 1)
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

	// 如果节点有 token，添加认证头
	if node.Token != "" {
		req.Header.Set("Authorization", "Bearer "+node.Token)
	} else if h.tokenSvc != nil && registry == "dockerhub" {
		if tok, err := h.tokenSvc.GetToken(r.Context(), registry, name); err == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	} else if h.tokenSvc != nil && registry == "dockerhub" {
		if tok, err := h.tokenSvc.GetToken(r.Context(), registry, name); err == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
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
