package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kspeeder/kspeeder-lite/internal/config"
	"github.com/kspeeder/kspeeder-lite/internal/metrics"
	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
	"github.com/kspeeder/kspeeder-lite/internal/server"
	"github.com/kspeeder/kspeeder-lite/pkg/version"
)

func main() {
	configPath := flag.String("config", "/config/nodes.yaml", "path to nodes.yaml config file")
	flag.Parse()

	// 环境变量覆盖
	if envConfig := os.Getenv("KS_CONFIG"); envConfig != "" {
		*configPath = envConfig
	}

	slog.Info("kspeeder-lite starting", "version", version.Version, "config", *configPath)

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// 初始化 metrics
	metrics.Init()

	// 初始化节点管理器
	nodeMgr := nodemgr.NewManager(cfg)
	nodeMgr.StartSpeedTest(context.Background())

	// 启动配置热加载
	configWatcher, err := config.StartWatcher(*configPath, cfg, nodeMgr)
	if err != nil {
		slog.Warn("failed to start config watcher, hot-reload disabled", "error", err)
	}

	// 构建依赖
	deps := &server.Dependencies{
		Config:  cfg,
		NodeMgr: nodeMgr,
	}

	// 启动 registry 服务器 (:5443)
	registrySrv, err := server.NewRegistryServer(cfg, deps)
	if err != nil {
		slog.Error("failed to create registry server", "error", err)
		os.Exit(1)
	}

	// 启动 CONNECT 代理服务器 (:5003)
	proxySrv := server.NewConnectProxy(cfg, deps)

	// 优雅关闭
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 启动 registry HTTPS 服务
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.RegistryPort)
		slog.Info("registry server listening", "addr", addr)
		if err := registrySrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			slog.Error("registry server error", "error", err)
		}
	}()

	// 启动 CONNECT 代理
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.ProxyPort)
		slog.Info("connect proxy listening", "addr", addr)
		if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("proxy server error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if configWatcher != nil {
		configWatcher.Close()
	}
	registrySrv.Shutdown(shutdownCtx)
	proxySrv.Shutdown(shutdownCtx)

	slog.Info("kspeeder-lite stopped")
}
