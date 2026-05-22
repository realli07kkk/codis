// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
	"github.com/CodisLabs/codis/pkg/utils/bytesize"
)

func newHotKeyCacheTestConfig(keys ...string) *Config {
	config := newProxyConfig()
	config.BackendNumberDatabases = 1
	config.HotKeyCacheEnabled = true
	config.HotKeyCacheKeys = keys
	return config
}

func newHotKeyCacheTestRouter(t *testing.T, config *Config, addr string, keys ...string) *Router {
	t.Helper()
	router := NewRouter(config)
	seen := make(map[int]bool)
	for _, key := range keys {
		slot := int(Hash([]byte(key)) % MaxSlotNum)
		if seen[slot] {
			continue
		}
		seen[slot] = true
		if err := router.FillSlot(&models.Slot{
			Id:            slot,
			BackendAddr:   addr,
			ForwardMethod: models.ForwardSync,
		}); err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, func() bool {
		return router.pool.primary.Get(addr).BackendConn(0, 0, false) != nil
	})
	return router
}

func hotKeyCacheCall(t *testing.T, session *Session, router *Router, args ...string) *redis.Resp {
	t.Helper()
	multi := make([]*redis.Resp, len(args))
	for i := range args {
		multi[i] = redis.NewBulkBytes([]byte(args[i]))
	}
	r := &Request{
		Multi:    multi,
		Batch:    &sync.WaitGroup{},
		Database: session.getDatabase(),
	}
	if err := session.handleRequest(r, router); err != nil {
		t.Fatal(err)
	}
	resp, err := session.handleResponse(r)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHotKeyCacheDisabledKeepsGetPathUnchanged(t *testing.T) {
	var gets int
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) == "GET" {
			gets++
			return redistest.Bulk("value")
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	config := newProxyConfig()
	config.BackendNumberDatabases = 1
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot")
	defer router.Close()

	session := &Session{config: config}
	for i := 0; i < 2; i++ {
		resp := hotKeyCacheCall(t, session, router, "GET", "hot")
		if !resp.IsBulkBytes() || string(resp.Value) != "value" {
			t.Fatalf("GET resp = %v", resp)
		}
	}
	if gets != 2 {
		t.Fatalf("GET count = %d, want 2", gets)
	}
	if stats := router.hotKeyCache.Stats(); stats.Enabled || stats.Hits != 0 || stats.Stores != 0 {
		t.Fatalf("cache stats = %+v", stats)
	}
}

func TestHotKeyCacheGetHitMiss(t *testing.T) {
	var gets int
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) == "GET" {
			gets++
			return redistest.Bulk("value")
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	config := newHotKeyCacheTestConfig("hot")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot")
	defer router.Close()

	session := &Session{config: config}
	for i := 0; i < 2; i++ {
		resp := hotKeyCacheCall(t, session, router, "GET", "hot")
		if !resp.IsBulkBytes() || string(resp.Value) != "value" {
			t.Fatalf("GET resp = %v", resp)
		}
	}
	if gets != 1 {
		t.Fatalf("GET count = %d, want 1", gets)
	}
	stats := router.hotKeyCache.Stats()
	if stats.Entries != 1 || stats.Misses != 1 || stats.Hits != 1 || stats.Stores != 1 {
		t.Fatalf("cache stats = %+v", stats)
	}
}

func TestHotKeyCacheColdKeyBypassesCache(t *testing.T) {
	var gets int
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) == "GET" {
			gets++
			return redistest.Bulk("cold-value")
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	config := newHotKeyCacheTestConfig("hot")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "cold")
	defer router.Close()

	session := &Session{config: config}
	for i := 0; i < 2; i++ {
		resp := hotKeyCacheCall(t, session, router, "GET", "cold")
		if !resp.IsBulkBytes() || string(resp.Value) != "cold-value" {
			t.Fatalf("GET resp = %v", resp)
		}
	}
	if gets != 2 {
		t.Fatalf("GET count = %d, want 2", gets)
	}
	if stats := router.hotKeyCache.Stats(); stats.Entries != 0 || stats.Misses != 0 || stats.Hits != 0 {
		t.Fatalf("cache stats = %+v", stats)
	}
}

