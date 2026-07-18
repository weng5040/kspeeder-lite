package server

import (
	"encoding/json"
	"net/http"
	"time"
)

var startTime = time.Now()

func healthzHandler(deps *Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		total, healthy := deps.NodeMgr.GetHealthStatus()
		resp := map[string]interface{}{
			"status":           "ok",
			"uptime":           time.Since(startTime).String(),
			"nodes_total":      total,
			"nodes_healthy":    healthy,
			"active_downloads": 0, // TODO: 从 downloader 获取
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
