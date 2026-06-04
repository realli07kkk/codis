// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
)

func TestConfigRejectsNegativeProxyQPSLimit(t *testing.T) {
	config := NewDefaultConfig()
	config.ProxyQPSLimit = -1
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "proxy_qps_limit") {
		t.Fatalf("Validate() error = %v, want proxy_qps_limit", err)
	}
}

func TestQPSLimiterDisabledAllowsRequests(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newTestQPSLimiter(t, 0, now)
	for i := 0; i < 10; i++ {
		if !limiter.Allow(now) {
			t.Fatal("disabled limiter rejected request")
		}
	}
	stats := limiter.Stats()
	if stats.Enabled || stats.Limit != 0 || stats.Accepted != 0 || stats.Rejected != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestQPSLimiterAllowsAndRejectsByTokenBudget(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newTestQPSLimiter(t, 2, now)

	if !limiter.Allow(now) || !limiter.Allow(now) {
		t.Fatal("initial burst should allow two requests")
	}
	if limiter.Allow(now) {
		t.Fatal("third request should be rejected")
	}
	if !limiter.Allow(now.Add(500 * time.Millisecond)) {
		t.Fatal("half-second refill should allow one request")
	}
	if limiter.Allow(now.Add(500 * time.Millisecond)) {
		t.Fatal("budget should be empty again")
	}

	stats := limiter.Stats()
	if !stats.Enabled || stats.Limit != 2 || stats.Accepted != 3 || stats.Rejected != 2 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestQPSLimiterSetLimitClampsBurst(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newTestQPSLimiter(t, 10, now)
	if err := limiter.SetLimit(1, 2, now); err != nil {
		t.Fatal(err)
	}

	if !limiter.Allow(now) || !limiter.Allow(now) {
		t.Fatal("clamped budget should allow two requests")
	}
	if limiter.Allow(now) {
		t.Fatal("lowered limit should not keep old burst")
	}

	stats := limiter.Stats()
	if stats.Revision != 1 || stats.Limit != 2 || stats.Accepted != 2 || stats.Rejected != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestQPSLimiterSetLimitZeroDisables(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newTestQPSLimiter(t, 1, now)
	if !limiter.Allow(now) {
		t.Fatal("first request should pass")
	}
	if limiter.Allow(now) {
		t.Fatal("second request should be rejected before disabling")
	}
	if err := limiter.SetLimit(2, 0, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if !limiter.Allow(now) {
			t.Fatal("disabled limiter rejected request")
		}
	}

	stats := limiter.Stats()
	if stats.Enabled || stats.Revision != 2 || stats.Limit != 0 || stats.Accepted != 1 || stats.Rejected != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestSessionRejectsOrdinaryRequestWhenQPSLimitExceeded(t *testing.T) {
	config := newProxyConfig()
	config.ProxyQPSLimit = 1
	router := NewRouter(config)
	session := &Session{config: config}

	resp := qpsLimitSessionCall(t, session, router, "SELECT", "0")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("first SELECT resp = %s %q", resp.Type, resp.Value)
	}
	resp = qpsLimitSessionCall(t, session, router, "SELECT", "0")
	if !resp.IsError() || string(resp.Value) != qpsLimitExceededMessage {
		t.Fatalf("second SELECT resp = %s %q", resp.Type, resp.Value)
	}

	stats := router.qpsLimiter.Stats()
	if stats.Accepted != 1 || stats.Rejected != 1 {
		t.Fatalf("qps limit stats = %+v", stats)
	}
}

func TestSessionBypassesQPSLimitForAuthAndQuit(t *testing.T) {
	config := newProxyConfig()
	config.SessionAuth = "secret"
	config.ProxyQPSLimit = 1
	router := NewRouter(config)
	session := &Session{config: config}

	resp := qpsLimitSessionCall(t, session, router, "AUTH", "secret")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("AUTH resp = %s %q", resp.Type, resp.Value)
	}
	resp = qpsLimitSessionCall(t, session, router, "SELECT", "0")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("SELECT resp = %s %q", resp.Type, resp.Value)
	}
	resp = qpsLimitSessionCall(t, session, router, "AUTH", "secret")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("AUTH after budget exhausted resp = %s %q", resp.Type, resp.Value)
	}
	resp = qpsLimitSessionCall(t, session, router, "QUIT")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("QUIT resp = %s %q", resp.Type, resp.Value)
	}

	stats := router.qpsLimiter.Stats()
	if stats.Accepted != 1 || stats.Rejected != 0 {
		t.Fatalf("qps limit stats = %+v", stats)
	}
}

