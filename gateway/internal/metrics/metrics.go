// Prometheus 指标:QPS/延迟/后端错误/ZERO_RESULTS 率(spec §9 哨兵)。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_http_requests_total",
		Help: "HTTP requests by path and status.",
	}, []string{"path", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gateway_http_request_duration_seconds",
		Help:    "Request latency.",
		Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
	}, []string{"path"})

	BackendErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_backend_errors_total",
		Help: "Upstream backend failures.",
	}, []string{"backend"})

	ZeroResults = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_zero_results_total",
		Help: "Responses with no results (data-quality sentinel).",
	}, []string{"path"})
)
