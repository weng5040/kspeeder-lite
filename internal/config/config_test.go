package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	if cfg.Server.RegistryPort != 5443 {
		t.Errorf("expected registry port 5443, got %d", cfg.Server.RegistryPort)
	}
	if cfg.Server.ProxyPort != 5003 {
		t.Errorf("expected proxy port 5003, got %d", cfg.Server.ProxyPort)
	}
	if cfg.Downloader.MaxConcurrentPerBlob != 4 {
		t.Errorf("expected max_concurrent_per_blob 4, got %d", cfg.Downloader.MaxConcurrentPerBlob)
	}
if len(cfg.Mirrors.Dockerhub) == 0 {
		t.Error("expected built-in dockerhub mirrors")
	}
	if len(cfg.Mirrors.Ghcr) == 0 {
		t.Error("expected built-in ghcr mirrors")
	}
	if len(cfg.Builtin.Ghcr) == 0 {
		t.Error("expected builtin ghcr entries")
	}
}

func TestLoadFromFile(t *testing.T) {
	cfg, err := Load("../../configs/nodes.sample.yaml")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.RegistryPort != 5443 {
		t.Errorf("expected port 5443, got %d", cfg.Server.RegistryPort)
	}
	if len(cfg.Mirrors.Dockerhub) < 4 {
		t.Errorf("expected at least 4 mirrors, got %d", len(cfg.Mirrors.Dockerhub))
	}
}

func TestGhcrDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	if len(cfg.Mirrors.Ghcr) != 1 {
		t.Fatalf("expected 1 default ghcr mirror, got %d", len(cfg.Mirrors.Ghcr))
	}

	nju := cfg.Mirrors.Ghcr[0]
	if nju.URL != "https://ghcr.nju.edu.cn" {
		t.Errorf("expected nju-ghcr URL, got %s", nju.URL)
	}
}

func TestGhcrTokenConfig(t *testing.T) {
	cfg := &Config{
		Mirrors: MirrorsConfig{
			Ghcr: []MirrorNode{
				{URL: "https://ghcr.nju.edu.cn", Token: "ghp_test123"},
			},
		},
	}
	applyDefaults(cfg)

	if len(cfg.Mirrors.Ghcr) != 1 {
		t.Fatalf("expected 1 ghcr mirror, got %d", len(cfg.Mirrors.Ghcr))
	}

	node := cfg.Mirrors.Ghcr[0]
	if node.Token != "ghp_test123" {
		t.Errorf("expected token 'ghp_test123', got '%s'", node.Token)
	}
}
