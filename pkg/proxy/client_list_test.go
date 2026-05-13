// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func openStartedProxy(config *Config) (*Proxy, string) {
	s, err := New(config)
	assert.MustNoError(err)
	assert.MustNoError(s.Start())
	return s, s.lproxy.Addr().String()
}

func dialProxy(addr string) *redis.Conn {
	c, err := net.Dial("tcp", addr)
	assert.MustNoError(err)
	return redis.NewConn(c, 4096, 4096)
}

func proxyCall(c *redis.Conn, args ...string) *redis.Resp {
	multi := make([]*redis.Resp, len(args))
	for i, arg := range args {
		multi[i] = redis.NewBulkBytes([]byte(arg))
	}
	assert.MustNoError(c.EncodeMultiBulk(multi, true))
	resp, err := c.Decode()
	assert.MustNoError(err)
	return resp
}

func parseClientList(value []byte) []map[string]string {
	text := strings.TrimSuffix(string(value), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	entries := make([]map[string]string, len(lines))
	for i, line := range lines {
		entry := make(map[string]string)
		for _, field := range strings.Split(line, " ") {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) == 2 {
				entry[kv[0]] = kv[1]
			}
		}
		entries[i] = entry
	}
	return entries
}

func TestClientCommandIsAllowed(t *testing.T) {
	opstr, flag, err := getOpInfo([]*redis.Resp{redis.NewBulkBytes([]byte("client"))})
	assert.MustNoError(err)
	assert.Must(opstr == "CLIENT")
	assert.Must(!flag.IsNotAllowed())
}

func TestClientListCommand(t *testing.T) {
	s, addr := openStartedProxy(newProxyConfig())
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "CLIENT", "LIST")
	assert.Must(resp.IsBulkBytes())

	entries := parseClientList(resp.Value)
	assert.Must(len(entries) >= 1)

	current := entries[0]
	assert.Must(current["id"] != "")
	assert.Must(current["addr"] != "")
	assert.Must(current["laddr"] != "")
	assert.Must(current["age"] != "")
	assert.Must(current["idle"] != "")
	assert.Must(current["flags"] == "N")
	assert.Must(current["db"] == "0")
	assert.Must(current["cmd"] == "CLIENT")
	assert.Must(current["user"] == "default")
}

func TestFormatClientList(t *testing.T) {
	entries := []clientListEntry{{
		ID:     1,
		Addr:   "127.0.0.1:1000",
		LAddr:  "127.0.0.1:19000",
		Age:    2,
		Idle:   1,
		Flags:  "N",
		DB:     3,
		Events: "r",
		Cmd:    "CLIENT",
		User:   "default",
	}}
	expect := "id=1 addr=127.0.0.1:1000 laddr=127.0.0.1:19000 name= age=2 idle=1 flags=N db=3 sub=0 psub=0 ssub=0 multi=-1 qbuf=0 qbuf-free=0 obl=0 oll=0 omem=0 events=r cmd=CLIENT user=default redir=-1 resp=2\n"
	assert.Must(string(formatClientList(entries)) == expect)
}

func TestClientListRequiresAuth(t *testing.T) {
	config := newProxyConfig()
	config.SessionAuth = "secret"

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "CLIENT", "LIST")
	assert.Must(resp.IsError())
	assert.Must(string(resp.Value) == "NOAUTH Authentication required")

	resp = proxyCall(c, "AUTH", "secret")
	assert.Must(resp.IsString())

	resp = proxyCall(c, "CLIENT", "LIST")
	assert.Must(resp.IsBulkBytes())
	assert.Must(len(parseClientList(resp.Value)) >= 1)
}

func TestClientListFilters(t *testing.T) {
	s, addr := openStartedProxy(newProxyConfig())
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "CLIENT", "LIST")
	assert.Must(resp.IsBulkBytes())
	entries := parseClientList(resp.Value)
	assert.Must(len(entries) >= 1)
	id := entries[0]["id"]

	resp = proxyCall(c, "CLIENT", "LIST", "TYPE", "normal")
	assert.Must(resp.IsBulkBytes())
	assert.Must(len(parseClientList(resp.Value)) >= 1)

	resp = proxyCall(c, "CLIENT", "LIST", "TYPE", "replica")
	assert.Must(resp.IsBulkBytes())
	assert.Must(string(resp.Value) == "")

	resp = proxyCall(c, "CLIENT", "LIST", "TYPE", "master")
	assert.Must(resp.IsBulkBytes())
	assert.Must(string(resp.Value) == "")

	resp = proxyCall(c, "CLIENT", "LIST", "TYPE", "pubsub")
	assert.Must(resp.IsBulkBytes())
	assert.Must(string(resp.Value) == "")

	resp = proxyCall(c, "CLIENT", "LIST", "TYPE", "unknown")
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "Unknown client type"))

	resp = proxyCall(c, "CLIENT", "LIST", "ID", id)
	assert.Must(resp.IsBulkBytes())
	entries = parseClientList(resp.Value)
	assert.Must(len(entries) == 1)
	assert.Must(entries[0]["id"] == id)

	resp = proxyCall(c, "CLIENT", "LIST", "ID", "not-number")
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "Invalid client ID"))
}

func TestClientUnsupportedSubcommand(t *testing.T) {
	s, addr := openStartedProxy(newProxyConfig())
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "CLIENT", "KILL", "127.0.0.1:1")
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "unsupported CLIENT subcommand"))
}

func TestSessionRegistry(t *testing.T) {
	s, addr := openStartedProxy(newProxyConfig())
	defer s.Close()

	baseAlive := SessionsAlive()
	baseRegistry := clientSessions.count()

	c1 := dialProxy(addr)
	c2 := dialProxy(addr)

	assertEventually(func() bool {
		return SessionsAlive() == baseAlive+2 && clientSessions.count() == baseRegistry+2
	})

	assert.MustNoError(c1.Close())
	assert.MustNoError(c2.Close())

	assertEventually(func() bool {
		return SessionsAlive() == baseAlive && clientSessions.count() == baseRegistry
	})
}

func assertEventually(ok func() bool) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond * 10)
	}
	assert.Must(ok())
}
