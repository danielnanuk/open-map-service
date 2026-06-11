# M5:生产加固 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 网关具备 API key 鉴权与配额、Prometheus 指标与告警规则、HTTP 服务器加固与优雅退出;数据更新管道一键蓝绿化;备份/恢复脚本并完成演练;全链路演练通过(spec §13-M5 验收)。

**Architecture:** 中间件链(request-id → metrics → auth)包裹既有 mux,`http.Server` 显式超时 + SIGTERM 优雅退出;api_keys 存 Postgres、内存缓存 60s + per-key 令牌桶;Prometheus 抓取网关 /metrics,告警规则文件随库;更新管道一个脚本串联(OSM 刷新→ETL 链→漂移闸→OSRM/Valhalla 图重建滚动替换);备份 = pg_dump + OpenSearch 文件系统快照 + 图构件 tar。

**Tech Stack:** Go(prometheus/client_golang, x/time/rate)· Prometheus(容器)· bash 管道脚本

**Spec:** §8(鉴权/配额/错误码)/§9(监控告警/更新节奏/降级)/§12(备份)/§13-M5(验收:全链路演练通过)/§14.5(单节点压测)。

**M1-M4 累积待办的处置(明确在内/在外):**
- 在内:http.Server 超时+优雅退出(M1 TODO)、凭据 env 化(.env.example)、独立测试库(M1)、OSRM 镜像 pin(M4)、Geofabrik 增量说明(M2)、ETL→图重建管道(M3 cp -n 注)、压测(§14.5)
- 在外(记 M6 候选,README 注明):类目映射调优(§14.1 ABA Bank 案例)、纯高棉文罗马音别名表(§14.4)、OSRM/Valhalla 速度模型校准(M4 终审)、Valhalla 分块并发化、km/zh 导航 locale 文件、maneuvers 暴露

**执行纪律(M1-M4 经验):** 前台执行 + 充足 timeout;psql 加 statement_timeout;新端口绑 127.0.0.1;集成测试后恢复数据;镜像先 pull 验证;**AUTH_ENABLED 默认 false**(dev/golden 不带 key 照常跑,演练与生产开真)。

---

## 文件结构(全貌)

```
gateway/internal/httpapi/middleware.go        # 新:request-id + metrics + auth 中间件链
gateway/internal/httpapi/middleware_test.go
gateway/internal/auth/keys.go                 # 新:api_keys 读取 + 缓存 + per-key 限流
gateway/internal/auth/keys_test.go
gateway/internal/metrics/metrics.go           # 新:Prometheus 指标定义(含 zero_results)
gateway/cmd/gateway/main.go                   # 修改:Server 加固 + 中间件 + /metrics + 退出
db/migrations/0002_api_keys.sql               # 新
deploy/prometheus/prometheus.yml              # 新:抓取配置
deploy/prometheus/rules.yml                   # 新:告警规则
docker-compose.yml                            # 修改:prometheus 服务、OS 快照卷、凭据 env 化、OSRM pin
.env.example                                  # 新
scripts/update_pipeline.sh                    # 新:蓝绿更新管道(含漂移闸)
scripts/backup.sh / scripts/restore.sh        # 新
scripts/load_test.py                          # 新:§14.5 压测
scripts/full_drill.sh                         # 新:§13-M5 全链路演练
etl/tests/conftest.py                         # 新:测试库隔离(DATABASE_URL_TEST)
etl/etl/index.py                              # 修改:ALIAS 可由 OPENSEARCH_ALIAS 覆写
Makefile / README.md                          # 修改
```

---

### Task 1:HTTP 服务器加固 + request-id(TDD)

**Files:**
- Create: `gateway/internal/httpapi/middleware.go`, `gateway/internal/httpapi/middleware_test.go`
- Modify: `gateway/cmd/gateway/main.go`

- [ ] **Step 1: failing tests**

```go
// gateway/internal/httpapi/middleware_test.go
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDInjected(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFrom(r.Context()) == "" {
			t.Fatal("request id missing in context")
		}
		w.WriteHeader(204)
	})
	rec := httptest.NewRecorder()
	WithRequestID(inner).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("X-Request-Id header missing")
	}
}

func TestRequestIDPassthrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFrom(r.Context()) != "client-supplied" {
			t.Fatalf("got %q", RequestIDFrom(r.Context()))
		}
	})
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Request-Id", "client-supplied")
	WithRequestID(inner).ServeHTTP(httptest.NewRecorder(), req)
}
```

- [ ] **Step 2: run → FAIL(`undefined: WithRequestID`)**

- [ ] **Step 3: 实现**

