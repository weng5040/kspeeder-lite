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

// DB is the SQLite-backed persistence layer for nodes and metrics.
type DB struct {
	db *sql.DB
}

// NodeRecord is a persisted node entry.
type NodeRecord struct {
	URL         string
	DisplayName string
	Type        string
	Priority    int
	Enabled     bool
	Targets     []string
	Token       string
	FailCount   int32
	SuccessCnt  int64
	LatencyMs   int64
	SpeedKBps   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// MetricRecord is a single scoring event for a node.
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

// StatsPeriod represents a time window for aggregations.
type StatsPeriod struct {
	Name    string
	Seconds int64
}

// Predefined stats periods
var (
	Period7Day     = StatsPeriod{"7d", 7 * 86400}
	Period15Day    = StatsPeriod{"15d", 15 * 86400}
	Period30Day    = StatsPeriod{"30d", 30 * 86400}
	Period180Day   = StatsPeriod{"180d", 180 * 86400}
	Period365Day   = StatsPeriod{"365d", 365 * 86400}
	AllPeriods     = []StatsPeriod{Period7Day, Period15Day, Period30Day, Period180Day, Period365Day}
)

// AggregatedStats holds per-node aggregated metrics for a time period.
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

// Open opens (or creates) the SQLite database.
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
		CREATE TABLE IF NOT EXISTS nodes (
			url          TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			type         TEXT DEFAULT 'mirror',
			priority     INTEGER DEFAULT 50,
			enabled      INTEGER DEFAULT 1,
			targets      TEXT DEFAULT '["dockerhub"]',
			token        TEXT DEFAULT '',
			fail_count   INTEGER DEFAULT 0,
			success_cnt  INTEGER DEFAULT 0,
			latency_ms   INTEGER DEFAULT 0,
			speed_kbps   INTEGER DEFAULT 0,
			created_at   INTEGER NOT NULL,
			updated_at   INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS node_metrics (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			node_url    TEXT NOT NULL REFERENCES nodes(url),
			timestamp   INTEGER NOT NULL,
			latency_ms  INTEGER DEFAULT 0,
			speed_kbps  INTEGER DEFAULT 0,
			success     INTEGER DEFAULT 0,
			fail_count  INTEGER DEFAULT 0,
			score       INTEGER DEFAULT 0,
			inflight    INTEGER DEFAULT 0,
			healthy     INTEGER DEFAULT 1,
			bytes_total INTEGER DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_metrics_url  ON node_metrics(node_url);
		CREATE INDEX IF NOT EXISTS idx_metrics_time ON node_metrics(timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_url_time ON node_metrics(node_url, timestamp);
	`)
	return err
}

// ─── Node CRUD ──────────────────────────────────────────────

// SaveNodes persists all nodes. Existing nodes are updated, new ones inserted.
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
			INSERT OR REPLACE INTO nodes
				(url, display_name, type, priority, enabled, targets, token,
				 fail_count, success_cnt, latency_ms, speed_kbps, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT created_at FROM nodes WHERE url=?), ?), ?)`,
			n.URL, n.DisplayName, n.Type, n.Priority, boolToInt(n.Enabled),
			string(targetsJSON), n.Token, n.FailCount, n.SuccessCnt,
			n.LatencyMs, n.SpeedKBps, n.URL, now, now,
		)
		if err != nil {
			return fmt.Errorf("save node %s: %w", n.URL, err)
		}
	}
	return tx.Commit()
}

// LoadNodes reads all persisted nodes.
func (s *DB) LoadNodes() ([]NodeRecord, error) {
	rows, err := s.db.Query(`SELECT url, display_name, type, priority, enabled, targets, token,
		fail_count, success_cnt, latency_ms, speed_kbps, created_at, updated_at FROM nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []NodeRecord
	for rows.Next() {
		var n NodeRecord
		var targetsJSON string
		var enabled int
		var createdAt, updatedAt int64
		if err := rows.Scan(&n.URL, &n.DisplayName, &n.Type, &n.Priority, &enabled,
			&targetsJSON, &n.Token, &n.FailCount, &n.SuccessCnt, &n.LatencyMs,
			&n.SpeedKBps, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(targetsJSON), &n.Targets)
		n.Enabled = enabled != 0
		n.CreatedAt = time.Unix(createdAt, 0)
		n.UpdatedAt = time.Unix(updatedAt, 0)
		result = append(result, n)
	}
	return result, rows.Err()
}

// ─── Metrics CRUD ───────────────────────────────────────────

// InsertMetric writes a metric record.
func (s *DB) InsertMetric(r MetricRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO node_metrics (node_url, timestamp, latency_ms, speed_kbps, success, fail_count, score, inflight, healthy, bytes_total)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.NodeURL, r.Timestamp.Unix(), r.LatencyMs, r.SpeedKBps,
		boolToInt(r.Success), r.FailCount, r.Score, r.InFlight, boolToInt(r.Healthy), r.BytesTotal,
	)
	return err
}

// LoadLatestMetrics loads the most recent metric for each node.
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

// ─── Aggregation Queries ────────────────────────────────────

// GetStats returns aggregated stats for all time periods.
func (s *DB) GetStats() (map[string][]AggregatedStats, error) {
	result := make(map[string][]AggregatedStats)
	for _, p := range AllPeriods {
		stats, err := s.getStatsForPeriod(p)
		if err != nil {
			return nil, fmt.Errorf("period %s: %w", p.Name, err)
		}
		result[p.Name] = stats
	}
	return result, nil
}

// GetStatsForPeriod returns aggregated stats for a specific period key.
func (s *DB) GetStatsForPeriod(periodKey string) ([]AggregatedStats, error) {
	for _, p := range AllPeriods {
		if p.Name == periodKey {
			return s.getStatsForPeriod(p)
		}
	}
	return nil, fmt.Errorf("unknown period: %s", periodKey)
}

func (s *DB) getStatsForPeriod(p StatsPeriod) ([]AggregatedStats, error) {
	cutoff := time.Now().Unix() - p.Seconds
	rows, err := s.db.Query(`
		SELECT node_url,
			COUNT(*) as total,
			SUM(CASE WHEN success THEN 1 ELSE 0 END) as successes,
			COALESCE(AVG(latency_ms), 0) as avg_latency,
			COALESCE(AVG(speed_kbps), 0) as avg_speed,
			COALESCE(AVG(score), 0) as avg_score,
			COALESCE(SUM(bytes_total), 0) as total_bytes,
			COALESCE(MAX(timestamp), 0) as last_used
		FROM node_metrics
		WHERE timestamp > ?
		GROUP BY node_url
		ORDER BY avg_score DESC
	`, cutoff)
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
		// Extract short name from URL for display
		as.NodeURL = extractShortName(as.NodeURL)
		result = append(result, as)
	}
	return result, rows.Err()
}

// ─── Cleanup ────────────────────────────────────────────────

// PruneMetrics removes metrics older than the given duration.
func (s *DB) PruneMetrics(retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()
	_, err := s.db.Exec(`DELETE FROM node_metrics WHERE timestamp < ?`, cutoff)
	if err != nil {
		return err
	}
	slog.Info("store: pruned old metrics", "cutoff_days", retentionDays)
	return nil
}

// Close closes the database.
func (s *DB) Close() error {
	return s.db.Close()
}

// ─── Helpers ────────────────────────────────────────────────

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