func TestProxySetQPSLimitAPI(t *testing.T) {
	config := newProxyConfig()
	proxy, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	client := NewApiClient(proxy.Model().AdminAddr)
	client.SetXAuth(config.ProductName, config.ProductAuth, proxy.Model().Token)
	if err := client.SetQPSLimit(3, 5); err != nil {
		t.Fatal(err)
	}
	stats, err := client.Stats(0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.QPSLimit == nil || stats.QPSLimit.Revision != 3 || stats.QPSLimit.Limit != 5 || !stats.QPSLimit.Enabled {
		t.Fatalf("qps limit stats = %+v", stats.QPSLimit)
	}

	if err := client.SetQPSLimit(4, 0); err != nil {
		t.Fatal(err)
	}
	stats, err = client.Stats(0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.QPSLimit == nil || stats.QPSLimit.Revision != 4 || stats.QPSLimit.Limit != 0 || stats.QPSLimit.Enabled {
		t.Fatalf("qps limit stats after disable = %+v", stats.QPSLimit)
	}
}

func TestProxyDefaultQPSLimitStatsHiddenAfterDisabledAllow(t *testing.T) {
	config := newProxyConfig()
	proxy, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	if !proxy.router.qpsLimiter.Allow(time.Now()) {
		t.Fatal("disabled qps limiter rejected request")
	}
	if stats := proxy.Stats(0); stats.QPSLimit != nil {
		t.Fatalf("default qps limit stats should be hidden, got %+v", stats.QPSLimit)
	}
}

func TestProxyResetStatsClearsQPSLimitCounters(t *testing.T) {
	config := newProxyConfig()
	proxy, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	client := NewApiClient(proxy.Model().AdminAddr)
	client.SetXAuth(config.ProductName, config.ProductAuth, proxy.Model().Token)
	if err := client.SetQPSLimit(3, 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if !proxy.router.qpsLimiter.Allow(now) {
		t.Fatal("first request should pass")
	}
	if proxy.router.qpsLimiter.Allow(now) {
		t.Fatal("second request should be rejected")
	}

	stats, err := client.Stats(0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.QPSLimit == nil || stats.QPSLimit.Accepted != 1 || stats.QPSLimit.Rejected != 1 {
		t.Fatalf("qps limit stats before reset = %+v", stats.QPSLimit)
	}
	if err := client.ResetStats(); err != nil {
		t.Fatal(err)
	}
	stats, err = client.Stats(0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.QPSLimit == nil || stats.QPSLimit.Revision != 3 || stats.QPSLimit.Limit != 1 ||
		stats.QPSLimit.Accepted != 0 || stats.QPSLimit.Rejected != 0 {
		t.Fatalf("qps limit stats after reset = %+v", stats.QPSLimit)
	}
}

func TestProxySetQPSLimitAPIRejectsNegativeLimit(t *testing.T) {
	config := newProxyConfig()
	proxy, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	client := NewApiClient(proxy.Model().AdminAddr)
	client.SetXAuth(config.ProductName, config.ProductAuth, proxy.Model().Token)
	if err := client.SetQPSLimit(1, -1); err == nil {
		t.Fatal("expected invalid proxy_qps_limit")
	}
}

func TestQPSRejectDoesNotCountRedisError(t *testing.T) {
	ResetStats()
	defer ResetStats()

	config := newProxyConfig()
	config.ProxyQPSLimit = 1
	router := NewRouter(config)
	session := &Session{config: config}

	accepted, resp := qpsLimitSessionRequest(t, session, router, "SELECT", "0")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("first SELECT resp = %s %q", resp.Type, resp.Value)
	}
	session.incrOpStats(accepted, resp.Type)

	rejected, resp := qpsLimitSessionRequest(t, session, router, "SELECT", "0")
	if !resp.IsError() || string(resp.Value) != qpsLimitExceededMessage {
		t.Fatalf("second SELECT resp = %s %q", resp.Type, resp.Value)
	}
	session.incrOpStats(rejected, resp.Type)
	session.flushOpStats(true)

	if OpFails() != 1 {
		t.Fatalf("OpFails() = %d, want 1", OpFails())
	}
	if OpRedisErrors() != 0 {
		t.Fatalf("OpRedisErrors() = %d, want 0", OpRedisErrors())
	}
	stats := router.qpsLimiter.Stats()
	if stats.Rejected != 1 {
		t.Fatalf("qps limit stats = %+v", stats)
	}
}

func newTestQPSLimiter(t *testing.T, limit int64, now time.Time) *QPSLimiter {
	t.Helper()
	limiter := &QPSLimiter{}
	if err := limiter.SetLimit(0, limit, now); err != nil {
		t.Fatal(err)
	}
	return limiter
}

func qpsLimitSessionCall(t *testing.T, session *Session, router *Router, args ...string) *redis.Resp {
	t.Helper()
	_, resp := qpsLimitSessionRequest(t, session, router, args...)
	return resp
}

func qpsLimitSessionRequest(t *testing.T, session *Session, router *Router, args ...string) (*Request, *redis.Resp) {
	t.Helper()
	multi := make([]*redis.Resp, len(args))
	for i := range args {
		multi[i] = redis.NewBulkBytes([]byte(args[i]))
	}
	r := &Request{
		Multi:    multi,
		Batch:    &sync.WaitGroup{},
		Database: session.getDatabase(),
		UnixNano: time.Now().UnixNano(),
	}
	if err := session.handleRequest(r, router); err != nil {
		t.Fatal(err)
	}
	resp, err := session.handleResponse(r)
	if err != nil {
		t.Fatal(err)
	}
	return r, resp
}
