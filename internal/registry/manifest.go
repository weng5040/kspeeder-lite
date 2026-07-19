package registry

import (
	"io"
	"log/slog"
	"net/http"
)

func (h *Handler) proxyManifest(w http.ResponseWriter, r *http.Request, name, reference, registry string) {
	manifestURL := "https://docker.1ms.run/v2/" + name + "/manifests/" + reference

	req, err := http.NewRequestWithContext(r.Context(), r.Method, manifestURL, nil)
	if err != nil {
		slog.Error("create manifest request", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	copyHeader(req.Header, r.Header, "Accept")

	// Always get token from docker.1ms.run auth internally
	// Docker mirror mode never sends auth headers to mirrors
	if h.tokenSvc != nil {
		if tok, err := h.tokenSvc.GetToken(r.Context(), registry, name); err == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("manifest fetch failed", "error", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	transparentHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
