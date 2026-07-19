package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// MetricStore persists per-node scoring and timing data.
type MetricStore struct {
	db *sql.DB
}

// MetricRecord is one recorded event for a node.
type MetricRecord struct {
	NodeURL   string
	Timestamp time.Time
	LatencyMs int64
	SpeedKBps int64
	Success   bool
	FailCount int32
	Score     int32
	InFlight  int32
	Healthy   bool
}

// Open opens (or creates) the SQLite metrics database.
func Open(dataDir string) (*MetricStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	path := filepath.Join(dataDir, "metrics.db")
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite: single writer

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	slog.Info("store: metrics db opened", "path", path)
	return &MetricStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS node_metrics (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			node_url   TEXT NOT NULL,
			timestamp  INTEGER NOT NULL,
			latency_ms INTEGER DEFAULT 0,
			speed_kbps INTEGER DEFAULT 0,
			success    INTEGER DEFAULT 0,
			fail_count INTEGER DEFAULT 0,
			score      INTEGER DEFAULT 0,
			inflight   INTEGER DEFAULT 0,
			healthy    INTEGER DEFAULT 1
		);
		CREATE INDEX IF NOT EXISTS idx_node_url   ON node_metrics(node_url);
		CREATE INDEX IF NOT EXISTS idx_timestamp  ON node_metrics(timestamp);
		CREATE INDEX IF NOT EXISTS idx_node_time  ON node_metrics(node_url, timestamp);
	`)
	return err
}

// Insert writes a metric record.
func (s *MetricStore) Insert(r MetricRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO node_metrics (node_url, timestamp, latency_ms, speed_kbps, success, fail_count, score, inflight, healthy)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.NodeURL, r.Timestamp.Unix(), r.LatencyMs, r.SpeedKBps,
		boolToInt(r.Success), r.FailCount, r.Score, r.InFlight, boolToInt(r.Healthy),
	)
	if err != nil {
		return fmt.Errorf("insert metric: %w", err)
	}
	return nil
}

// LoadLatest loads the most recent metric for each unique node.
func (s *MetricStore) LoadLatest() (map[string]MetricRecord, error) {
	rows, err := s.db.Query(`
		SELECT node_url, timestamp, latency_ms, speed_kbps, success, fail_count, score, inflight, healthy
		FROM node_metrics
		WHERE id IN (SELECT MAX(id) FROM node_metrics GROUP BY node_url)
	`)
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
			&success, &r.FailCount, &r.Score, &r.InFlight, &healthy); err != nil {
			return nil, err
		}
		r.Timestamp = time.Unix(ts, 0)
		r.Success = success != 0
		r.Healthy = healthy != 0
		result[r.NodeURL] = r
	}
	return result, rows.Err()
}

// Stats7Day returns aggregated stats for the last 7 days per node.
func (s *MetricStore) Stats7Day() (map[string]NodeStats, error) {
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	rows, err := s.db.Query(`
		SELECT node_url,
			COUNT(*) AS total,
			SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successes,
			AVG(latency_ms) AS avg_latency,
			AVG(speed_kbps) AS avg_speed,
			AVG(score) AS avg_score
		FROM node_metrics
		WHERE timestamp > ?
		GROUP BY node_url
		ORDER BY avg_score DESC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]NodeStats)
	for rows.Next() {
		var ns NodeStats
		var nodeURL string
		if err := rows.Scan(&nodeURL, &ns.Total, &ns.Successes, &ns.AvgLatency, &ns.AvgSpeed, &ns.AvgScore); err != nil {
			return nil, err
		}
		ns.SuccessRate = float64(ns.Successes) / float64(max(ns.Total, 1))
		result[nodeURL] = ns
	}
	return result, rows.Err()
}

// NodeStats holds aggregated statistics for a node.
type NodeStats struct {
	Total       int     `json:"total"`
	Successes   int     `json:"successes"`
	SuccessRate float64 `json:"success_rate"`
	AvgLatency  float64 `json:"avg_latency_ms"`
	AvgSpeed    float64 `json:"avg_speed_kbps"`
	AvgScore    float64 `json:"avg_score"`
}

// Close closes the database.
func (s *MetricStore) Close() error {
	return s.db.Close()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
