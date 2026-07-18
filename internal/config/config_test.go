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
