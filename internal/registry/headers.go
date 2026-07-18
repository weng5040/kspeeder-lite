package registry

import "net/http"

// copyHeader 复制指定 header
func copyHeader(dst, src http.Header, key string) {
	if v := src.Get(key); v != "" {
		dst.Set(key, v)
	}
}

// transparentHeaders 透传响应头
func transparentHeaders(w http.ResponseWriter, resp *http.Response) {
	transparent := []string{
		"Content-Type",
		"Content-Length",
		"Content-Range",
		"Docker-Content-Digest",
		"Docker-Distribution-API-Version",
		"Etag",
		"Cache-Control",
		"Accept-Ranges",
		"Last-Modified",
	}
	for _, h := range transparent {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
}