func TestHotKeyCacheDoesNotStoreUncacheableResponses(t *testing.T) {
	counts := make(map[string]int)
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "GET" {
			t.Fatalf("unexpected command: %v", args)
		}
		counts[args[1]]++
		switch args[1] {
		case "nil":
			return redis.NewBulkBytes(nil)
		case "err":
			return redistest.Error("ERR backend")
		case "large":
			return redistest.Bulk("large")
		default:
			t.Fatalf("unexpected key: %v", args)
			return nil
		}
	})

	config := newHotKeyCacheTestConfig("nil", "err", "large")
	config.HotKeyCacheMaxValueSize = bytesize.Int64(2)
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "nil", "err", "large")
	defer router.Close()

	session := &Session{config: config}
	for _, key := range []string{"nil", "err", "large"} {
		for i := 0; i < 2; i++ {
			_ = hotKeyCacheCall(t, session, router, "GET", key)
		}
		if counts[key] != 2 {
			t.Fatalf("GET %s count = %d, want 2", key, counts[key])
		}
	}
	if stats := router.hotKeyCache.Stats(); stats.Entries != 0 || stats.Hits != 0 || stats.Stores != 0 {
		t.Fatalf("cache stats = %+v", stats)
	}
}

func TestHotKeyCacheMGetPartialHit(t *testing.T) {
	mgetKeys := make(map[string]int)
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "GET":
			return redistest.Bulk("value-" + args[1])
		case "MGET":
			mgetKeys[args[1]]++
			return redistest.Array(redistest.Bulk("value-" + args[1]))
		default:
			t.Fatalf("unexpected command: %v", args)
			return nil
		}
	})

	config := newHotKeyCacheTestConfig("hot", "hot2")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot", "cold", "hot2")
	defer router.Close()

	session := &Session{config: config}
	resp := hotKeyCacheCall(t, session, router, "GET", "hot")
	if !resp.IsBulkBytes() || string(resp.Value) != "value-hot" {
		t.Fatalf("GET hot resp = %v", resp)
	}

	resp = hotKeyCacheCall(t, session, router, "MGET", "hot", "cold", "hot2")
	if !resp.IsArray() || len(resp.Array) != 3 {
		t.Fatalf("MGET resp = %v", resp)
	}
	for i, want := range []string{"value-hot", "value-cold", "value-hot2"} {
		if !resp.Array[i].IsBulkBytes() || string(resp.Array[i].Value) != want {
			t.Fatalf("MGET resp[%d] = %v, want %s", i, resp.Array[i], want)
		}
	}
	if mgetKeys["hot"] != 0 || mgetKeys["cold"] != 1 || mgetKeys["hot2"] != 1 {
		t.Fatalf("MGET backend keys = %v", mgetKeys)
	}
}

func TestHotKeyCacheSetInvalidatesLocalEntry(t *testing.T) {
	value := "v1"
	var gets int
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "GET":
			gets++
			return redistest.Bulk(value)
		case "SET":
			value = args[2]
			return redistest.OK()
		default:
			t.Fatalf("unexpected command: %v", args)
			return nil
		}
	})

	config := newHotKeyCacheTestConfig("hot")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot")
	defer router.Close()

	session := &Session{config: config}
	_ = hotKeyCacheCall(t, session, router, "GET", "hot")
	_ = hotKeyCacheCall(t, session, router, "GET", "hot")
	if gets != 1 {
		t.Fatalf("GET count before SET = %d, want 1", gets)
	}
	_ = hotKeyCacheCall(t, session, router, "SET", "hot", "v2")

	resp := hotKeyCacheCall(t, session, router, "GET", "hot")
	if !resp.IsBulkBytes() || string(resp.Value) != "v2" {
		t.Fatalf("GET after SET resp = %v", resp)
	}
	if gets != 2 {
		t.Fatalf("GET count after SET = %d, want 2", gets)
	}
	if stats := router.hotKeyCache.Stats(); stats.Invalidations == 0 {
		t.Fatalf("cache stats = %+v", stats)
	}
}

