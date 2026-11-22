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

func openStartedProxy(t *testing.T, config *Config) (*Proxy, string) {
	s, err := New(config)
	assert.MustNoError(t, err)
	assert.MustNoError(t, s.Start())
	return s, s.lproxy.Addr().String()
}

func dialProxy(t *testing.T, addr string) *redis.Conn {
	c, err := net.Dial("tcp", addr)
	assert.MustNoError(t, err)
	return redis.NewConn(c, 4096, 4096)
}

func proxyCall(t *testing.T, c *redis.Conn, args ...string) *redis.Resp {
	multi := make([]*redis.Resp, len(args))
	for i, arg := range args {
		multi[i] = redis.NewBulkBytes([]byte(arg))
	}
	assert.MustNoError(t, c.EncodeMultiBulk(multi, true))
	resp, err := c.Decode()
	assert.MustNoError(t, err)
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
	assert.MustNoError(t, err)
	assert.Must(t, opstr == "CLIENT")
	assert.Must(t, !flag.IsNotAllowed())
}

func TestClientListCommand(t *testing.T) {
	s, addr := openStartedProxy(t, newProxyConfig())
	defer s.Close()

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLIENT", "LIST")
	assert.Must(t, resp.IsBulkBytes())

	entries := parseClientList(resp.Value)
	assert.Must(t, len(entries) >= 1)

	current := entries[0]
	assert.Must(t, current["id"] != "")
	assert.Must(t, current["addr"] != "")
	assert.Must(t, current["laddr"] != "")
	assert.Must(t, current["age"] != "")
	assert.Must(t, current["idle"] != "")
	assert.Must(t, current["flags"] == "N")
	assert.Must(t, current["db"] == "0")
	assert.Must(t, current["cmd"] == "CLIENT")
	assert.Must(t, current["user"] == "default")
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
	assert.Must(t, string(formatClientList(entries)) == expect)
}

func TestClientListRequiresAuth(t *testing.T) {
	config := newProxyConfig()
	config.SessionAuth = "secret"

	s, addr := openStartedProxy(t, config)
	defer s.Close()

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLIENT", "LIST")
	assert.Must(t, resp.IsError())
	assert.Must(t, string(resp.Value) == "NOAUTH Authentication required")

	resp = proxyCall(t, c, "AUTH", "secret")
	assert.Must(t, resp.IsString())

	resp = proxyCall(t, c, "CLIENT", "LIST")
	assert.Must(t, resp.IsBulkBytes())
	assert.Must(t, len(parseClientList(resp.Value)) >= 1)
}

func TestClientListFilters(t *testing.T) {
	s, addr := openStartedProxy(t, newProxyConfig())
	defer s.Close()

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLIENT", "LIST")
	assert.Must(t, resp.IsBulkBytes())
	entries := parseClientList(resp.Value)
	assert.Must(t, len(entries) >= 1)
	id := entries[0]["id"]

	resp = proxyCall(t, c, "CLIENT", "LIST", "TYPE", "normal")
	assert.Must(t, resp.IsBulkBytes())
	assert.Must(t, len(parseClientList(resp.Value)) >= 1)

	resp = proxyCall(t, c, "CLIENT", "LIST", "TYPE", "replica")
	assert.Must(t, resp.IsBulkBytes())
	assert.Must(t, string(resp.Value) == "")

	resp = proxyCall(t, c, "CLIENT", "LIST", "TYPE", "master")
	assert.Must(t, resp.IsBulkBytes())
	assert.Must(t, string(resp.Value) == "")

	resp = proxyCall(t, c, "CLIENT", "LIST", "TYPE", "pubsub")
	assert.Must(t, resp.IsBulkBytes())
	assert.Must(t, string(resp.Value) == "")

	resp = proxyCall(t, c, "CLIENT", "LIST", "TYPE", "unknown")
	assert.Must(t, resp.IsError())
	assert.Must(t, strings.Contains(string(resp.Value), "Unknown client type"))

	resp = proxyCall(t, c, "CLIENT", "LIST", "ID", id)
	assert.Must(t, resp.IsBulkBytes())
	entries = parseClientList(resp.Value)
	assert.Must(t, len(entries) == 1)
	assert.Must(t, entries[0]["id"] == id)

	resp = proxyCall(t, c, "CLIENT", "LIST", "ID", "not-number")
	assert.Must(t, resp.IsError())
	assert.Must(t, strings.Contains(string(resp.Value), "Invalid client ID"))
}

func TestClientUnsupportedSubcommand(t *testing.T) {
	s, addr := openStartedProxy(t, newProxyConfig())
	defer s.Close()

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLIENT", "KILL", "127.0.0.1:1")
	assert.Must(t, resp.IsError())
	assert.Must(t, strings.Contains(string(resp.Value), "unsupported CLIENT subcommand"))
}

func TestSessionRegistry(t *testing.T) {
	s, addr := openStartedProxy(t, newProxyConfig())
	defer s.Close()

	baseAlive := SessionsAlive()
	baseRegistry := clientSessions.count()

	c1 := dialProxy(t, addr)
	c2 := dialProxy(t, addr)

	assertEventually(t, func() bool {
		return SessionsAlive() == baseAlive+2 && clientSessions.count() == baseRegistry+2
	})

	assert.MustNoError(t, c1.Close())
	assert.MustNoError(t, c2.Close())

	assertEventually(t, func() bool {
		return SessionsAlive() == baseAlive && clientSessions.count() == baseRegistry
	})
}

func assertEventually(t *testing.T, ok func() bool) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond * 10)
	}
	assert.Must(t, ok())
}
