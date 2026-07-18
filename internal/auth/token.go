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
	cache *tokenCache
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
	url := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull", name)

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
		Token     string `json:"token"`
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