func TestHotKeyCacheInvalidatesAfterWriteResponse(t *testing.T) {
	var (
		mu         sync.Mutex
		value      = "v1"
		gets       int
		setStarted = make(chan struct{})
		releaseSet = make(chan struct{})
		once       sync.Once
	)

	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "GET":
			mu.Lock()
			gets++
			v := value
			mu.Unlock()
			return redistest.Bulk(v)
		case "SET":
			once.Do(func() { close(setStarted) })
			<-releaseSet
			mu.Lock()
			value = args[2]
			mu.Unlock()
			return redistest.OK()
		default:
			t.Fatalf("unexpected command: %v", args)
			return nil
		}
	})

	config := newHotKeyCacheTestConfig("hot")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot")
	defer router.Close()

	session := &Session{config: config}
	_ = hotKeyCacheCall(t, session, router, "GET", "hot")
	_ = hotKeyCacheCall(t, session, router, "GET", "hot")
	if stats := router.hotKeyCache.Stats(); stats.Entries != 1 || stats.Hits != 1 {
		t.Fatalf("cache stats before SET = %+v", stats)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp := hotKeyCacheCall(t, session, router, "SET", "hot", "v2")
		if !resp.IsString() {
			t.Errorf("SET resp = %v", resp)
		}
	}()

	select {
	case <-setStarted:
	case <-time.After(time.Second):
		t.Fatal("SET did not reach backend")
	}

	resp := hotKeyCacheCall(t, session, router, "GET", "hot")
	if !resp.IsBulkBytes() || string(resp.Value) != "v1" {
		t.Fatalf("GET while SET is in-flight resp = %v", resp)
	}
	mu.Lock()
	gotGets := gets
	mu.Unlock()
	if gotGets != 1 {
		t.Fatalf("GET count while SET is in-flight = %d, want 1", gotGets)
	}
	if stats := router.hotKeyCache.Stats(); stats.Entries != 1 {
		t.Fatalf("cache should not be invalidated before SET response: %+v", stats)
	}

	close(releaseSet)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SET did not finish")
	}

	if stats := router.hotKeyCache.Stats(); stats.Entries != 0 {
		t.Fatalf("cache should be invalidated after SET response: %+v", stats)
	}
	resp = hotKeyCacheCall(t, session, router, "GET", "hot")
	if !resp.IsBulkBytes() || string(resp.Value) != "v2" {
		t.Fatalf("GET after SET resp = %v", resp)
	}
}

func TestHotKeyCacheStaleMissTokenCannotStoreAfterInvalidation(t *testing.T) {
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	config := newHotKeyCacheTestConfig("hot")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot")
	defer router.Close()

	token := router.hotKeyCacheToken(0, []byte("hot"))
	if !token.ok {
		t.Fatal("expected cache token for hot key")
	}

	router.hotKeyCache.invalidateKey(0, []byte("hot"))
	router.hotKeyCacheStore(token, redis.NewBulkBytes([]byte("stale")))
	if stats := router.hotKeyCache.Stats(); stats.Entries != 0 {
		t.Fatalf("stale token should not store after invalidation: %+v", stats)
	}
}

func TestHotKeyCacheWriteInvalidationCommandGroups(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"setex", []string{"SETEX", "hot", "10", "v2"}},
		{"psetex", []string{"PSETEX", "hot", "1000", "v2"}},
		{"getset", []string{"GETSET", "hot", "v2"}},
		{"append", []string{"APPEND", "hot", "x"}},
		{"incr", []string{"INCR", "hot"}},
		{"incrby", []string{"INCRBY", "hot", "1"}},
		{"decr", []string{"DECR", "hot"}},
		{"decrby", []string{"DECRBY", "hot", "1"}},
		{"setbit", []string{"SETBIT", "hot", "0", "1"}},
		{"setrange", []string{"SETRANGE", "hot", "0", "v2"}},
		{"expire", []string{"EXPIRE", "hot", "10"}},
		{"expireat", []string{"EXPIREAT", "hot", "2000000000"}},
		{"pexpire", []string{"PEXPIRE", "hot", "1000"}},
		{"pexpireat", []string{"PEXPIREAT", "hot", "2000000000000"}},
		{"persist", []string{"PERSIST", "hot"}},
		{"mset", []string{"MSET", "hot", "v2"}},
		{"del", []string{"DEL", "hot"}},
		{"eval", []string{"EVAL", "return 1", "1", "hot"}},
		{"unknown-may-write", []string{"UNKNOWNWRITE", "hot"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := "v1"
			var gets int
			s := redistest.NewServer(t, func(args []string) *redistest.Resp {
				switch strings.ToUpper(args[0]) {
				case "GET":
					gets++
					return redistest.Bulk(value)
				case "MSET", "SETEX", "PSETEX", "GETSET", "APPEND", "SETBIT", "SETRANGE":
					value = "v2"
					return redistest.OK()
				case "DEL", "EXPIRE", "EXPIREAT", "PEXPIRE", "PEXPIREAT", "PERSIST",
					"INCR", "INCRBY", "DECR", "DECRBY", "EVAL", "UNKNOWNWRITE":
					value = "v2"
					return redistest.Int("1")
				default:
					t.Fatalf("unexpected command: %v", args)
					return nil
				}
			})

			config := newHotKeyCacheTestConfig("hot")
			router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot")
			defer router.Close()

			session := &Session{config: config}
			_ = hotKeyCacheCall(t, session, router, "GET", "hot")
			_ = hotKeyCacheCall(t, session, router, "GET", "hot")
			if gets != 1 {
				t.Fatalf("GET count before write = %d, want 1", gets)
			}

			_ = hotKeyCacheCall(t, session, router, tt.args...)
			resp := hotKeyCacheCall(t, session, router, "GET", "hot")
			if !resp.IsBulkBytes() || string(resp.Value) != "v2" {
				t.Fatalf("GET after %s resp = %v", tt.name, resp)
			}
			if gets != 2 {
				t.Fatalf("GET count after %s = %d, want 2", tt.name, gets)
			}
		})
	}
}

