// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
)

const redis8TestSlotID = 42

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not satisfied before timeout")
}

func TestBackendRedis8AuthAndSelect(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		dec := redis.NewDecoder(server)
		enc := redis.NewEncoder(server)
		for _, expected := range [][]string{
			{"AUTH", "secret"},
			{"SELECT", "2"},
		} {
			r, err := dec.Decode()
			if err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			if len(r.Array) != len(expected) {
				t.Errorf("request length = %d, want %d", len(r.Array), len(expected))
				return
			}
			for i := range expected {
				if got := string(r.Array[i].Value); got != expected[i] {
					t.Errorf("request[%d] = %q, want %q", i, got, expected[i])
					return
				}
			}
			if err := enc.Encode(redis.NewString([]byte("OK")), true); err != nil {
				t.Errorf("encode response: %v", err)
				return
			}
		}
	}()

	conn := redis.NewConn(client, 128*1024, 128*1024)
	bc := &BackendConn{}
	if err := bc.verifyAuth(conn, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := bc.selectDatabase(conn, 2); err != nil {
		t.Fatal(err)
	}
}

func TestBackendRedis8InfoKeepAliveRecoversOnlyWhenReady(t *testing.T) {
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "INFO":
			return redistest.Bulk("loading:0\r\nmaster_link_status:up\r\n")
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})
	config := NewDefaultConfig()
	config.BackendNumberDatabases = 1
	bc := NewBackendConn(s.Addr(), 0, config)
	defer bc.Close()

	waitUntil(t, bc.IsConnected)
	bc.state.Set(stateDataStale)
	if !bc.KeepAlive() {
		t.Fatal("KeepAlive returned false")
	}
	waitUntil(t, func() bool {
		return bc.IsConnected() && s.CountCommand("INFO") != 0
	})
}

func TestBackendRedis8InfoKeepAliveKeepsLoadingBackendStale(t *testing.T) {
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "INFO":
			return redistest.Bulk("loading:1\r\nmaster_link_status:up\r\n")
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})
	config := NewDefaultConfig()
	config.BackendNumberDatabases = 1
	bc := NewBackendConn(s.Addr(), 0, config)
	defer bc.Close()

	waitUntil(t, bc.IsConnected)
	bc.state.Set(stateDataStale)
	if !bc.KeepAlive() {
		t.Fatal("KeepAlive returned false")
	}
	waitUntil(t, func() bool {
		return s.CountCommand("INFO") != 0
	})
	if bc.state.Int64() != stateDataStale {
		t.Fatalf("backend state = %d, want stateDataStale", bc.state.Int64())
	}
}

func TestSessionRedis8SlotsInfoAndSlotsScanDispatch(t *testing.T) {
	s := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "SLOTSINFO":
			if len(args) != 1 {
				t.Fatalf("SLOTSINFO backend args = %v", args)
			}
			return redistest.Array()
		case "SLOTSSCAN":
			return redistest.Array(redistest.Bulk("0"), redistest.Array())
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	config := NewDefaultConfig()
	config.BackendNumberDatabases = 1
	router := NewRouter(config)
	defer router.Close()
	if err := router.FillSlot(&models.Slot{
		Id:            redis8TestSlotID,
		BackendAddr:   s.Addr(),
		ForwardMethod: models.ForwardSync,
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		return router.pool.primary.Get(s.Addr()).BackendConn(0, 0, false) != nil
	})

	session := &Session{}
	r1 := &Request{
		Multi: []*redis.Resp{
			redis.NewBulkBytes([]byte("SLOTSINFO")),
			redis.NewBulkBytes([]byte(s.Addr())),
		},
		Batch: &sync.WaitGroup{},
	}
	if err := session.handleRequestSlotsInfo(r1, router); err != nil {
		t.Fatal(err)
	}
	r1.Batch.Wait()
	if r1.Err != nil {
		t.Fatal(r1.Err)
	}
	if r1.Resp == nil || !r1.Resp.IsArray() {
		t.Fatalf("SLOTSINFO resp = %v", r1.Resp)
	}

	r2 := &Request{
		Multi: []*redis.Resp{
			redis.NewBulkBytes([]byte("SLOTSSCAN")),
			redis.NewBulkBytes([]byte(strconv.Itoa(redis8TestSlotID))),
			redis.NewBulkBytes([]byte("0")),
			redis.NewBulkBytes([]byte("COUNT")),
			redis.NewBulkBytes([]byte("10")),
		},
		Batch: &sync.WaitGroup{},
	}
	if err := session.handleRequestSlotsScan(r2, router); err != nil {
		t.Fatal(err)
	}
	r2.Batch.Wait()
	if r2.Err != nil {
		t.Fatal(r2.Err)
	}
	if r2.Resp == nil || !r2.Resp.IsArray() {
		t.Fatalf("SLOTSSCAN resp = %v", r2.Resp)
	}
}

func TestForwardHelperRedis8ExecWrapperResponses(t *testing.T) {
	type wrapperReply struct {
		code  string
		reply *redis.Resp
	}
	replies := []wrapperReply{
		{code: "0", reply: redis.NewError([]byte("the specified key doesn't exist"))},
		{code: "1", reply: redis.NewError([]byte("the specified key is being migrated"))},
		{code: "2", reply: redistest.Bulk("PONG")},
	}
	var mu sync.Mutex
	source := redistest.NewServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "SLOTSMGRT-EXEC-WRAPPER" {
			t.Fatalf("unexpected command: %v", args)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(replies) == 0 {
			t.Fatalf("no wrapper reply left for %v", args)
		}
		reply := replies[0]
		replies = replies[1:]
		return redistest.Array(redistest.Int(reply.code), reply.reply)
	})
	target := redistest.NewServer(t, func(args []string) *redistest.Resp {
		t.Fatalf("target should not receive wrapper test commands: %v", args)
		return nil
	})

	config := NewDefaultConfig()
	config.BackendNumberDatabases = 1
	router := NewRouter(config)
	defer router.Close()
	if err := router.FillSlot(&models.Slot{
		Id:            redis8TestSlotID,
		BackendAddr:   target.Addr(),
		MigrateFrom:   source.Addr(),
		ForwardMethod: models.ForwardSemiAsync,
	}); err != nil {
		t.Fatal(err)
	}

	helper := &forwardHelper{}
	multi := []*redis.Resp{redis.NewBulkBytes([]byte("PING"))}
	resp, moved, err := helper.slotsmgrtExecWrapper(&router.slots[redis8TestSlotID], []byte("key"), 0, 0, multi)
	if err != nil {
		t.Fatal(err)
	}
	if !moved || resp != nil {
		t.Fatalf("code 0 => resp=%v moved=%v", resp, moved)
	}

	resp, moved, err = helper.slotsmgrtExecWrapper(&router.slots[redis8TestSlotID], []byte("key"), 0, 0, multi)
	if err != nil {
		t.Fatal(err)
	}
	if moved || resp != nil {
		t.Fatalf("code 1 => resp=%v moved=%v", resp, moved)
	}

	resp, moved, err = helper.slotsmgrtExecWrapper(&router.slots[redis8TestSlotID], []byte("key"), 0, 0, multi)
	if err != nil {
		t.Fatal(err)
	}
	if moved || resp == nil || string(resp.Value) != "PONG" {
		t.Fatalf("code 2 => resp=%v moved=%v", resp, moved)
	}
}
