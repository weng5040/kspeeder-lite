package server

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
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
	regHandler := registry.NewHandler(cfg, deps.NodeMgr, deps.Downloader)
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

	http.Error(w, "forbidden", http.StatusForbidden)
}

// handleRegistryTunnel 处理 registry 内部隧道
func (h *connectHandler) handleRegistryTunnel(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, bufrw, err := hijacker.Hijack()
	if err != nil {
		slog.Error("hijack failed", "error", err)
		return
	}
	defer clientConn.Close()

	// 发送 200 Connection Established
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// 处理隧道内的 HTTP 请求
	for {
		// 读取请求行: "GET /v2/... HTTP/1.1\r\n"
		reqLine, err := bufrw.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				slog.Error("read tunnel request line", "error", err)
			}
			return
		}
		reqLine = strings.TrimSpace(reqLine)
		if reqLine == "" {
			continue
		}

		// 解析 Method, Path, HTTP version
		parts := strings.SplitN(reqLine, " ", 3)
		if len(parts) < 3 {
			slog.Error("invalid request line", "line", reqLine)
			return
		}
		method := parts[0]
		path := parts[1]
		proto := strings.TrimPrefix(parts[2], "HTTP/")

		// 读取 headers
		header := make(http.Header)
		for {
			line, err := bufrw.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					slog.Error("read tunnel header", "error", err)
				}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			idx := strings.Index(line, ":")
			if idx > 0 {
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				header.Add(key, val)
			}
		}

		// 构建 http.Request
		req := &http.Request{
			Method: method,
			URL:    &url.URL{Path: path},
			Proto:  "HTTP/" + proto,
			Header: header,
			Host:   header.Get("Host"),
		}

		// 创建 mock ResponseWriter
		respWriter := &tunnelResponseWriter{
			conn:   clientConn,
			header: make(http.Header),
		}

		// 路由请求到 registry handler
		h.regHandler.ServeHTTP(respWriter, req)

		// 如果是非持久连接，关闭
		if header.Get("Connection") == "close" {
			return
		}
	}
}

// tunnelResponseWriter 隧道内响应写入器
type tunnelResponseWriter struct {
	conn        net.Conn
	header      http.Header
	wroteHeader bool
	statusCode  int
	body        []byte
}

func (w *tunnelResponseWriter) Header() http.Header {
	return w.header
}

func (w *tunnelResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.body = append(w.body, data...)
	return len(data), nil
}

func (w *tunnelResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = statusCode

	// 构建并写入 HTTP 响应
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = fmt.Sprintf("%d", statusCode)
	}

	var resp strings.Builder
	resp.WriteString("HTTP/1.1 ")
	resp.WriteString(fmt.Sprintf("%d", statusCode))
	resp.WriteString(" ")
	resp.WriteString(statusText)
	resp.WriteString("\r\n")

	for key, vals := range w.header {
		for _, val := range vals {
			resp.WriteString(key)
			resp.WriteString(": ")
			resp.WriteString(val)
			resp.WriteString("\r\n")
		}
	}

	if len(w.body) > 0 {
		resp.WriteString("Content-Length: ")
		resp.WriteString(fmt.Sprintf("%d", len(w.body)))
		resp.WriteString("\r\n")
	}
	resp.WriteString("\r\n")

	w.conn.Write([]byte(resp.String()))
	if len(w.body) > 0 {
		w.conn.Write(w.body)
		w.body = nil
	}
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
