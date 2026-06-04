// Copyright 2026 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	proxyredis "github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
)

func TestSentinelSubscribeCommandWaitsForAckAndSwitchMaster(t *testing.T) {
	server := newSentinelPubSubServer(t, "codis")
	sentinel := NewSentinel("codis", "")
	client, err := NewClientNoAuth(server.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}

	var subscribed bool
	if err := sentinel.subscribeCommand(client, server.Addr(), func() {
		subscribed = true
	}); err != nil {
		t.Fatal(err)
	}
	if !subscribed {
		t.Fatal("onSubscribed was not called")
	}
	if !redisCommandExistsFold(server.Commands(), []string{"SUBSCRIBE", "+switch-master"}) {
		t.Fatalf("SUBSCRIBE command not observed: %v", server.Commands())
	}
}

func TestSentinelMastersAndSlavesParseStringMaps(t *testing.T) {
	server := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "SENTINEL" || len(args) < 2 {
			t.Fatalf("unexpected command: %v", args)
		}
		switch strings.ToLower(args[1]) {
		case "masters":
			return redistest.Array(
				sentinelStringMap("name", "codis-1", "ip", "127.0.0.1", "port", "6379", "config-epoch", "9"),
				sentinelStringMap("name", "other-2", "ip", "127.0.0.2", "port", "6380", "config-epoch", "1"),
			)
		case "get-master-addr-by-name":
			if args[2] == "codis-1" {
				return redistest.Array(redistest.Bulk("127.0.0.1"), redistest.Bulk("6379"))
			}
			return proxyredis.NewArray(nil)
		case "slaves":
			if args[2] != "codis-1" {
				t.Fatalf("unexpected slaves command: %v", args)
			}
			return redistest.Array(
				sentinelStringMap("name", "slave-1", "ip", "127.0.0.3", "port", "6381"),
			)
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	sentinel := NewSentinel("codis", "")
	client, err := NewClientNoAuth(server.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	groups, err := sentinel.MastersAndSlavesClient(client)
	if err != nil {
		t.Fatal(err)
	}
	group := groups["codis-1"]
	if group == nil {
		t.Fatalf("codis-1 group missing: %v", groups)
	}
	if group.Master["ip"] != "127.0.0.1" || group.Master["config-epoch"] != "9" {
		t.Fatalf("master = %v", group.Master)
	}
	if len(group.Slaves) != 1 || group.Slaves[0]["name"] != "slave-1" {
		t.Fatalf("slaves = %v", group.Slaves)
	}
	if _, ok := groups["other-2"]; ok {
		t.Fatalf("foreign product should be ignored: %v", groups)
	}
}

func TestSentinelMonitorRemoveAndFlushConfigCommands(t *testing.T) {
	server := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "SENTINEL" || len(args) < 2 {
			t.Fatalf("unexpected command: %v", args)
		}
		switch strings.ToLower(args[1]) {
		case "get-master-addr-by-name":
			return redistest.Array(redistest.Bulk("127.0.0.1"), redistest.Bulk("6379"))
		case "remove", "monitor", "set", "flushconfig":
			return redistest.OK()
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	sentinel := NewSentinel("codis", "secret")
	client, err := NewClientNoAuth(server.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = sentinel.monitorGroupsCommand(client, server.Addr(), &MonitorConfig{
		Quorum:        2,
		ParallelSyncs: 1,
		DownAfter:     time.Second,
	}, map[int]*net.TCPAddr{
		1: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 6379},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do("SENTINEL", "flushconfig"); err != nil {
		t.Fatal(err)
	}

	commands := redisUserCommands(server.Commands())
	for _, want := range [][]string{
		{"SENTINEL", "get-master-addr-by-name", "codis-1"},
		{"SENTINEL", "remove", "codis-1"},
		{"SENTINEL", "monitor", "codis-1", "127.0.0.1", "6379", "2"},
		{"SENTINEL", "set", "codis-1", "parallel-syncs", "1", "down-after-milliseconds", "1000", "auth-pass", "secret"},
		{"SENTINEL", "flushconfig"},
	} {
		if !redisCommandExistsFold(commands, want) {
			t.Fatalf("command %v not observed: %v", want, commands)
		}
	}
}

func sentinelStringMap(values ...string) *redistest.Resp {
	items := make([]*redistest.Resp, len(values))
	for i := range values {
		items[i] = redistest.Bulk(values[i])
	}
	return redistest.Array(items...)
}

func redisCommandExistsFold(commands [][]string, want []string) bool {
	for _, cmd := range commands {
		if len(cmd) != len(want) {
			continue
		}
		match := true
		for i := range cmd {
			if !strings.EqualFold(cmd[i], want[i]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

type sentinelPubSubServer struct {
	t  testing.TB
	ln net.Listener

	mu       sync.Mutex
	commands [][]string

	product string
}

func newSentinelPubSubServer(t testing.TB, product string) *sentinelPubSubServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &sentinelPubSubServer{t: t, ln: ln, product: product}
	go s.serve()
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func (s *sentinelPubSubServer) Addr() string {
	return s.ln.Addr().String()
}

func (s *sentinelPubSubServer) Close() error {
	return s.ln.Close()
}

func (s *sentinelPubSubServer) Commands() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	commands := make([][]string, len(s.commands))
	for i := range s.commands {
		commands[i] = append([]string(nil), s.commands[i]...)
	}
	return commands
}

func (s *sentinelPubSubServer) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serveConn(c)
	}
}

func (s *sentinelPubSubServer) serveConn(c net.Conn) {
	defer c.Close()
	dec := proxyredis.NewDecoder(c)
	enc := proxyredis.NewEncoder(c)
	for {
		r, err := dec.Decode()
		if err != nil {
			return
		}
		args := make([]string, len(r.Array))
		for i, a := range r.Array {
			args[i] = string(a.Value)
		}
		s.mu.Lock()
		s.commands = append(s.commands, append([]string(nil), args...))
		s.mu.Unlock()

		switch strings.ToUpper(args[0]) {
		case "HELLO":
			if err := enc.Encode(proxyredis.NewError([]byte("ERR unknown command 'HELLO'")), true); err != nil {
				return
			}
		case "PING":
			if err := enc.Encode(proxyredis.NewString([]byte("PONG")), true); err != nil {
				return
			}
		case "SUBSCRIBE":
			if len(args) != 2 || args[1] != "+switch-master" {
				s.t.Errorf("unexpected subscribe command: %v", args)
				return
			}
			if err := enc.Encode(proxyredis.NewArray([]*proxyredis.Resp{
				proxyredis.NewBulkBytes([]byte("subscribe")),
				proxyredis.NewBulkBytes([]byte("+switch-master")),
				proxyredis.NewInt([]byte("1")),
			}), true); err != nil {
				return
			}
			if err := enc.Encode(proxyredis.NewArray([]*proxyredis.Resp{
				proxyredis.NewBulkBytes([]byte("message")),
				proxyredis.NewBulkBytes([]byte("+switch-master")),
				proxyredis.NewBulkBytes([]byte(s.product + "-1 127.0.0.1 6379 127.0.0.1 6380")),
			}), true); err != nil {
				return
			}
			return
		default:
			s.t.Errorf("unexpected command: %v", args)
			return
		}
	}
}
