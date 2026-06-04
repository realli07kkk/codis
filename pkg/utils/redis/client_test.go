// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import (
	"strings"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
)

const redis8ClientTestSlotID = 42

func TestClientRedis8InfoFullFields(t *testing.T) {
	const info = "# Replication\r\n" +
		"role:slave\r\n" +
		"master_host:127.0.0.1\r\n" +
		"master_port:6380\r\n" +
		"master_link_status:up\r\n" +
		"loading:0\r\n"
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "INFO":
			return redistest.Bulk(info)
		case "CONFIG":
			if len(args) == 3 && strings.ToUpper(args[1]) == "GET" && args[2] == "maxmemory" {
				return redistest.Array(redistest.Bulk("maxmemory"), redistest.Int("1048576"))
			}
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got, err := c.InfoFull()
	if err != nil {
		t.Fatal(err)
	}
	if got["master_addr"] != "127.0.0.1:6380" {
		t.Fatalf("master_addr = %q", got["master_addr"])
	}
	if got["master_link_status"] != "up" {
		t.Fatalf("master_link_status = %q", got["master_link_status"])
	}
	if got["maxmemory"] != "1048576" {
		t.Fatalf("maxmemory = %q", got["maxmemory"])
	}
}

func TestClientRedis8InfoFullStandaloneDoesNotInventMasterAddr(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "INFO":
			return redistest.Bulk("# Replication\r\nrole:master\r\nloading:0\r\n")
		case "CONFIG":
			return redistest.Array(redistest.Bulk("maxmemory"), redistest.Int("0"))
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got, err := c.InfoFull()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["master_addr"]; ok {
		t.Fatalf("master_addr should be absent for standalone info: %v", got)
	}
}

func TestClientRedis8SetMasterKeepsSlaveofAlias(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "MULTI":
			return redistest.OK()
		case "CONFIG", "SLAVEOF", "CLIENT":
			return redistest.String("QUEUED")
		case "EXEC":
			return redistest.Array(redistest.OK(), redistest.OK(), redistest.OK(), redistest.OK())
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.SetMaster("127.0.0.1:6380"); err != nil {
		t.Fatal(err)
	}
	var sawSlaveof, sawReplicaof, sawKillNormal bool
	for _, cmd := range s.Commands() {
		switch strings.ToUpper(cmd[0]) {
		case "SLAVEOF":
			sawSlaveof = len(cmd) == 3 && cmd[1] == "127.0.0.1" && cmd[2] == "6380"
		case "REPLICAOF":
			sawReplicaof = true
		case "CLIENT":
			sawKillNormal = len(cmd) == 4 && strings.ToUpper(cmd[1]) == "KILL" &&
				strings.ToUpper(cmd[2]) == "TYPE" && cmd[3] == "normal"
		}
	}
	if !sawSlaveof {
		t.Fatalf("SLAVEOF command not observed: %v", s.Commands())
	}
	if sawReplicaof {
		t.Fatalf("REPLICAOF should not replace SLAVEOF in Redis 3 compatible path: %v", s.Commands())
	}
	if !sawKillNormal {
		t.Fatalf("CLIENT KILL TYPE normal not observed: %v", s.Commands())
	}
	if !redisCommandExists(s.Commands(), []string{"CONFIG", "SET", "masteruser", ""}) {
		t.Fatalf("CONFIG SET masteruser clear not observed: %v", s.Commands())
	}
}

func TestClientRedis8SetMasterWritesMasterUserForNamedAuth(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH":
			if len(args) != 3 || args[1] != "svc" || args[2] != "secret" {
				t.Fatalf("unexpected auth command: %v", args)
			}
			return redistest.OK()
		case "MULTI":
			return redistest.OK()
		case "CONFIG", "SLAVEOF", "CLIENT":
			return redistest.String("QUEUED")
		case "EXEC":
			return redistest.Array(redistest.OK(), redistest.OK(), redistest.OK(), redistest.OK(), redistest.OK())
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientWithAuthIdentity(s.Addr(), RedisAuthIdentity{Username: "svc", Password: "secret"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.SetMaster("127.0.0.1:6380"); err != nil {
		t.Fatal(err)
	}
	if !redisCommandExists(s.Commands(), []string{"CONFIG", "SET", "masteruser", "svc"}) {
		t.Fatalf("CONFIG SET masteruser svc not observed: %v", s.Commands())
	}
	if !redisCommandExists(s.Commands(), []string{"CONFIG", "SET", "masterauth", "secret"}) {
		t.Fatalf("CONFIG SET masterauth secret not observed: %v", s.Commands())
	}
}

func TestClientSetMasterIgnoresUnsupportedMasterUserClear(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "CONFIG":
			if len(args) == 4 && strings.ToUpper(args[1]) == "SET" && args[2] == "masteruser" {
				return redistest.Error("ERR Unsupported CONFIG parameter: masteruser")
			}
			return redistest.String("QUEUED")
		case "MULTI":
			return redistest.OK()
		case "SLAVEOF", "CLIENT":
			return redistest.String("QUEUED")
		case "EXEC":
			return redistest.Array(redistest.OK(), redistest.OK(), redistest.OK(), redistest.OK())
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.SetMaster("127.0.0.1:6380"); err != nil {
		t.Fatal(err)
	}
}

func TestClientRedis8SelectTracksDatabase(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "SELECT" {
			t.Fatalf("unexpected command: %v", args)
		}
		return redistest.OK()
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Select(2); err != nil {
		t.Fatal(err)
	}
	if err := c.Select(2); err != nil {
		t.Fatal(err)
	}
	if c.Database != 2 {
		t.Fatalf("database = %d", c.Database)
	}
	commands := redisUserCommands(s.Commands())
	if n := len(commands); n != 1 {
		t.Fatalf("SELECT should be sent once, got %d commands: %v", n, commands)
	}
}

func TestClientGoRedisHandshakeUsesResp2AndDisablesIdentity(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var sawHello2 bool
	for _, cmd := range s.Commands() {
		if strings.ToUpper(cmd[0]) == "HELLO" && len(cmd) >= 2 && cmd[1] == "2" {
			sawHello2 = true
		}
		if len(cmd) >= 2 && strings.ToUpper(cmd[0]) == "CLIENT" && strings.ToUpper(cmd[1]) == "SETINFO" {
			t.Fatalf("CLIENT SETINFO should be disabled: %v", s.Commands())
		}
	}
	if !sawHello2 {
		t.Fatalf("HELLO 2 not observed: %v", s.Commands())
	}
}

func newRedisClientTestServer(t testing.TB, handler redistest.Handler) *redistest.Server {
	t.Helper()
	return redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "HELLO":
			return redistest.Error("ERR unknown command 'HELLO'")
		case "PING":
			return redistest.String("PONG")
		}
		return handler(args)
	})
}

