package admin

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kspeeder/kspeeder-lite/internal/downloader"
	"github.com/kspeeder/kspeeder-lite/internal/nodemgr"
)

// ReloadFunc 配置热加载回调
type ReloadFunc func() error

// API 管理 API
type API struct {
	nodeMgr  *nodemgr.Manager
	reloader ReloadFunc
}

// NewAPI 创建管理 API
func NewAPI(mgr *nodemgr.Manager) *API {
	return &API{nodeMgr: mgr}
}

// NewAPIWithReload 创建带热加载的管理 API
func NewAPIWithReload(mgr *nodemgr.Manager, reloader ReloadFunc) *API {
	return &API{nodeMgr: mgr, reloader: reloader}
}

// SetReloader 设置热加载回调（延迟注入）
func (a *API) SetReloader(reloader ReloadFunc) {
	a.reloader = reloader
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
	nodeID := chi.URLParam(r, "id")
	if nodeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing node id"})
		return
	}

	nodes := a.nodeMgr.List()
	var target *nodemgr.Node
	for _, n := range nodes {
		if n.URL == nodeID || n.DisplayName == nodeID {
			target = n
			break
		}
	}

	if target == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}

	a.nodeMgr.TestSingleNode(target)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "testing",
		"node":   target.URL,
	})
}

// Stats GET /admin/stats
func (a *API) Stats(w http.ResponseWriter, r *http.Request) {
	total, healthy := a.nodeMgr.GetHealthStatus()
	stats := downloader.GetGlobalStats()
	resp := map[string]interface{}{
		"nodes_total":      total,
		"nodes_healthy":    healthy,
		"active_downloads": stats.Active,
		"completed":        stats.Completed,
		"failed":           stats.Failed,
		"error_rate":       stats.ErrorRate,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ReloadConfig POST /admin/config/reload
func (a *API) ReloadConfig(w http.ResponseWriter, r *http.Request) {
	if a.reloader == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"status": "reload not available",
			"error":  "no reload function configured",
		})
		return
	}

	if err := a.reloader(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "config reloaded"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