func TestHotKeyCacheTTLAndEviction(t *testing.T) {
	counts := make(map[string]int)
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "GET" {
			t.Fatalf("unexpected command: %v", args)
		}
		counts[args[1]]++
		return redistest.Bulk("value-" + args[1])
	})

	config := newHotKeyCacheTestConfig("a", "b")
	config.HotKeyCacheTTL.Set(20 * time.Millisecond)
	config.HotKeyCacheMaxEntries = 1
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "a", "b")
	defer router.Close()

	session := &Session{config: config}
	_ = hotKeyCacheCall(t, session, router, "GET", "a")
	_ = hotKeyCacheCall(t, session, router, "GET", "a")
	if counts["a"] != 1 {
		t.Fatalf("GET a count before TTL = %d, want 1", counts["a"])
	}
	time.Sleep(30 * time.Millisecond)
	_ = hotKeyCacheCall(t, session, router, "GET", "a")
	if counts["a"] != 2 {
		t.Fatalf("GET a count after TTL = %d, want 2", counts["a"])
	}

	_ = hotKeyCacheCall(t, session, router, "GET", "b")
	if stats := router.hotKeyCache.Stats(); stats.Entries != 1 || stats.Evictions == 0 {
		t.Fatalf("cache stats = %+v", stats)
	}
}

func TestHotKeyCacheSlotInvalidationAndMayWriteClear(t *testing.T) {
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "GET":
			return redistest.Bulk("value-" + args[1])
		case "EVAL":
			return redistest.Int("1")
		default:
			t.Fatalf("unexpected command: %v", args)
			return nil
		}
	})

	config := newHotKeyCacheTestConfig("hot", "hot2")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot", "hot2")
	defer router.Close()

	session := &Session{config: config}
	_ = hotKeyCacheCall(t, session, router, "GET", "hot")
	_ = hotKeyCacheCall(t, session, router, "GET", "hot2")
	if stats := router.hotKeyCache.Stats(); stats.Entries != 2 {
		t.Fatalf("cache stats before clear = %+v", stats)
	}

	_ = hotKeyCacheCall(t, session, router, "EVAL", "return 1", "1", "hot")
	if stats := router.hotKeyCache.Stats(); stats.Entries != 0 {
		t.Fatalf("cache stats after EVAL = %+v", stats)
	}

	_ = hotKeyCacheCall(t, session, router, "GET", "hot")
	slot := int(Hash([]byte("hot")) % MaxSlotNum)
	if err := router.FillSlot(&models.Slot{
		Id:            slot,
		BackendAddr:   s.Addr(),
		ForwardMethod: models.ForwardSync,
		Locked:        true,
	}); err != nil {
		t.Fatal(err)
	}
	if stats := router.hotKeyCache.Stats(); stats.Entries != 0 {
		t.Fatalf("cache stats after FillSlot = %+v", stats)
	}
	if _, _, ok := router.hotKeyCacheLookup(0, []byte("hot")); ok {
		t.Fatal("locked slot should not return hot key cache hit")
	}
}

func TestHotKeyCacheStatsAreExposedByProxyStats(t *testing.T) {
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) == "GET" {
			return redistest.Bulk("value")
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	config := newHotKeyCacheTestConfig("hot")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "hot")
	defer router.Close()

	session := &Session{config: config}
	_ = hotKeyCacheCall(t, session, router, "GET", "hot")
	_ = hotKeyCacheCall(t, session, router, "GET", "hot")

	proxy := &Proxy{config: config, router: router}
	stats := proxy.Stats(StatsFull)
	if stats.HotKeyCache == nil || !stats.HotKeyCache.Enabled || stats.HotKeyCache.Entries != 1 ||
		stats.HotKeyCache.Hits != 1 || stats.HotKeyCache.Misses != 1 {
		t.Fatalf("proxy stats hot key cache = %+v", stats.HotKeyCache)
	}
}

func TestHotKeyCacheStatsAreOmittedWhenDisabled(t *testing.T) {
	config := newProxyConfig()
	router := NewRouter(config)
	defer router.Close()

	proxy := &Proxy{config: config, router: router}
	stats := proxy.Stats(0)
	if stats.HotKeyCache != nil {
		t.Fatalf("disabled hot key cache stats should be omitted, got %+v", stats.HotKeyCache)
	}
}
