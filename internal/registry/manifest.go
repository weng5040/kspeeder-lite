package registry

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pullfusion/pullfusion/internal/config"
	"github.com/pullfusion/pullfusion/internal/nodemgr"
)

// proxyManifest forces registry-1.docker.io with token for dockerhub manifests.
func (h *Handler) proxyManifest(w http.ResponseWriter, r *http.Request, name, reference, registry string) {
	var node *nodemgr.Node
	allNodes := h.nodeMgr.List()
	for _, n := range allNodes {
		if n.Enabled && n.Healthy && n.URL == "https://registry-1.docker.io" {
			node = n
			break
		}
	}
	if node == nil {
		nodes := h.nodeMgr.SelectForBlob(registry, 0, 1)
		if len(nodes) == 0 {
			http.Error(w, "no healthy nodes available", http.StatusBadGateway)
			return
		}
		node = nodes[0]
	}

	manifestURL := strings.TrimRight(node.URL, "/") + "/v2/" + name + "/manifests/" + reference
	req, err := http.NewRequestWithContext(r.Context(), r.Method, manifestURL, nil)
	if err != nil {
		slog.Error("create manifest request", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Inject dockerhub token
	if registry == "dockerhub" && h.tokenSvc != nil {
		if tok, err := h.tokenSvc.GetToken(r.Context(), registry, name); err == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}

	copyHeader(req.Header, r.Header, "Accept")

	if token := getNodeToken(h.cfg, node.URL); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("manifest fetch failed", "node", node.URL, "error", err)
		h.nodeMgr.MarkFailed(node)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotModified {
		h.nodeMgr.MarkSuccess(node)
	}

	transparentHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// getNodeToken returns the configured token for a node URL.
func getNodeToken(cfg *config.Config, nodeURL string) string {
	for _, m := range cfg.Mirrors.Dockerhub {
		if m.URL == nodeURL && m.Token != "" { return m.Token }
	}
	for _, m := range cfg.Mirrors.Ghcr {
		if m.URL == nodeURL && m.Token != "" { return m.Token }
	}
	return ""
}
