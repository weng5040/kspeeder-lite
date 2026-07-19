package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct{ db *sql.DB }

// NodeRecord is a persisted node entry (identity + config only, no runtime state).
type NodeRecord struct {
	URL         string
	DisplayName string
	Enabled     bool
	Targets     []string
	Token       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MetricRecord struct {
	NodeURL    string
	Timestamp  time.Time
	LatencyMs  int64
	SpeedKBps  int64
	Success    bool
	FailCount  int32
	Score      int32
	InFlight   int32
	Healthy    bool
	BytesTotal int64
}

var (
	Period7Day   = StatsPeriod{"7d", 7 * 86400}
	Period15Day  = StatsPeriod{"15d", 15 * 86400}
	Period30Day  = StatsPeriod{"30d", 30 * 86400}
	Period180Day = StatsPeriod{"180d", 180 * 86400}
	Period365Day = StatsPeriod{"365d", 365 * 86400}
	AllPeriods   = []StatsPeriod{Period7Day, Period15Day, Period30Day, Period180Day, Period365Day}
)

type StatsPeriod struct{ Name string; Seconds int64 }

type AggregatedStats struct {
	NodeURL     string  `json:"node_url"`
	Total       int     `json:"total_requests"`
	Successes   int     `json:"successes"`
	Failures    int     `json:"failures"`
	SuccessRate float64 `json:"success_rate"`
	AvgLatency  float64 `json:"avg_latency_ms"`
	AvgSpeed    float64 `json:"avg_speed_kbps"`
	AvgScore    float64 `json:"avg_score"`
	TotalBytes  int64   `json:"total_bytes"`
	LastUsed    string  `json:"last_used"`
}

// ── Open ──

func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "pullfusion.db")
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_busy_timeout=5000&_sync=1")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &DB{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	slog.Info("store: database opened", "path", path)
	return s, nil
}

func (s *DB) migrate() error {
	_, err := s.db.Exec(`
		-- 节点配置表：每个镜像加速节点的标识和配置
		CREATE TABLE IF NOT EXISTS nodes (
			url          TEXT PRIMARY KEY,  -- 节点唯一标识（镜像站地址）
			display_name TEXT NOT NULL,     -- 前端展示名称（来自 status.anye.xyz）
			enabled      INTEGER DEFAULT 1, -- 是否启用：1=启用 0=禁用
			targets      TEXT DEFAULT '["dockerhub"]', -- 支持的 registry（JSON 数组）
			token        TEXT DEFAULT '',    -- 节点专用认证 token（预留）
			created_at   INTEGER NOT NULL,  -- 首次入库时间（Unix 秒）
			updated_at   INTEGER NOT NULL   -- 最后更新时间（Unix 秒）
		);

		-- 评分时序表：每次下载事件的完整记录
		-- 所有评分维度（延迟/速度/失败/成功/分数/负载）都存在这里
		-- 支持 7d/30d/365d 等任意时间窗口的聚合查询
		CREATE TABLE IF NOT EXISTS node_metrics (
			id          INTEGER PRIMARY KEY AUTOINCREMENT, -- 自增事件 ID
			node_url    TEXT NOT NULL REFERENCES nodes(url),-- 关联的节点地址
			timestamp   INTEGER NOT NULL,   -- 事件发生时间（Unix 秒）
			latency_ms  INTEGER DEFAULT 0,  -- 本次请求延迟（毫秒）
			speed_kbps  INTEGER DEFAULT 0,  -- 本次下载速度（KB/s）
			success     INTEGER DEFAULT 0,  -- 是否成功：1=成功 0=失败
			fail_count  INTEGER DEFAULT 0,  -- 当时的连续失败次数
			score       INTEGER DEFAULT 0,  -- 当时的综合评分（0~10000）
			inflight    INTEGER DEFAULT 0,  -- 当时的并发数
			healthy     INTEGER DEFAULT 1,  -- 当时是否健康：1=健康 0=熔断
			bytes_total INTEGER DEFAULT 0   -- 本次下载字节数
		);

		CREATE INDEX IF NOT EXISTS idx_metrics_url      ON node_metrics(node_url);
		CREATE INDEX IF NOT EXISTS idx_metrics_time     ON node_metrics(timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_url_time ON node_metrics(node_url, timestamp);
	`)
	return err
}

// ─── Node CRUD ──────────────────────────────────────────────

func (s *DB) SaveNodes(nodes []NodeRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, n := range nodes {
		targetsJSON, _ := json.Marshal(n.Targets)
		_, err := tx.Exec(`
			INSERT OR REPLACE INTO nodes (url, display_name, enabled, targets, token, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, COALESCE((SELECT created_at FROM nodes WHERE url=?), ?), ?)`,
			n.URL, n.DisplayName, boolToInt(n.Enabled), string(targetsJSON), n.Token, n.URL, now, now)
		if err != nil {
			return fmt.Errorf("save node %s: %w", n.URL, err)
		}
	}
	return tx.Commit()
}