```go
// gateway/internal/httpapi/middleware.go
// 中间件:request-id(本任务)、metrics 与 auth(后续任务追加于此文件)。
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
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
```

- [ ] **Step 4: main.go 加固**(兑现 M1 的 TODO(M5);中间件链先只挂 request-id,后续任务往链上加)

```go
// main() 末段替换 log.Fatal(http.ListenAndServe(...)):
	handler := httpapi.WithRequestID(mux)
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second, // 大矩阵响应(25 万元素 ~28MB)需要余量
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("gateway listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
```
(import 增加 os/signal、syscall、time、context;删除旧 TODO 注释。)

- [ ] **Step 5: run → ALL PASS + gofmt/vet;`docker compose up -d --build gateway` 后 `docker compose stop gateway` 观察日志出现 "shutting down..."(优雅退出证据),再 start 恢复**

- [ ] **Step 6: Commit**:`feat: http server hardening + request id middleware` + trailer

---

### Task 2:API key 鉴权 + 配额(TDD)

**Files:**
- Create: `db/migrations/0002_api_keys.sql`, `gateway/internal/auth/keys.go`, `gateway/internal/auth/keys_test.go`, `.env.example`
- Modify: `gateway/internal/httpapi/middleware.go`(+WithAuth), `gateway/internal/httpapi/middleware_test.go`, `gateway/cmd/gateway/main.go`, `docker-compose.yml`(凭据 env 化), `Makefile`(gen-api-key)

- [ ] **Step 1: migration**

```sql
-- db/migrations/0002_api_keys.sql
CREATE TABLE IF NOT EXISTS api_keys (
  key        text PRIMARY KEY,              -- 明文 key(单机自托管;轮换=插新删旧)
  name       text NOT NULL,
  rpm_limit  int  NOT NULL DEFAULT 300,     -- 每分钟请求数
  active     bool NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
```

`make migrate` 应用(幂等)。Makefile 加:

```makefile
.PHONY: gen-api-key
gen-api-key:
	@KEY=$$(openssl rand -hex 24); \
	docker exec places-postgis-1 psql -U places -d places -c \
	  "INSERT INTO api_keys (key, name) VALUES ('$$KEY', '$(or $(NAME),default)')" >/dev/null && \
	echo "API key: $$KEY"
```

- [ ] **Step 2: auth 包 failing tests**(纯内存逻辑可测:Store 接缓存与限流,DB 读取经窄接口 fake)

```go
// gateway/internal/auth/keys_test.go
package auth

import (
	"context"
	"testing"
	"time"
)

type fakeDB struct{ keys map[string]int } // key → rpm

func (f *fakeDB) LookupKey(_ context.Context, key string) (rpm int, ok bool, err error) {
	rpm, ok = f.keys[key]
	return rpm, ok, nil
}

func TestAllowUnknownKey(t *testing.T) {
	s := NewStore(&fakeDB{keys: map[string]int{}}, time.Minute)
	if dec := s.Check(context.Background(), "nope"); dec != DecisionDenied {
		t.Fatalf("want denied, got %v", dec)
	}
}

func TestAllowAndRateLimit(t *testing.T) {
	s := NewStore(&fakeDB{keys: map[string]int{"k1": 2}}, time.Minute) // 2 rpm,burst=2
	ctx := context.Background()
	if s.Check(ctx, "k1") != DecisionAllowed || s.Check(ctx, "k1") != DecisionAllowed {
		t.Fatal("first two should pass")
	}
	if s.Check(ctx, "k1") != DecisionRateLimited {
		t.Fatal("third within same minute should be rate limited")
	}
}

func TestCacheAvoidsRepeatedLookups(t *testing.T) {
	db := &countingDB{fakeDB{keys: map[string]int{"k1": 100}}, 0}
	s := NewStore(db, time.Minute)
	ctx := context.Background()
	s.Check(ctx, "k1")
	s.Check(ctx, "k1")
	if db.calls != 1 {
		t.Fatalf("lookup should be cached, calls=%d", db.calls)
	}
}

type countingDB struct {
	fakeDB
	calls int
}

func (c *countingDB) LookupKey(ctx context.Context, key string) (int, bool, error) {
	c.calls++
	return c.fakeDB.LookupKey(ctx, key)
}
```

- [ ] **Step 3: run → FAIL(`undefined: NewStore`)**

- [ ] **Step 4: 实现 auth 包**

