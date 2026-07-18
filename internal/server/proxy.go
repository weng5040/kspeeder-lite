package server

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/kspeeder/kspeeder-lite/internal/admin"
	"github.com/kspeeder/kspeeder-lite/internal/config"
	"github.com/kspeeder/kspeeder-lite/internal/metrics"
	"github.com/kspeeder/kspeeder-lite/internal/registry"
)

// NewConnectProxy 创建 CONNECT 代理服务器
func NewConnectProxy(cfg *config.Config, deps *Dependencies) *http.Server {
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	// Prometheus 指标
	r.Handle("/metrics", metrics.Handler())

	// 管理 API
	adminAPI := admin.NewAPI(deps.NodeMgr)
	r.Get("/healthz", healthzHandler(deps))
	r.Get("/admin/nodes", adminAPI.ListNodes)
	r.Post("/admin/nodes/{id}/test", adminAPI.TestNode)
	r.Get("/admin/stats", adminAPI.Stats)
	r.Post("/admin/config/reload", adminAPI.ReloadConfig)

	// CONNECT 代理处理器
	regHandler := registry.NewHandler(cfg, deps.NodeMgr)
	proxyHandler := &connectHandler{
		cfg:        cfg,
		regHandler: regHandler,
	}

	r.HandleFunc("/*", proxyHandler.ServeHTTP)

	return &http.Server{
		Addr:    ":5003",
		Handler: r,
	}
}

// connectHandler CONNECT 代理处理器
type connectHandler struct {
	cfg        *config.Config
	regHandler *registry.Handler
}

// ServeHTTP 处理 CONNECT 请求
func (h *connectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		h.handleConnect(w, r)
		return
	}
	// 非 CONNECT 方法交给 chi 路由
	w.WriteHeader(http.StatusNotFound)
}

// handleConnect 处理 CONNECT 隧道
func (h *connectHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Basic Auth 校验
	if h.cfg.Server.ProxyAuth != "" {
		if !h.checkAuth(r) {
			w.Header().Set("Proxy-Authenticate", "Basic")
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
	}

	// 解析目标地址
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	slog.Info("CONNECT request", "host", r.Host)

	// 对内建的 registry domain，走内部 handler（绕过 TLS）
	if h.cfg.Server.RegistryDomain != "" &&
		(host == h.cfg.Server.RegistryDomain || host == "registry-1.docker.io") &&
		port == "443" {
		h.handleRegistryTunnel(w, r)
		return
	}

	// 其他 CONNECT 拒绝
	http.Error(w, "forbidden", http.StatusForbidden)
}

// handleRegistryTunnel 处理 registry 内部隧道
func (h *connectHandler) handleRegistryTunnel(w http.ResponseWriter, r *http.Request) {
	// 劫持连接
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		slog.Error("hijack failed", "error", err)
		return
	}
	defer clientConn.Close()

	// 发送 200 Connection Established
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// 这里后续阶段实现：从 clientConn 读取 HTTP 请求，交给 regHandler 处理
	// 目前仅建立隧道
	slog.Info("registry tunnel established")
	io.Copy(io.Discard, clientConn)
}

// checkAuth 校验 Basic Auth
func (h *connectHandler) checkAuth(r *http.Request) bool {
	auth := r.Header.Get("Proxy-Authorization")
	if auth == "" {
		return false
	}
	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return false
	}
	return string(decoded) == h.cfg.Server.ProxyAuth
}
