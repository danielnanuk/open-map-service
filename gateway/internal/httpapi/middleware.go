// 中间件:request-id、auth(M5 Task 2)、metrics(M5 Task 3 追加)。
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/danielnanuk/open-map-service/gateway/internal/auth"
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