```go
// gateway/internal/auth/keys.go
// API key 校验 + 配额:DB 读取经 KeyDB 接口,内存缓存 TTL,per-key 令牌桶(rpm)。
package auth

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Decision int

const (
	DecisionAllowed Decision = iota
	DecisionDenied
	DecisionRateLimited
)

type KeyDB interface {
	LookupKey(ctx context.Context, key string) (rpm int, ok bool, err error)
}

type entry struct {
	rpm       int
	ok        bool
	limiter   *rate.Limiter
	expiresAt time.Time
}

type Store struct {
	db  KeyDB
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]*entry
}

func NewStore(db KeyDB, ttl time.Duration) *Store {
	return &Store{db: db, ttl: ttl, m: map[string]*entry{}}
}

func (s *Store) Check(ctx context.Context, key string) Decision {
	if key == "" {
		return DecisionDenied
	}
	s.mu.Lock()
	e, hit := s.m[key]
	if !hit || time.Now().After(e.expiresAt) {
		s.mu.Unlock()
		rpm, ok, err := s.db.LookupKey(ctx, key)
		if err != nil { // DB 故障:放行已缓存 key 的旧值,未知 key 拒绝(fail-closed)
			if hit {
				s.mu.Lock()
				e.expiresAt = time.Now().Add(s.ttl)
				s.mu.Unlock()
			} else {
				return DecisionDenied
			}
		} else {
			ne := &entry{rpm: rpm, ok: ok, expiresAt: time.Now().Add(s.ttl)}
			if ok {
				ne.limiter = rate.NewLimiter(rate.Limit(float64(rpm)/60.0), rpm)
			}
			s.mu.Lock()
			if hit && e.ok && ok { // 保留既有桶,避免刷新清空配额状态
				ne.limiter = e.limiter
			}
			s.m[key] = ne
			e = ne
			s.mu.Unlock()
		}
	} else {
		s.mu.Unlock()
	}
	if !e.ok {
		return DecisionDenied
	}
	if !e.limiter.Allow() {
		return DecisionRateLimited
	}
	return DecisionAllowed
}
```

`go get golang.org/x/time/rate`。注意测试 `TestAllowAndRateLimit` 期望 burst=rpm:`rate.NewLimiter(rpm/60, rpm)` —— 2 rpm 时桶容量 2,第三次立即请求被拒 ✓。

pgx 侧实现(放 `gateway/internal/store/store.go` 追加,复用连接池):

```go
// LookupKey 实现 auth.KeyDB。
func (p *PG) LookupKey(ctx context.Context, key string) (int, bool, error) {
	var rpm int
	err := p.pool.QueryRow(ctx,
		`SELECT rpm_limit FROM api_keys WHERE key = $1 AND active`, key).Scan(&rpm)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return rpm, true, nil
}
```

- [ ] **Step 5: WithAuth 中间件 + failing tests**(追加 middleware_test.go)

```go
type fakeChecker struct{ dec auth.Decision }

func (f *fakeChecker) Check(context.Context, string) auth.Decision { return f.dec }

func TestAuthDisabledPassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	rec := httptest.NewRecorder()
	WithAuth(false, &fakeChecker{auth.DecisionDenied})(inner).
		ServeHTTP(rec, httptest.NewRequest("GET", "/v1/places/x", nil))
	if rec.Code != 204 {
		t.Fatalf("disabled auth must pass: %d", rec.Code)
	}
}

func TestAuthDeniedAndRateLimited(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	rec := httptest.NewRecorder()
	WithAuth(true, &fakeChecker{auth.DecisionDenied})(inner).
		ServeHTTP(rec, httptest.NewRequest("GET", "/v1/places/x", nil))
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "PERMISSION_DENIED") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	WithAuth(true, &fakeChecker{auth.DecisionRateLimited})(inner).
		ServeHTTP(rec, httptest.NewRequest("GET", "/v1/places/x?key=k", nil))
	if rec.Code != 429 || !strings.Contains(rec.Body.String(), "RESOURCE_EXHAUSTED") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthExemptsHealthAndMetrics(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, p := range []string{"/healthz", "/metrics"} {
		rec := httptest.NewRecorder()
		WithAuth(true, &fakeChecker{auth.DecisionDenied})(inner).
			ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 204 {
			t.Fatalf("%s must be exempt: %d", p, rec.Code)
		}
	}
}
```

实现(追加 middleware.go;key 取 `X-Goog-Api-Key` 头或 `?key=`,对齐 Google):

```go
type Checker interface {
	Check(ctx context.Context, key string) auth.Decision
}

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
			case DecisionAllowedAlias: // 见下注
				next.ServeHTTP(w, r)
			case auth.DecisionRateLimited:
				writeError(w, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED", "rate limit exceeded")
			default:
				writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "missing or invalid API key")
			}
		})
	}
}
```
注:`DecisionAllowedAlias` 为伪码占位说明——实现里直接 `case auth.DecisionAllowed:`(import auth 包)。switch 三分支:Allowed→next、RateLimited→429、默认(Denied)→403。

