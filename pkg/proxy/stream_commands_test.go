// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"strings"
	"sync"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
)

func streamCall(t *testing.T, session *Session, router *Router, args ...string) *redis.Resp {
	t.Helper()
	r := &Request{
		Multi:    redisMultiBulk(args...),
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

func fillStreamSlot(t *testing.T, router *Router, key string, slot *models.Slot) {
	t.Helper()
	slot.Id = int(Hash([]byte(key)) % MaxSlotNum)
	if slot.ForwardMethod == 0 {
		slot.ForwardMethod = models.ForwardSync
	}
	if err := router.FillSlot(slot); err != nil {
		t.Fatal(err)
	}
}

func waitBackendConn(t *testing.T, router *Router, addr string, replica bool) {
	t.Helper()
	pool := router.pool.primary
	if replica {
		pool = router.pool.replica
	}
	waitUntil(t, func() bool {
		return pool.Get(addr).BackendConn(0, 0, false) != nil
	})
}

func streamRespErrorContains(t *testing.T, resp *redis.Resp, text string) {
	t.Helper()
	if resp == nil || !resp.IsError() || !strings.Contains(string(resp.Value), text) {
		t.Fatalf("resp = %v, want error containing %q", resp, text)
	}
}

func TestStreamResolveSingleAndContainerRoutes(t *testing.T) {
	tests := []struct {
		name     string
		opstr    string
		args     []string
		hashKey  string
		keyCount int
	}{
		{"xadd", "XADD", []string{"XADD", "mystream", "*", "f", "v"}, "mystream", 1},
		{"xgroup", "XGROUP", []string{"XGROUP", "CREATE", "mystream", "group-1", "0"}, "mystream", 1},
		{"xinfo", "XINFO", []string{"XINFO", "GROUPS", "mystream"}, "mystream", 1},
		{"xread", "XREAD", []string{"XREAD", "STREAMS", "mystream", "0-0"}, "mystream", 1},
		{"xreadgroup", "XREADGROUP", []string{"XREADGROUP", "GROUP", "g", "c", "STREAMS", "mystream", ">"}, "mystream", 1},
		{"xread-same-tag", "XREAD", []string{"XREAD", "STREAMS", "{u1}:a", "{u1}:b", "0-0", "0-0"}, "{u1}:a", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, resp := resolveStreamRoute(redisMultiBulk(tt.args...), tt.opstr)
			if resp != nil {
				t.Fatalf("unexpected resolver error: %s %q", resp.Type, resp.Value)
			}
			if string(route.hashKey) != tt.hashKey || len(route.keys) != tt.keyCount {
				t.Fatalf("route = %+v, want hashKey=%q keyCount=%d", route, tt.hashKey, tt.keyCount)
			}
		})
	}
}

func TestStreamResolveRejectsUnsafeForms(t *testing.T) {
	tests := []struct {
		name  string
		opstr string
		args  []string
		want  string
	}{
		{"xread-block", "XREAD", []string{"XREAD", "BLOCK", "1000", "STREAMS", "mystream", "0-0"}, "unsupported blocking Stream command"},
		{"xreadgroup-block", "XREADGROUP", []string{"XREADGROUP", "GROUP", "g", "c", "BLOCK", "1000", "STREAMS", "mystream", ">"}, "unsupported blocking Stream command"},
		{"xread-cross-tag", "XREAD", []string{"XREAD", "STREAMS", "{u1}:a", "{u2}:b", "0-0", "0-0"}, "CROSSSLOT"},
		{"xread-key-id-mismatch", "XREAD", []string{"XREAD", "STREAMS", "a", "b", "0-0"}, "keys and IDs must be paired"},
		{"xgroup-help", "XGROUP", []string{"XGROUP", "HELP"}, "unsupported Stream subcommand"},
		{"xinfo-help", "XINFO", []string{"XINFO", "HELP"}, "unsupported Stream subcommand"},
		{"xgroup-unknown", "XGROUP", []string{"XGROUP", "UNKNOWN", "mystream"}, "unsupported Stream subcommand"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, resp := resolveStreamRoute(redisMultiBulk(tt.args...), tt.opstr)
			streamRespErrorContains(t, resp, tt.want)
		})
	}
}

