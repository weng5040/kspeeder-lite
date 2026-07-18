package registry

import (
	"fmt"
	"strings"
)

// Resolver 镜像名 → 目标 registry 映射
type Resolver struct{}

// NewResolver 创建解析器
func NewResolver() *Resolver {
	return &Resolver{}
}

// Resolve 解析镜像名到目标 URL
// 如 library/nginx → https://registry-1.docker.io/v2/library/nginx
func (r *Resolver) Resolve(registry, name string) (string, error) {
	switch registry {
	case "dockerhub":
		return fmt.Sprintf("https://registry-1.docker.io/v2/%s", name), nil
	case "ghcr":
		// 去除 ghcr/ 前缀
		clean := strings.TrimPrefix(name, "ghcr/")
		return fmt.Sprintf("https://ghcr.io/v2/%s", clean), nil
	default:
		return "", fmt.Errorf("unknown registry: %s", registry)
	}
}

// DetectRegistry 从镜像名检测 registry
func DetectRegistry(name string) string {
	if strings.HasPrefix(name, "ghcr/") || strings.HasPrefix(name, "ghcr.io/") {
		return "ghcr"
	}
	return "dockerhub"
}