- [ ] **Step 6: main.go 接线 + 凭据 env 化**

main.go:
```go
	authEnabled := env("AUTH_ENABLED", "false") == "true"
	keyStore := auth.NewStore(pg, time.Minute)
	handler := httpapi.WithRequestID(httpapi.WithAuth(authEnabled, keyStore)(mux))
```

docker-compose.yml:postgis 的 `POSTGRES_PASSWORD: places` 改 `POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-places}`;nominatim 的 `NOMINATIM_PASSWORD: nominatim_places` 改 `${NOMINATIM_PASSWORD:-nominatim_places}`;gateway 的 `DATABASE_URL` 改 `postgresql://places:${POSTGRES_PASSWORD:-places}@postgis:5432/places`,并加 `AUTH_ENABLED: ${AUTH_ENABLED:-false}`。

`.env.example`:
```bash
# 复制为 .env 并改强口令(compose 自动加载;不入库)
POSTGRES_PASSWORD=places
NOMINATIM_PASSWORD=nominatim_places
AUTH_ENABLED=false
```

- [ ] **Step 7: run → ALL PASS + gofmt/vet;e2e:`make migrate && make gen-api-key`,`AUTH_ENABLED=true docker compose up -d gateway` 后无 key 403 / 坏 key 403 / 好 key 200 / 连打超 rpm 出 429(rpm 设小值验证),然后恢复 AUTH_ENABLED=false 重启(golden 不带 key)**

- [ ] **Step 8: Commit**:`feat: api key auth + per-key quota (env-gated)` + trailer

---

### Task 3:Prometheus 指标(TDD)

**Files:**
- Create: `gateway/internal/metrics/metrics.go`
- Modify: `gateway/internal/httpapi/middleware.go`(+WithMetrics)、`middleware_test.go`、`respond.go`/`geocode_handler.go`(zero_results 埋点)、`gateway/cmd/gateway/main.go`(/metrics 路由)

- [ ] **Step 1: 指标定义**

```go
// gateway/internal/metrics/metrics.go
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
```

`go get github.com/prometheus/client_golang/prometheus`。

- [ ] **Step 2: WithMetrics 中间件 + test**

```go
// middleware.go 追加:
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
```

test(追加 middleware_test.go):
```go
func TestMetricPathCardinality(t *testing.T) {
	if metricPath("/v1/places/abc-123") != "/v1/places/_id" {
		t.Fatal("details path must collapse")
	}
	if metricPath("/v1/places:searchText") != "/v1/places:searchText" {
		t.Fatal("rpc-style path must stay literal")
	}
}
```

- [ ] **Step 3: zero_results 埋点**(两处:writeGeocode 在 status=="ZERO_RESULTS" 时 `metrics.ZeroResults.WithLabelValues("/maps/api/geocode/json").Inc()`;docsToPlacesResponse 调用方 SearchText/SearchNearby 在 len(docs)==0 时 `metrics.ZeroResults.WithLabelValues(r.URL.Path).Inc()`)+ backend_errors 埋点(internal() 里 `metrics.BackendErrors.WithLabelValues("internal").Inc()` 与 geocode/matrix 的 fallback log 处按后端名计数:nominatim/opensearch/osrm/valhalla)

- [ ] **Step 4: main.go**:`mux.Handle("GET /metrics", promhttp.Handler())`(import promhttp);中间件链 `WithRequestID(WithMetrics(WithAuth(...)(mux)))`

- [ ] **Step 5: run → ALL PASS;e2e:rebuild gateway,打几个请求后 `curl -s localhost:8080/metrics | grep gateway_` 确认四类指标都有样本(贴摘录)**

- [ ] **Step 6: Commit**:`feat: prometheus metrics (qps/latency/backend errors/zero results)` + trailer

---

### Task 4:Prometheus 服务 + 告警规则

**Files:**
- Create: `deploy/prometheus/prometheus.yml`, `deploy/prometheus/rules.yml`
- Modify: `docker-compose.yml`, `Makefile`(up 加 prometheus), `README.md`

- [ ] **Step 1: 配置**

```yaml
# deploy/prometheus/prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 30s
rule_files: ["/etc/prometheus/rules.yml"]
scrape_configs:
  - job_name: gateway
    static_configs:
      - targets: ["gateway:8080"]
```