func TestStreamResolveUnsupportedSubcommandDoesNotEchoBulk(t *testing.T) {
	huge := strings.Repeat("x", 4096)
	_, resp := resolveStreamRoute(redisMultiBulk("XGROUP", huge, "mystream"), "XGROUP")
	streamRespErrorContains(t, resp, "unsupported Stream subcommand")
	if strings.Contains(string(resp.Value), huge) {
		t.Fatalf("unsupported subcommand response echoed client bulk")
	}
}

func TestSessionStreamContainerDispatchUsesStreamKey(t *testing.T) {
	streamKey := "mystream"
	for Hash([]byte(streamKey))%MaxSlotNum == Hash([]byte("CREATE"))%MaxSlotNum {
		streamKey += "x"
	}

	expected := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "XGROUP" || args[2] != streamKey {
			t.Fatalf("unexpected stream backend command: %v", args)
		}
		return redistest.OK()
	})
	wrong := redistest.NewServer(t, func(args []string) *redistest.Resp {
		t.Fatalf("XGROUP routed by subcommand instead of stream key: %v", args)
		return nil
	})

	config := newProxyConfig()
	config.BackendNumberDatabases = 1
	router := NewRouter(config)
	defer router.Close()
	fillStreamSlot(t, router, streamKey, &models.Slot{BackendAddr: expected.Addr()})
	fillStreamSlot(t, router, "CREATE", &models.Slot{BackendAddr: wrong.Addr()})
	waitBackendConn(t, router, expected.Addr(), false)
	waitBackendConn(t, router, wrong.Addr(), false)

	session := &Session{config: config}
	resp := streamCall(t, session, router, "XGROUP", "CREATE", streamKey, "group-1", "0")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("XGROUP resp = %s %q", resp.Type, resp.Value)
	}
	if expected.CountCommand("XGROUP") != 1 || wrong.CountCommand("XGROUP") != 0 {
		t.Fatalf("backend counts: expected=%d wrong=%d", expected.CountCommand("XGROUP"), wrong.CountCommand("XGROUP"))
	}
}

func TestSessionStreamSingleKeyBackendErrorPassThrough(t *testing.T) {
	key := "mystream"
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "XADD" || args[1] != key {
			t.Fatalf("unexpected backend command: %v", args)
		}
		return redistest.Error("ERR unknown command 'XADD'")
	})

	config := newProxyConfig()
	config.BackendNumberDatabases = 1
	router := NewRouter(config)
	defer router.Close()
	fillStreamSlot(t, router, key, &models.Slot{BackendAddr: s.Addr()})
	waitBackendConn(t, router, s.Addr(), false)

	session := &Session{config: config}
	resp := streamCall(t, session, router, "XADD", key, "*", "f", "v")
	streamRespErrorContains(t, resp, "unknown command")
	if s.CountCommand("XADD") != 1 {
		t.Fatalf("XADD backend count = %d, want 1", s.CountCommand("XADD"))
	}
}

func TestSessionStreamReadWriteReplicaMasterRouting(t *testing.T) {
	key := "mystream"
	var primaryCommands, replicaCommands int
	primary := redistest.NewServer(t, func(args []string) *redistest.Resp {
		primaryCommands++
		switch strings.ToUpper(args[0]) {
		case "XREADGROUP":
			return redistest.Array()
		default:
			t.Fatalf("unexpected primary command: %v", args)
			return nil
		}
	})
	replica := redistest.NewServer(t, func(args []string) *redistest.Resp {
		replicaCommands++
		switch strings.ToUpper(args[0]) {
		case "XLEN":
			return redistest.Int("0")
		default:
			t.Fatalf("unexpected replica command: %v", args)
			return nil
		}
	})

	config := newProxyConfig()
	config.BackendNumberDatabases = 1
	router := NewRouter(config)
	defer router.Close()
	fillStreamSlot(t, router, key, &models.Slot{
		BackendAddr:        primary.Addr(),
		ReplicaGroups:      [][]string{{replica.Addr()}},
		ForwardMethod:      models.ForwardSync,
		BackendAddrGroupId: 1,
	})
	waitBackendConn(t, router, primary.Addr(), false)
	waitBackendConn(t, router, replica.Addr(), true)

	session := &Session{config: config}
	if resp := streamCall(t, session, router, "XLEN", key); !resp.IsInt() {
		t.Fatalf("XLEN resp = %s %q", resp.Type, resp.Value)
	}
	if resp := streamCall(t, session, router, "XREADGROUP", "GROUP", "g", "c", "STREAMS", key, ">"); !resp.IsArray() {
		t.Fatalf("XREADGROUP resp = %s %q", resp.Type, resp.Value)
	}
	if primaryCommands != 1 || replicaCommands != 1 {
		t.Fatalf("primaryCommands=%d replicaCommands=%d, want 1/1", primaryCommands, replicaCommands)
	}
}

