package downloader

import (
	"sync/atomic"
	"time"

	"github.com/kspeeder/kspeeder-lite/internal/metrics"
)

// 全局下载统计（供 admin API 使用）
var globalStats struct {
	Active    atomic.Int64
	Completed atomic.Int64
	Failed    atomic.Int64
}

// StatsSnapshot 统计快照
type StatsSnapshot struct {
	Active    int64
	Completed int64
	Failed    int64
	ErrorRate float64
}

// GetGlobalStats 获取全局下载统计
func GetGlobalStats() StatsSnapshot {
	completed := globalStats.Completed.Load()
	failed := globalStats.Failed.Load()
	var errorRate float64
	total := completed + failed
	if total > 0 {
		errorRate = float64(failed) / float64(total)
	}
	return StatsSnapshot{
		Active:    globalStats.Active.Load(),
		Completed: completed,
		Failed:    failed,
		ErrorRate: errorRate,
	}
}

// TrackStart 跟踪下载开始
func TrackStart() {
	globalStats.Active.Add(1)
	metrics.ActiveDownloads.Inc()
}

// TrackEnd 跟踪下载完成
func TrackEnd(registry string, duration time.Duration, bytes int64, err error) {
	globalStats.Active.Add(-1)
	if err != nil {
		globalStats.Failed.Add(1)
	} else {
		globalStats.Completed.Add(1)
	}
	metrics.ActiveDownloads.Dec()
	status := "success"
	if err != nil {
		status = "error"
	}
	metrics.BlobDownloadsTotal.WithLabelValues(registry, status).Inc()
	if err == nil {
		metrics.BlobDownloadDuration.WithLabelValues(registry).Observe(duration.Seconds())
		metrics.BlobDownloadBytes.Add(float64(bytes))
	}
}