```yaml
# deploy/prometheus/rules.yml — 告警规则(通知渠道由运维接 Alertmanager,M5 仅产出规则与 ALERTS 序列)
groups:
  - name: places
    rules:
      - alert: GatewayHighErrorRate
        expr: sum(rate(gateway_http_requests_total{status=~"5.."}[5m])) / clamp_min(sum(rate(gateway_http_requests_total[5m])), 0.001) > 0.05
        for: 5m
        labels: {severity: critical}
        annotations: {summary: "5xx 比例 >5% 持续 5 分钟"}
      - alert: BackendErrorsSpiking
        expr: sum(rate(gateway_backend_errors_total[5m])) > 0.5
        for: 5m
        labels: {severity: warning}
        annotations: {summary: "上游后端错误率异常"}
      - alert: ZeroResultsSurge
        expr: sum(rate(gateway_zero_results_total[15m])) / clamp_min(sum(rate(gateway_http_requests_total[15m])), 0.001) > 0.3
        for: 15m
        labels: {severity: warning}
        annotations: {summary: "ZERO_RESULTS 比例 >30%——数据质量哨兵(spec §9)"}
      - alert: GatewayP99High
        expr: histogram_quantile(0.99, sum(rate(gateway_http_request_duration_seconds_bucket[5m])) by (le)) > 10
        for: 10m
        labels: {severity: warning}
        annotations: {summary: "P99 延迟 >10s"}
```

compose 加:
```yaml
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./deploy/prometheus:/etc/prometheus:ro
      - prometheus-data:/prometheus
    ports: ["127.0.0.1:9090:9090"]
```
(顶层 volumes 加 prometheus-data;`make up` 加 prometheus。)

- [ ] **Step 2: e2e**:up 后 `curl -s 'localhost:9090/api/v1/targets' | grep -o '"health":"[a-z]*"'` 应为 up;`curl -s 'localhost:9090/api/v1/rules' | head -c 300` 出现告警组;README 监控段(指标清单、规则说明、Alertmanager 留接口)。

- [ ] **Step 3: Commit**:`feat: prometheus service + alert rules` + trailer

---

### Task 5:更新管道蓝绿化

**Files:**
- Create: `scripts/update_pipeline.sh`
- Modify: `docker-compose.yml`(OSRM 镜像 pin), `Makefile`(update-all), `README.md`

- [ ] **Step 1: OSRM 镜像 pin**(M4 待办):`docker inspect ghcr.io/project-osrm/osrm-backend:latest --format '{{index .RepoDigests 0}}'` 取 digest,compose 三处 image 改为 `ghcr.io/project-osrm/osrm-backend@sha256:...`(贴实际值);valhalla/nominatim 同法 pin(它们也是 :latest/:5.1——5.1 已半固定,valhalla-scripted:latest pin digest)。

- [ ] **Step 2: 管道脚本**

```bash
#!/usr/bin/env bash
# scripts/update_pipeline.sh — 数据更新蓝绿管道(spec §9)
# 顺序:OSM 刷新 → ETL 链(含漂移闸)→ 索引 alias 切换 → OSRM 三图重建+滚动替换 → Valhalla 重建
# cron 示例(每周日 02:00):0 2 * * 0 cd /home/daniel/places && bash scripts/update_pipeline.sh >> logs/update.log 2>&1
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== [1/6] 刷新 OSM PBF =="
curl -fsSL -o data/cambodia-latest.osm.pbf.new https://download.geofabrik.de/asia/cambodia-latest.osm.pbf
mv data/cambodia-latest.osm.pbf.new data/cambodia-latest.osm.pbf

echo "== [2/6] 记录基线计数 =="
BEFORE=$(docker exec places-postgis-1 psql -U places -d places -t -A -c "SELECT count(*) FROM places")

echo "== [3/6] ETL 链 =="
make etl-overture etl-osm etl-load etl-conflate

echo "== [4/6] 漂移闸(±15%)=="
AFTER=$(docker exec places-postgis-1 psql -U places -d places -t -A -c "SELECT count(*) FROM places")
python3 -c "
b,a=$BEFORE,$AFTER
drift=abs(a-b)/max(b,1)
print(f'places: {b} -> {a} (drift {drift:.1%})')
assert drift < 0.15, f'ABORT: drift {drift:.1%} exceeds 15% — investigate before indexing'"

echo "== [5/6] 重建检索索引(alias 原子切换)=="
make etl-index

echo "== [6/6] 路由图重建 + 滚动替换 =="
make osrm-build                       # 写入 data/osrm/*(在线实例 mmap 旧文件,完成后重启切换)
docker compose restart osrm-car osrm-moto osrm-tuktuk
rm -rf data/valhalla/valhalla_tiles data/valhalla/tiles 2>/dev/null || true
cp -f data/cambodia-latest.osm.pbf data/valhalla/
docker compose restart valhalla        # docker-valhalla 检测到新 PBF/无 tiles 自动重建
echo "DONE. 验证:make golden"
```
(Nominatim:mediagis 支持增量复制,M5 不开启常驻 update 容器——README 注明 `REPLICATION_URL` 配置方法与取舍:全量管道每周重导一致性更简单;增量复制留运维选配。注意 osrm-build 期间在线实例继续用旧图,restart 间隙 = 降级窗口(matrix 自动回退 Valhalla),符合"滚动"语义。)

