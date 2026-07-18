package admin

import (
	"encoding/json"
	"net/http"

	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
)

// API 管理 API
type API struct {
	nodeMgr *nodemgr.Manager
}

// NewAPI 创建管理 API
func NewAPI(mgr *nodemgr.Manager) *API {
	return &API{nodeMgr: mgr}
}

// ListNodes GET /admin/nodes
func (a *API) ListNodes(w http.ResponseWriter, r *http.Request) {
	nodes := a.nodeMgr.List()
	snapshots := make([]nodemgr.NodeSnapshot, len(nodes))
	for i, n := range nodes {
		snapshots[i] = nodemgr.Snapshot(n)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snapshots)
}

// TestNode POST /admin/nodes/{id}/test
func (a *API) TestNode(w http.ResponseWriter, r *http.Request) {
	// 触发单节点测速（阶段三实现）
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "not implemented yet"})
}

// Stats GET /admin/stats
func (a *API) Stats(w http.ResponseWriter, r *http.Request) {
	total, healthy := a.nodeMgr.GetHealthStatus()
	resp := map[string]interface{}{
		"nodes_total":      total,
		"nodes_healthy":    healthy,
		"active_downloads": 0,
		"downloads_total":  0,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ReloadConfig POST /admin/config/reload
func (a *API) ReloadConfig(w http.ResponseWriter, r *http.Request) {
	// 配置热加载由 fsnotify 自动处理
	// 此端点留存，方便手动触发
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "config reload triggered"})
}