func TestSessionStreamReadRejectsWithoutBackendDispatch(t *testing.T) {
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		t.Fatalf("rejected Stream command reached backend: %v", args)
		return nil
	})

	config := newProxyConfig()
	config.BackendNumberDatabases = 1
	router := NewRouter(config)
	defer router.Close()
	fillStreamSlot(t, router, "{u1}:a", &models.Slot{BackendAddr: s.Addr()})
	fillStreamSlot(t, router, "{u2}:b", &models.Slot{BackendAddr: s.Addr()})
	waitBackendConn(t, router, s.Addr(), false)

	session := &Session{config: config}
	streamRespErrorContains(t,
		streamCall(t, session, router, "XREAD", "BLOCK", "1000", "STREAMS", "{u1}:a", "0-0"),
		"unsupported blocking Stream command",
	)
	streamRespErrorContains(t,
		streamCall(t, session, router, "XREAD", "STREAMS", "{u1}:a", "{u2}:b", "0-0", "0-0"),
		"CROSSSLOT",
	)
	if s.CountCommand("XREAD") != 0 {
		t.Fatalf("rejected XREAD reached backend %d times", s.CountCommand("XREAD"))
	}
}

func TestSessionStreamMigrationUsesResolvedStreamKey(t *testing.T) {
	key := "mystream"
	source := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "SLOTSMGRT-EXEC-WRAPPER" {
			t.Fatalf("unexpected source command: %v", args)
		}
		if args[1] != key {
			t.Fatalf("migration hkey = %q, want %q; args=%v", args[1], key, args)
		}
		if len(args) < 5 || strings.ToUpper(args[2]) != "XGROUP" || strings.ToUpper(args[3]) != "CREATE" || args[4] != key {
			t.Fatalf("wrapped command = %v", args)
		}
		return redistest.Array(redistest.Int("2"), redistest.OK())
	})
	target := redistest.NewServer(t, func(args []string) *redistest.Resp {
		t.Fatalf("target should not receive wrapped Stream command: %v", args)
		return nil
	})

	config := newProxyConfig()
	config.BackendNumberDatabases = 1
	router := NewRouter(config)
	defer router.Close()
	fillStreamSlot(t, router, key, &models.Slot{
		BackendAddr:   target.Addr(),
		MigrateFrom:   source.Addr(),
		ForwardMethod: models.ForwardSemiAsync,
	})
	waitBackendConn(t, router, source.Addr(), false)
	waitBackendConn(t, router, target.Addr(), false)

	session := &Session{config: config}
	resp := streamCall(t, session, router, "XGROUP", "CREATE", key, "group-1", "0")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("XGROUP resp = %s %q", resp.Type, resp.Value)
	}
}

func TestStreamWriteInvalidatesHotKeyCacheByResolvedKey(t *testing.T) {
	value := "v1"
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "GET":
			return redistest.Bulk(value)
		case "XGROUP":
			if strings.ToUpper(args[1]) != "CREATE" || args[2] != "mystream" {
				t.Fatalf("unexpected XGROUP args: %v", args)
			}
			value = "v2"
			return redistest.OK()
		default:
			t.Fatalf("unexpected command: %v", args)
			return nil
		}
	})

	config := newHotKeyCacheTestConfig("mystream")
	router := newHotKeyCacheTestRouter(t, config, s.Addr(), "mystream")
	defer router.Close()

	session := &Session{config: config}
	_ = streamCall(t, session, router, "GET", "mystream")
	_ = streamCall(t, session, router, "GET", "mystream")
	if s.CountCommand("GET") != 1 {
		t.Fatalf("GET count before Stream write = %d, want 1", s.CountCommand("GET"))
	}

	resp := streamCall(t, session, router, "XGROUP", "CREATE", "mystream", "group-1", "0")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("XGROUP resp = %s %q", resp.Type, resp.Value)
	}
	resp = streamCall(t, session, router, "GET", "mystream")
	if !resp.IsBulkBytes() || string(resp.Value) != "v2" {
		t.Fatalf("GET after XGROUP resp = %s %q", resp.Type, resp.Value)
	}
	if s.CountCommand("GET") != 2 {
		t.Fatalf("GET count after Stream write = %d, want 2", s.CountCommand("GET"))
	}
}
