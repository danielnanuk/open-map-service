// 中间件:request-id、auth(M5 Task 2)、metrics(M5 Task 3 追加)。
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielnanuk/open-map-service/gateway/internal/auth"
	"github.com/danielnanuk/open-map-service/gateway/internal/metrics"
)

type ctxKey int

const ctxKeyRequestID ctxKey = iota

func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRequestID).(string)
	return v
}

// WithRequestID 透传或生成请求 ID,写响应头并入 context(日志关联用)。
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			var b [8]byte
			rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// WithMetrics 记录每请求计数与延迟(path 用路由模式名,避免高基数:取 r.URL.Path 的前两段)。
func WithMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		start := time.Now()
		next.ServeHTTP(rec, r)
		path := metricPath(r.URL.Path)
		metrics.RequestsTotal.WithLabelValues(path, strconv.Itoa(rec.status)).Inc()
		metrics.RequestDuration.WithLabelValues(path).Observe(time.Since(start).Seconds())
	})
}

// metricPath 压低基数:/v1/places/{id} → /v1/places/_id,其余取原路径(端点都是固定字面量)。
func metricPath(p string) string {
	if strings.HasPrefix(p, "/v1/places/") && !strings.Contains(p[len("/v1/places/"):], ":") {
		return "/v1/places/_id"
	}
	return p
}

// Checker 是 auth.Store 的接口(便于测试注入 fake)。
type Checker interface {
	Check(ctx context.Context, key string) auth.Decision
}

// WithAuth 当 enabled=true 时校验 API key(X-Goog-Api-Key 头或 ?key= 参数)。
// /healthz 与 /metrics 路径无条件豁免。
func WithAuth(enabled bool, checker Checker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled || r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get("X-Goog-Api-Key")
			if key == "" {
				key = r.URL.Query().Get("key")
			}
			switch checker.Check(r.Context(), key) {
			case auth.DecisionAllowed:
				next.ServeHTTP(w, r)
			case auth.DecisionRateLimited:
				writeError(w, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED", "rate limit exceeded")
			default:
				writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "missing or invalid API key")
			}
		})
	}
}