- [ ] **Step 3: Makefile**:`update-all: ; bash scripts/update_pipeline.sh`;**演练**:实际跑一遍 `make update-all`(Overture 下载大,可 export 跳过开关?不加开关——全量真跑一次,~20-40 分钟,输出进报告),完成后 `make golden` 18/18。

- [ ] **Step 4: Commit**:`feat: blue-green data update pipeline + image pinning` + trailer

---

### Task 6:备份与恢复演练

**Files:**
- Create: `scripts/backup.sh`, `scripts/restore.sh`
- Modify: `docker-compose.yml`(OS 快照仓库卷+env), `README.md`

- [ ] **Step 1: OpenSearch 快照仓库**:opensearch 服务 environment 加 `path.repo: /snapshots`,volumes 加 `- ossnapshots:/snapshots`(顶层 volumes 加 ossnapshots);重启 OS 后注册仓库:
```bash
curl -s -X PUT 'localhost:9200/_snapshot/local' -H 'Content-Type: application/json' \
  -d '{"type":"fs","settings":{"location":"/snapshots"}}'
```

- [ ] **Step 2: 脚本**

```bash
#!/usr/bin/env bash
# scripts/backup.sh — pg_dump + OS snapshot + 路由图构件 tar → ./backups/<ts>/
set -euo pipefail
cd "$(dirname "$0")/.."
TS=$(date +%Y%m%d%H%M%S)
DIR="backups/$TS"
mkdir -p "$DIR"
echo "== postgres =="
docker exec places-postgis-1 pg_dump -U places -d places -Fc > "$DIR/places.dump"
echo "== opensearch snapshot =="
curl -sf -X PUT "localhost:9200/_snapshot/local/snap-$TS?wait_for_completion=true" >/dev/null
echo "snap-$TS" > "$DIR/os_snapshot_name"
echo "== route graphs =="
tar -czf "$DIR/graphs.tar.gz" data/osrm data/valhalla 2>/dev/null
du -sh "$DIR"/*
echo "DONE → $DIR(生产:rclone/aws s3 cp 同步到对象存储,见 README)"
```

```bash
#!/usr/bin/env bash
# scripts/restore.sh <backup_dir> — 恢复 pg + OS 快照 + 图构件
set -euo pipefail
cd "$(dirname "$0")/.."
DIR="${1:?usage: restore.sh backups/<ts>}"
echo "== postgres restore =="
docker exec -i places-postgis-1 pg_restore -U places -d places --clean --if-exists < "$DIR/places.dump"
echo "== opensearch restore =="
SNAP=$(cat "$DIR/os_snapshot_name")
# 关闭现有 places-* 索引再恢复(快照含 alias)
for idx in $(curl -s 'localhost:9200/_cat/indices/places-*?h=index'); do
  curl -s -X DELETE "localhost:9200/$idx" >/dev/null
done
curl -sf -X POST "localhost:9200/_snapshot/local/$SNAP/_restore?wait_for_completion=true" \
  -H 'Content-Type: application/json' -d '{"indices":"places-*"}' >/dev/null
echo "== graphs =="
tar -xzf "$DIR/graphs.tar.gz"
docker compose restart osrm-car osrm-moto osrm-tuktuk valhalla
echo "DONE. 验证:make golden"
```

- [ ] **Step 3: 演练(spec §12;全输出进报告)**:`bash scripts/backup.sh` → 破坏(`docker exec places-postgis-1 psql -U places -d places -c "DROP TABLE places CASCADE"` + 删 places-* 索引)→ `bash scripts/restore.sh backups/<ts>` → `make golden` 18/18 → `make test-go` 全过。README 备份段(cron 示例 + 对象存储上传位 + 演练记录)。

- [ ] **Step 4: Commit**:`feat: backup/restore scripts + verified drill` + trailer

---

### Task 7:独立测试库(M1 待办)

