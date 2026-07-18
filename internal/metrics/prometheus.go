package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	BlobDownloadsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kspeeder_blob_downloads_total",
			Help: "Total number of blob downloads",
		},
		[]string{"registry", "status"},
	)

	BlobDownloadDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kspeeder_blob_download_duration_seconds",
			Help:    "Blob download duration distribution",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12),
		},
		[]string{"registry"},
	)

	BlobDownloadBytes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kspeeder_blob_download_bytes",
			Help: "Total bytes downloaded",
		},
	)

	NodeSpeed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kspeeder_node_speed_mbps",
			Help: "Node current speed in Mbps",
		},
		[]string{"node"},
	)

	NodeHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kspeeder_node_health",
			Help: "Node health status (1=healthy, 0=unhealthy)",
		},
		[]string{"node"},
	)

	NodeInflight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kspeeder_node_inflight",
			Help: "Node current inflight downloads",
		},
		[]string{"node"},
	)

	ActiveDownloads = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kspeeder_active_downloads",
			Help: "Current active downloads",
		},
	)

	ConfigReloadsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kspeeder_config_reloads_total",
			Help: "Total config reloads",
		},
	)
)

// Init 注册 Prometheus 指标
func Init() {
	prometheus.MustRegister(
		BlobDownloadsTotal,
		BlobDownloadDuration,
		BlobDownloadBytes,
		NodeSpeed,
		NodeHealth,
		NodeInflight,
		ActiveDownloads,
		ConfigReloadsTotal,
	)
}

// Handler 返回 Prometheus HTTP handler
func Handler() http.Handler {
	return promhttp.Handler()
}

// IncConfigReload 增加配置重载计数
func IncConfigReload() {
	ConfigReloadsTotal.Inc()
}
