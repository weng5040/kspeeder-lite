package registry

import (
	"io"
	"log/slog"
	"net/http"
)

// proxyManifest transparently proxies to docker.1ms.run, passing auth through.
func (h *Handler) proxyManifest(w http.ResponseWriter, r *http.Request, name, reference, registry string) {
	manifestURL := "https://docker.1ms.run/v2/" + name + "/manifests/" + reference

	req, err := http.NewRequestWithContext(r.Context(), r.Method, manifestURL, nil)
	if err != nil {
		slog.Error("create manifest request", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Pass through auth headers from docker client
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	copyHeader(req.Header, r.Header, "Accept")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("manifest fetch failed", "error", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward response as-is (including 401 + Www-Authenticate from docker.1ms.run)
	transparentHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