func redisUserCommands(commands [][]string) [][]string {
	var filtered [][]string
	for _, cmd := range commands {
		switch strings.ToUpper(cmd[0]) {
		case "HELLO", "PING":
			continue
		}
		filtered = append(filtered, cmd)
	}
	return filtered
}

func redisCommandExists(commands [][]string, want []string) bool {
	for _, cmd := range commands {
		if len(cmd) != len(want) {
			continue
		}
		match := true
		for i := range cmd {
			if cmd[i] != want[i] {
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

func TestClientRedis8SlotsInfoStrictShape(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "SLOTSINFO" {
			t.Fatalf("unexpected command: %v", args)
		}
		return redistest.Array(
			redistest.Array(redistest.Int("3"), redistest.Int("11")),
			redistest.Array(redistest.Int("899"), redistest.Int("1")),
		)
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got, err := c.SlotsInfo()
	if err != nil {
		t.Fatal(err)
	}
	if got[3] != 11 || got[899] != 1 || len(got) != 2 {
		t.Fatalf("slots info = %v", got)
	}
}

func TestClientRedis8SlotsInfoRejectsMalformedShape(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		return redistest.Array(redistest.Array(redistest.Int("3")))
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.SlotsInfo(); err == nil {
		t.Fatal("expected malformed SLOTSINFO response to fail")
	}
}

func TestClientRedis8MigrationResponsesReturnRemainingCount(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "SLOTSMGRTTAGSLOT":
			return redistest.Array(redistest.Int("4"), redistest.Int("7"))
		case "SLOTSMGRTTAGSLOT-ASYNC":
			return redistest.Array(redistest.Int("9"), redistest.Int("0"))
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	remaining, err := c.MigrateSlot(redis8ClientTestSlotID, "127.0.0.1:6380")
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 7 {
		t.Fatalf("sync remaining = %d", remaining)
	}
	remaining, err = c.MigrateSlotAsync(redis8ClientTestSlotID, "127.0.0.1:6380", &MigrateSlotAsyncOption{
		MaxBulks: 200,
		MaxBytes: 1024,
		NumKeys:  100,
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("async remaining = %d", remaining)
	}
}

func TestClientRedis8RoleUppercasesArrayShape(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "ROLE" {
			t.Fatalf("unexpected command: %v", args)
		}
		return redistest.Array(redistest.Bulk("slave"))
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	role, err := c.Role()
	if err != nil {
		t.Fatal(err)
	}
	if role != "SLAVE" {
		t.Fatalf("role = %q", role)
	}
}

func TestClientPipelineCountsDriveRecycle(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "PING" {
			t.Fatalf("unexpected command: %v", args)
		}
		return redistest.String("PONG")
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Send("PING"); err != nil {
		t.Fatal(err)
	}
	if err := c.Send("PING"); err != nil {
		t.Fatal(err)
	}
	if c.isRecyclable() {
		t.Fatal("client with pending pipeline replies should not be recyclable")
	}
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		reply, err := c.Receive()
		if err != nil {
			t.Fatal(err)
		}
		if reply != "PONG" {
			t.Fatalf("reply[%d] = %v", i, reply)
		}
	}
	if c.Pipeline.Send != c.Pipeline.Recv {
		t.Fatalf("pipeline counts = %d/%d", c.Pipeline.Send, c.Pipeline.Recv)
	}
	if !c.isRecyclable() {
		t.Fatal("client should be recyclable after all pipeline replies are received")
	}
}

func TestClientRedis8AuthDefaultUserPath(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "AUTH" || len(args) != 2 || args[1] != "secret" {
			t.Fatalf("unexpected command: %v", args)
		}
		return redistest.OK()
	})

	c, err := NewClient(s.Addr(), "secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
}

func TestClientRedis8AuthNamedUserPath(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) != "AUTH" || len(args) != 3 || args[1] != "svc" || args[2] != "secret" {
			t.Fatalf("unexpected command: %v", args)
		}
		return redistest.OK()
	})

	c, err := NewClientWithAuthIdentity(s.Addr(), RedisAuthIdentity{
		Username: "svc",
		Password: "secret",
	}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
}

func TestClientRejectsAuthUsernameWithoutPassword(t *testing.T) {
	if _, err := NewClientWithAuthIdentity("127.0.0.1:0", RedisAuthIdentity{Username: "svc"}, time.Second); err == nil {
		t.Fatal("expected invalid redis auth identity")
	}
}
