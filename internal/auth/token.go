package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// TokenService Docker registry token 服务
type TokenService struct {
	cache      *tokenCache
	ghcrTokens map[string]string // key: "ghcr:"+name, value: PAT
	ghcrMu     sync.RWMutex
}

// tokenCache token 缓存
type tokenCache struct {
	mu     sync.RWMutex
	tokens map[string]*tokenEntry
}

type tokenEntry struct {
	token     string
	expiresAt time.Time
}

// NewTokenService 创建 token 服务
func NewTokenService() *TokenService {
	return &TokenService{
		cache: &tokenCache{
			tokens: make(map[string]*tokenEntry),
		},
		ghcrTokens: make(map[string]string),
	}
}

// GetToken 获取 dockerhub 匿名 token
func (t *TokenService) GetToken(ctx context.Context, registry, name string) (string, error) {
	key := registry + ":" + name

	t.cache.mu.RLock()
	if e, ok := t.cache.tokens[key]; ok && time.Until(e.expiresAt) > 60*time.Second {
		t.cache.mu.RUnlock()
		return e.token, nil
	}
	t.cache.mu.RUnlock()

	// 重新获取
	token, expiresIn, err := t.fetchToken(ctx, name)
	if err != nil {
		return "", err
	}

	t.cache.mu.Lock()
	t.cache.tokens[key] = &tokenEntry{
		token:     token,
		expiresAt: time.Now().Add(time.Duration(expiresIn)*time.Second - 60*time.Second),
	}
	t.cache.mu.Unlock()

	return token, nil
}

// fetchToken 从 dockerhub auth 获取 token
func (t *TokenService) fetchToken(ctx context.Context, name string) (string, int, error) {
	url := fmt.Sprintf("https://docker.1ms.run/openapi/v1/auth/token?service=docker.1ms.run&scope=repository:%s:pull", name)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", 0, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("fetch token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var result struct {
		Token     string `json:"access_token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("parse token response: %w", err)
	}

	if result.Token == "" {
		return "", 0, fmt.Errorf("empty token returned")
	}

	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300 // 默认 5 分钟
	}

	return result.Token, expiresIn, nil
}

// SetGHCRToken 设置 ghcr PAT token（从配置加载时调用）
func (t *TokenService) SetGHCRToken(name, pat string) {
	t.ghcrMu.Lock()
	defer t.ghcrMu.Unlock()
	key := "ghcr:" + name
	t.ghcrTokens[key] = pat
}

// GetGHCRToken 获取 ghcr PAT token。
// 直接返回 PAT 作为 Bearer token（ghcr 直接使用 PAT，无需 exchange）。
// 对于公共仓库的匿名访问（无 token 配置），返回空字符串。
func (t *TokenService) GetGHCRToken(ctx context.Context, name string) (string, error) {
	key := "ghcr:" + name

	// 精确匹配
	t.ghcrMu.RLock()
	if pat, ok := t.ghcrTokens[key]; ok {
		t.ghcrMu.RUnlock()
		return pat, nil
	}
	t.ghcrMu.RUnlock()

	// 通配符：空名称代表全局 ghcr token
	t.ghcrMu.RLock()
	if pat, ok := t.ghcrTokens["ghcr:"]; ok {
		t.ghcrMu.RUnlock()
		return pat, nil
	}
	t.ghcrMu.RUnlock()

	// 无 token，尝试匿名访问
	return "", nil
}
