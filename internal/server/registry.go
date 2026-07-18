package server

import (
	"crypto/tls"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/kspeeder/kspeeder-lite/internal/config"
	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
	"github.com/kspeeder/kspeeder-lite/internal/registry"
	"github.com/kspeeder/kspeeder-lite/internal/tlsutil"
)

// Dependencies 服务器依赖
type Dependencies struct {
	Config  *config.Config
	NodeMgr *nodemgr.Manager
}

// NewRegistryServer 创建 registry HTTPS 服务器
func NewRegistryServer(cfg *config.Config, deps *Dependencies) (*http.Server, error) {
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RealIP)

	// Registry API V2 路由
	regHandler := registry.NewHandler(cfg, deps.NodeMgr)

	r.Get("/v2/", regHandler.V2Ping)
	r.Head("/v2/", regHandler.V2Ping)
	r.Get("/v2/{name}/manifests/{reference}", regHandler.GetManifest)
	r.Head("/v2/{name}/manifests/{reference}", regHandler.GetManifest)
	r.Get("/v2/{name}/blobs/{digest}", regHandler.GetBlob)
	r.Head("/v2/{name}/blobs/{digest}", regHandler.GetBlob)

	// 上传端返回 405
	r.Post("/v2/{name}/blobs/uploads/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// 健康检查
	r.Get("/healthz", healthzHandler(deps))

	// TLS 证书处理
	var tlsConfig *tls.Config
	if cfg.Server.TLS.Cert != "" && cfg.Server.TLS.Key != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Server.TLS.Cert, cfg.Server.TLS.Key)
		if err != nil {
			return nil, err
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	} else {
		// 自动生成自签证书
		cert, err := tlsutil.GenerateSelfSigned(cfg.Server.RegistryDomain)
		if err != nil {
			return nil, err
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		slog.Info("generated self-signed certificate", "domain", cfg.Server.RegistryDomain)
	}

	srv := &http.Server{
		Addr:      ":5443",
		Handler:   r,
		TLSConfig: tlsConfig,
	}

	return srv, nil
}