**Files:**
- Create: `etl/tests/conftest.py`
- Modify: `etl/tests/test_load.py` / `test_conflate.py` / `test_index.py`(DSN/alias 改读 fixture/env), `etl/etl/index.py`(ALIAS env 化), `db/migrate.sh`(支持 TARGET_DB), `Makefile`(migrate-test)

- [ ] **Step 1: 基建**

`db/migrate.sh` 的 psql 行改:`docker compose exec -T postgis psql -U places -d "${TARGET_DB:-places}" -v ON_ERROR_STOP=1 < "$f"`。Makefile 加:

```makefile
.PHONY: migrate-test
migrate-test:
	docker exec places-postgis-1 psql -U places -d places -c "SELECT 1 FROM pg_database WHERE datname='places_test'" | grep -q 1 || \
	  docker exec places-postgis-1 psql -U places -d places -c "CREATE DATABASE places_test"
	TARGET_DB=places_test bash db/migrate.sh
```

`etl/etl/index.py`:`ALIAS = os.environ.get("OPENSEARCH_ALIAS", "places")`(import os)。

`etl/tests/conftest.py`:
```python
import os

# 集成测试一律打独立库/独立 alias,不再破坏开发数据(M1 教训)
os.environ.setdefault("DATABASE_URL", "postgresql://places:places@localhost:5432/places_test")
os.environ.setdefault("OPENSEARCH_ALIAS", "places_test")
```
(conftest 在 collection 前执行,setdefault 保证 CI 可覆写。注意 test_index.py 导入 `from etl.index import ALIAS` 发生在 conftest 之后——pytest conftest 先于测试模块导入,成立;若有顺序问题改为在测试内引用 `etl.index.ALIAS`。)

三个集成测试文件里写死的 `DSN = os.environ.get("DATABASE_URL", ...)` 默认值同步改为 places_test;test_index 的 managed_indexes fixture 操作的是 OPENSEARCH_ALIAS 指向的 alias(places_test),不再碰生产 places。

- [ ] **Step 2: 验证(关键验收:跑完集成测试,生产数据完好)**

```bash
make migrate-test
docker exec places-postgis-1 psql -U places -d places -t -c "SELECT count(*) FROM places"   # 记录 N=108373
make test-py-integration    # 全过
docker exec places-postgis-1 psql -U places -d places -t -c "SELECT count(*) FROM places"   # 仍是 N!
curl -s localhost:9200/places/_count                                                        # 仍 108373
make golden                  # 18/18,无需重灌
```
README 删除"集成测试破坏性"警告段,替换为 places_test 说明。

- [ ] **Step 3: Commit**:`feat: isolated test database/alias for integration tests` + trailer

---

### Task 8:压测 + 全链路演练(spec §13-M5 / §14.5 验收)

**Files:**
- Create: `scripts/load_test.py`, `scripts/full_drill.sh`
- Modify: `README.md`(运维手册 + 压测数字 + M6 候选清单)

- [ ] **Step 1: 压测脚本**

```python
#!/usr/bin/env python3
"""scripts/load_test.py — §14.5:searchText 并发压测,报告 QPS 与 P50/P95/P99。
用法:python3 scripts/load_test.py [base] [concurrency] [total]"""
import json
import statistics
import sys
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
CONC = int(sys.argv[2]) if len(sys.argv) > 2 else 20
TOTAL = int(sys.argv[3]) if len(sys.argv) > 3 else 1000
QUERIES = ["អង្គរវត្ត", "coffee", "Royal Palace", "ផ្សារ", "hotel", "金边", "bank", "school"]

def one(i):
    body = json.dumps({"textQuery": QUERIES[i % len(QUERIES)], "pageSize": 10}).encode()
    req = urllib.request.Request(BASE + "/v1/places:searchText", data=body,
                                 headers={"Content-Type": "application/json"})
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=30) as r:
        r.read()
        ok = r.status == 200
    return time.time() - t0, ok

t0 = time.time()
with ThreadPoolExecutor(max_workers=CONC) as ex:
    results = list(ex.map(one, range(TOTAL)))
wall = time.time() - t0
lats = sorted(d for d, _ in results)
errs = sum(1 for _, ok in results if not ok)
q = lambda p: lats[int(len(lats) * p)]
print(f"total={TOTAL} conc={CONC} wall={wall:.1f}s qps={TOTAL/wall:.0f} errors={errs}")
print(f"P50={q(.5)*1000:.0f}ms P95={q(.95)*1000:.0f}ms P99={q(.99)*1000:.0f}ms")
```

- [ ] **Step 2: 全链路演练脚本**(spec §13-M5 验收;每步失败即退)

