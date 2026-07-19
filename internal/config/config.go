package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 完整配置结构
type Config struct {
	Mirrors    MirrorsConfig    `yaml:"mirrors"`
	Proxies    ProxiesConfig    `yaml:"proxies"`
	Server     ServerConfig     `yaml:"server"`
	Downloader DownloaderConfig `yaml:"downloader"`
	Builtin    BuiltinConfig    `yaml:"builtin_mirrors"`
}

// MirrorsConfig 镜像源配置
type MirrorsConfig struct {
	Dockerhub []MirrorNode `yaml:"dockerhub"`
	Ghcr      []MirrorNode `yaml:"ghcr"`
}

// MirrorNode 镜像源节点
type MirrorNode struct {
	URL         string `yaml:"url"`
	DisplayName string `yaml:"display_name"`
	Token       string `yaml:"token,omitempty"`
}

// ProxiesConfig 代理节点配置
type ProxiesConfig struct {
	Enabled bool        `yaml:"enabled"`
	Nodes   []ProxyNode `yaml:"nodes"`
}

// ProxyNode 代理节点
type ProxyNode struct {
	URL         string   `yaml:"url"`
	DisplayName string   `yaml:"display_name"`
	Targets     []string `yaml:"targets"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	RegistryPort   int       `yaml:"registry_port"`
	ProxyPort      int       `yaml:"proxy_port"`
	ProxyAuth      string    `yaml:"proxy_auth"`
	TLS            TLSConfig `yaml:"tls"`
	RegistryDomain string    `yaml:"registry_domain"`
}

// TLSConfig TLS 配置
type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// DownloaderConfig 下载器配置
type DownloaderConfig struct {
	MaxConcurrentPerBlob int           `yaml:"max_concurrent_per_blob"`
	MaxConcurrentGlobal  int           `yaml:"max_concurrent_global"`
	ChunkMinSize         int64         `yaml:"chunk_min_size"`
	ChunkMaxSize         int64         `yaml:"chunk_max_size"`
	NodeFailThreshold    int           `yaml:"node_fail_threshold"`
	SpeedTestInterval    time.Duration `yaml:"speed_test_interval"`
	SpeedTestURL         string        `yaml:"speed_test_url"`
	CacheEnabled         bool          `yaml:"cache_enabled"`
	CacheDir             string        `yaml:"cache_dir"`
	CacheMaxSize         int64         `yaml:"cache_max_size"`
}

// BuiltinConfig 内置节点配置
type BuiltinConfig struct {
	Dockerhub []string `yaml:"dockerhub"`
	Ghcr      []string `yaml:"ghcr"`
}

// Load 加载配置文件
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	return cfg, nil
}

// applyDefaults 应用默认值
func applyDefaults(cfg *Config) {
	if cfg.Server.RegistryPort == 0 {
		cfg.Server.RegistryPort = 443
	}
	if cfg.Server.ProxyPort == 0 {
		cfg.Server.ProxyPort = 5003
	}
	if cfg.Server.RegistryDomain == "" {
		cfg.Server.RegistryDomain = "registry.local"
	}
	if cfg.Downloader.MaxConcurrentPerBlob == 0 {
		cfg.Downloader.MaxConcurrentPerBlob = 4
	}
	if cfg.Downloader.MaxConcurrentGlobal == 0 {
		cfg.Downloader.MaxConcurrentGlobal = 64
	}
	if cfg.Downloader.ChunkMinSize == 0 {
		cfg.Downloader.ChunkMinSize = 1 << 20 // 1MB
	}
	if cfg.Downloader.ChunkMaxSize == 0 {
		cfg.Downloader.ChunkMaxSize = 16 << 20 // 16MB
	}
	if cfg.Downloader.NodeFailThreshold == 0 {
		cfg.Downloader.NodeFailThreshold = 3
	}
	if cfg.Downloader.SpeedTestInterval == 0 {
		cfg.Downloader.SpeedTestInterval = 5 * time.Minute
	}
	if cfg.Downloader.SpeedTestURL == "" {
		cfg.Downloader.SpeedTestURL = "https://github.com/linkease/istore-packages/releases/download/prebuilt/quickstart-binary-0.11.9.tar.gz"
	}

	// 内置 builtin 节点
	if len(cfg.Builtin.Dockerhub) == 0 {
		cfg.Builtin.Dockerhub = []string{
			"https://docker.1ms.run",
			"https://docker.m.daocloud.io",
			"https://dockerproxy.net",
			"https://hub.rat.dev",
			"https://docker.xuanyuan.me",
		}
	}
	if len(cfg.Builtin.Ghcr) == 0 {
		cfg.Builtin.Ghcr = []string{"https://ghcr.nju.edu.cn"}
	}
}

// applyEnvOverrides 环境变量覆盖
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("PF_REGISTRY_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.RegistryPort = p
		}
	}
	if v := os.Getenv("PF_PROXY_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.ProxyPort = p
		}
	}
	if v := os.Getenv("PF_PROXY_AUTH"); v != "" {
		cfg.Server.ProxyAuth = v
	}
	if v := os.Getenv("PF_REGISTRY_DOMAIN"); v != "" {
		cfg.Server.RegistryDomain = v
	}
	if v := os.Getenv("PF_TLS_CERT"); v != "" {
		cfg.Server.TLS.Cert = v
	}
	if v := os.Getenv("PF_TLS_KEY"); v != "" {
		cfg.Server.TLS.Key = v
	}
}
