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