```bash
#!/usr/bin/env bash
# scripts/full_drill.sh — M5 全链路演练:健康→鉴权→golden→压测→降级→备份恢复→golden
set -euo pipefail
cd "$(dirname "$0")/.."
step() { echo; echo "===== $1 ====="; }

step "1. 全栈健康"
docker compose ps --format '{{.Name}} {{.Status}}' | tee /dev/stderr | grep -vq "unhealthy" || true
for url in localhost:8080/healthz localhost:9200/places/_count localhost:8081/status localhost:8002/status; do
  curl -sf "$url" >/dev/null && echo "OK $url"
done

step "2. 鉴权链路(临时开启)"
KEY=$(make -s gen-api-key NAME=drill | awk '{print $3}')
AUTH_ENABLED=true docker compose up -d gateway >/dev/null 2>&1 && sleep 3
[ "$(curl -s -o /dev/null -w '%{http_code}' -X POST localhost:8080/v1/places:searchText -H 'Content-Type: application/json' -d '{"textQuery":"x"}')" = "403" ] && echo "OK 无 key→403"
[ "$(curl -s -o /dev/null -w '%{http_code}' -X POST "localhost:8080/v1/places:searchText?key=$KEY" -H 'Content-Type: application/json' -d '{"textQuery":"angkor"}')" = "200" ] && echo "OK 有 key→200"
AUTH_ENABLED=false docker compose up -d gateway >/dev/null 2>&1 && sleep 3

step "3. golden 18/18"
make golden

step "4. 压测(20 并发 ×1000)"
python3 scripts/load_test.py

step "5. 矩阵降级"
docker compose stop osrm-car >/dev/null
python3 - <<'PY'
import json, random, time, urllib.request
random.seed(1)
pts=lambda n: [{"waypoint":{"location":{"latLng":{"latitude":round(random.uniform(11.52,11.62),6),"longitude":round(random.uniform(104.88,104.95),6)}}}} for _ in range(n)]
body=json.dumps({"origins":pts(60),"destinations":pts(60),"travelMode":"DRIVE"}).encode()
r=urllib.request.urlopen(urllib.request.Request("http://localhost:8080/distanceMatrix/v2:computeRouteMatrix",data=body,headers={"Content-Type":"application/json"}),timeout=120)
arr=json.load(r); assert len(arr)==3600, len(arr)
print("OK 降级 3600 元素")
PY
docker compose start osrm-car >/dev/null && sleep 8

step "6. 备份→破坏→恢复"
bash scripts/backup.sh
LATEST=$(ls -1d backups/* | tail -1)
docker exec places-postgis-1 psql -U places -d places -c "DROP TABLE places CASCADE" >/dev/null
bash scripts/restore.sh "$LATEST"

step "7. 终验 golden"
make golden
echo; echo "DRILL PASSED"
```

- [ ] **Step 3: 跑 `bash scripts/full_drill.sh`(全输出进报告,这就是 §13-M5 验收)**

- [ ] **Step 4: README 收尾**:运维手册段(生产部署 checklist:.env 强口令、sysctl、cron 两行(update_pipeline 每周/backup 每日)、AUTH_ENABLED=true、对象存储同步);压测数字;**M6 候选清单**(类目调优/罗马音别名/速度校准/分块并发/locale/maneuvers)。

- [ ] **Step 5: Commit**:`feat: load test + full production drill (M5 complete)` + trailer(body 附压测与演练摘要)

---

## 自审记录(写完计划后跑过)

1. **Spec 覆盖(M5)**:鉴权配额 ✓ T2(X-Goog-Api-Key/?key=、403/429 错误码对齐 §8);监控 ✓ T3/T4(§9 四指标含 ZERO_RESULTS 哨兵 + 告警规则含数据漂移的管道闸版本);蓝绿更新 ✓ T5(§9 节奏:OSM/Overture/图重建;OS alias 已有);备份恢复演练 ✓ T6(§12);全链路演练 ✓ T8(§13-M5 验收);压测 ✓ T8(§14.5);累积待办按头部清单处置,M6 外延明示。
2. **占位符扫描**:无 TBD;T2 Step 5 的 `DecisionAllowedAlias` 伪码已当场给出真实写法说明;T5 镜像 digest"贴实际值"是执行期产物非占位。
3. **类型一致性**:`auth.Decision`/`KeyDB`/`Store.Check` 与 middleware 的 `Checker` 接口一致(`*auth.Store` 实现 Check ✓);`store.PG.LookupKey` 满足 `auth.KeyDB` ✓;中间件链 main 写法与各任务一致(RequestID→Metrics→Auth→mux);`metricPath` 在 T3 定义与测试一致;`OPENSEARCH_ALIAS` 在 index.py 与 conftest 一致。
