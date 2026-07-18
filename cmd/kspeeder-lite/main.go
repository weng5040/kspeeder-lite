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

	"github.com/kspeeder/kspeeder-lite/internal/admin"
	"github.com/kspeeder/kspeeder-lite/internal/cache"
	"github.com/kspeeder/kspeeder-lite/internal/config"
	"github.com/kspeeder/kspeeder-lite/internal/downloader"
	"github.com/kspeeder/kspeeder-lite/internal/metrics"
	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
	"github.com/kspeeder/kspeeder-lite/internal/server"
	"github.com/kspeeder/kspeeder-lite/pkg/version"
)

func main() {
	configPath := flag.String("config", "/config/nodes.yaml", "path to nodes.yaml config file")
	flag.Parse()

	if envConfig := os.Getenv("KS_CONFIG"); envConfig != "" {
		*configPath = envConfig
	}

	slog.Info("kspeeder-lite starting", "version", version.Version, "config", *configPath)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	metrics.Init()

	// 初始化 Blob 本地磁盘缓存（二次加速）
	var blobCache *cache.Cache
	if cfg.Downloader.CacheEnabled {
		blobCache, err = cache.NewCache(cfg.Downloader.CacheDir, cfg.Downloader.CacheMaxSize)
		if err != nil {
			slog.Error("failed to create blob cache, caching disabled", "error", err)
		} else {
			used, _, count := blobCache.Stats()
			slog.Info("blob cache enabled", "dir", cfg.Downloader.CacheDir,
				"used", used, "files", count)
		}
	}

	nodeMgr := nodemgr.NewManager(cfg)
	nodeMgr.StartSpeedTest(context.Background())

	configWatcher, err := config.StartWatcher(*configPath, cfg, nodeMgr)
	if err != nil {
		slog.Warn("failed to start config watcher, hot-reload disabled", "error", err)
	}

	dl := downloader.NewMultiSourceDownloader(
		nodeMgr,
		cfg.Downloader.MaxConcurrentPerBlob,
		cfg.Downloader.MaxConcurrentGlobal,
		cfg.Downloader.ChunkMinSize,
		cfg.Downloader.ChunkMaxSize,
		cfg.Downloader.NodeFailThreshold,
	)

	api := admin.NewAPI(nodeMgr)

	deps := &server.Dependencies{
		Config:     cfg,
		NodeMgr:    nodeMgr,
		Downloader: dl,
		Recorder:   api,
	}

	registrySrv, err := server.NewRegistryServer(cfg, deps)
	if err != nil {
		slog.Error("failed to create registry server", "error", err)
		os.Exit(1)
	}

	proxySrv := server.NewConnectProxy(cfg, deps)

	if configWatcher != nil {
		api.SetReloader(func() error {
			newCfg, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			nodeMgr.ReloadNodes(newCfg)
			metrics.IncConfigReload()
			return nil
		})
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.RegistryPort)
		slog.Info("registry server listening", "addr", addr)
		if err := registrySrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			slog.Error("registry server error", "error", err)
		}
	}()

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
