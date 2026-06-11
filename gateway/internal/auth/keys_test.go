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