func (s *DB) LoadNodes() ([]NodeRecord, error) {
	rows, err := s.db.Query(`SELECT url, display_name, enabled, targets, token, created_at, updated_at FROM nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []NodeRecord
	for rows.Next() {
		var n NodeRecord
		var targetsJSON string
		var enabled int
		var ca, ua int64
		if err := rows.Scan(&n.URL, &n.DisplayName, &enabled, &targetsJSON, &n.Token, &ca, &ua); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(targetsJSON), &n.Targets)
		n.Enabled = enabled != 0
		n.CreatedAt = time.Unix(ca, 0)
		n.UpdatedAt = time.Unix(ua, 0)
		result = append(result, n)
	}
	return result, rows.Err()
}

// ─── Metrics + Aggregation ──────────────────────────────────

func (s *DB) InsertMetric(r MetricRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO node_metrics (node_url, timestamp, latency_ms, speed_kbps, success, fail_count, score, inflight, healthy, bytes_total)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.NodeURL, r.Timestamp.Unix(), r.LatencyMs, r.SpeedKBps,
		boolToInt(r.Success), r.FailCount, r.Score, r.InFlight, boolToInt(r.Healthy), r.BytesTotal)
	return err
}

func (s *DB) LoadLatestMetrics() (map[string]MetricRecord, error) {
	rows, err := s.db.Query(`
		SELECT node_url, timestamp, latency_ms, speed_kbps, success, fail_count, score, inflight, healthy, bytes_total
		FROM node_metrics WHERE id IN (SELECT MAX(id) FROM node_metrics GROUP BY node_url)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]MetricRecord)
	for rows.Next() {
		var r MetricRecord
		var ts int64
		var success, healthy int
		if err := rows.Scan(&r.NodeURL, &ts, &r.LatencyMs, &r.SpeedKBps,
			&success, &r.FailCount, &r.Score, &r.InFlight, &healthy, &r.BytesTotal); err != nil {
			return nil, err
		}
		r.Timestamp = time.Unix(ts, 0)
		r.Success = success != 0
		r.Healthy = healthy != 0
		result[r.NodeURL] = r
	}
	return result, rows.Err()
}

// RecentFails returns how many of the last N requests to a node failed.
func (s *DB) RecentFails(nodeURL string, lookback int) int {
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM (
		SELECT success FROM node_metrics WHERE node_url=? ORDER BY id DESC LIMIT ?
	) WHERE success=0`, nodeURL, lookback).Scan(&count)
	return count
}

func (s *DB) GetStats() (map[string][]AggregatedStats, error) {
	result := make(map[string][]AggregatedStats)
	for _, p := range AllPeriods {
		stats, err := s.getStatsForPeriod(p)
		if err != nil {
			return nil, err
		}
		result[p.Name] = stats
	}
	return result, nil
}

func (s *DB) GetStatsForPeriod(key string) ([]AggregatedStats, error) {
	for _, p := range AllPeriods {
		if p.Name == key {
			return s.getStatsForPeriod(p)
		}
	}
	return nil, fmt.Errorf("unknown period: %s", key)
}

func (s *DB) getStatsForPeriod(p StatsPeriod) ([]AggregatedStats, error) {
	rows, err := s.db.Query(`
		SELECT node_url, COUNT(*), SUM(CASE WHEN success THEN 1 ELSE 0 END),
			COALESCE(AVG(latency_ms),0), COALESCE(AVG(speed_kbps),0),
			COALESCE(AVG(score),0), COALESCE(SUM(bytes_total),0), COALESCE(MAX(timestamp),0)
		FROM node_metrics WHERE timestamp > ? GROUP BY node_url ORDER BY AVG(score) DESC`,
		time.Now().Unix()-p.Seconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AggregatedStats
	for rows.Next() {
		var as AggregatedStats
		var lastUsed int64
		if err := rows.Scan(&as.NodeURL, &as.Total, &as.Successes,
			&as.AvgLatency, &as.AvgSpeed, &as.AvgScore, &as.TotalBytes, &lastUsed); err != nil {
			return nil, err
		}
		as.Failures = as.Total - as.Successes
		if as.Total > 0 {
			as.SuccessRate = float64(as.Successes) / float64(as.Total)
		}
		if lastUsed > 0 {
			as.LastUsed = time.Unix(lastUsed, 0).Format(time.RFC3339)
		}
		as.NodeURL = extractShortName(as.NodeURL)
		result = append(result, as)
	}
	return result, rows.Err()
}

func (s *DB) PruneMetrics(retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	_, err := s.db.Exec(`DELETE FROM node_metrics WHERE timestamp < ?`, cutoff)
	if err == nil {
		slog.Info("store: pruned old metrics", "cutoff_days", retentionDays)
	}
	return err
}

func (s *DB) Close() error { return s.db.Close() }

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func extractShortName(url string) string {
	s := strings.TrimPrefix(url, "https://")
	s = strings.TrimPrefix(s, "http://")
	if idx := strings.Index(s, "/"); idx > 0 {
		s = s[:idx]
	}
	return s
}
