package registry

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kspeeder/kspeeder-lite/internal/config"
	"github.com/kspeeder/kspeeder-lite/internal/downloader"
	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
)

// Handler 实现 Docker Registry HTTP API V2
type Handler struct {
	cfg        *config.Config
	nodeMgr    *nodemgr.Manager
	downloader *downloader.MultiSourceDownloader
}

// NewHandler 创建 registry handler
func NewHandler(cfg *config.Config, mgr *nodemgr.Manager, dl *downloader.MultiSourceDownloader) *Handler {
	return &Handler{cfg: cfg, nodeMgr: mgr, downloader: dl}
}

// V2Ping GET/HEAD /v2/ — 版本握手
func (h *Handler) V2Ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	w.WriteHeader(http.StatusOK)
}

// GetManifest GET/HEAD /v2/{name}/manifests/{reference}
// 委托到 manifest.go 的实现
func (h *Handler) GetManifest(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	reference := chi.URLParam(r, "reference")

	slog.Info("manifest request", "name", name, "ref", reference)
	h.proxyManifest(w, r, name, reference)
}

// GetBlob GET/HEAD /v2/{name}/blobs/{digest}
// 委托到 blob.go 的实现
func (h *Handler) GetBlob(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	digest := chi.URLParam(r, "digest")

	slog.Info("blob request", "name", name, "digest", digest)
	h.serveBlob(w, r, name, digest)
}

// ServeHTTP 直接 HTTP 请求处理（用于 CONNECT 隧道内路由）
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rtr := chi.NewRouter()
	rtr.Get("/v2/", h.V2Ping)
	rtr.Head("/v2/", h.V2Ping)
	rtr.Get("/v2/{name:.+}/manifests/{reference}", h.GetManifest)
	rtr.Head("/v2/{name:.+}/manifests/{reference}", h.GetManifest)
	rtr.Get("/v2/{name:.+}/blobs/{digest}", h.GetBlob)
	rtr.Head("/v2/{name:.+}/blobs/{digest}", h.GetBlob)
	rtr.ServeHTTP(w, r)
}
