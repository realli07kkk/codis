// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package migration

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/shake/client"
	"github.com/CodisLabs/codis/pkg/shake/client/proto"
)

// fakeRedis is a minimal RESP server used to drive the real RedisShake scan
// reader and standalone writer through Run without a Redis binary.
type fakeRedis struct {
	addr    string
	ln      net.Listener
	mu      sync.Mutex
	cmds    [][]string
	conns   map[net.Conn]struct{}
	handler func(cmd []string) []byte
	wg      sync.WaitGroup
}

func startFakeRedis(t *testing.T, handler func(cmd []string) []byte) *fakeRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRedis{addr: ln.Addr().String(), ln: ln, handler: handler, conns: map[net.Conn]struct{}{}}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.conns[conn] = struct{}{}
			f.mu.Unlock()
			f.wg.Add(1)
			go f.serve(conn)
		}
	}()
	// RedisShake keeps its client connections open after a one-shot task (the
	// process normally exits), so cleanup closes them before waiting.
	t.Cleanup(func() {
		ln.Close()
		f.mu.Lock()
		for conn := range f.conns {
			conn.Close()
		}
		f.mu.Unlock()
		f.wg.Wait()
	})
	return f
}

func (f *fakeRedis) serve(conn net.Conn) {
	defer f.wg.Done()
	defer conn.Close()
	pr := proto.NewReader(bufio.NewReader(conn))
	for {
		reply, err := pr.ReadReply()
		if err != nil {
			return
		}
		cmd := client.ArrayString(reply, nil)
		f.mu.Lock()
		f.cmds = append(f.cmds, cmd)
		handler := f.handler
		f.mu.Unlock()
		out := []byte("+OK\r\n")
		if handler != nil {
			if res := handler(cmd); res != nil {
				out = res
			}
		}
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

func (f *fakeRedis) commands() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.cmds...)
}

func (f *fakeRedis) hasCommand(prefix ...string) bool {
	for _, cmd := range f.commands() {
		if len(cmd) >= len(prefix) {
			match := true
			for i, p := range prefix {
				if !strings.EqualFold(cmd[i], p) {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

func respSimple(s string) []byte { return []byte("+" + s + "\r\n") }
func respInt(n int64) []byte     { return []byte(fmt.Sprintf(":%d\r\n", n)) }
func respBulk(s string) []byte   { return []byte(fmt.Sprintf("$%d\r\n%s\r\n", len(s), s)) }
func respArray(items ...string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(items))
	for _, item := range items {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(item), item)
	}
	return []byte(b.String())
}
func respScan(cursor string, keys ...string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "*2\r\n$%d\r\n%s\r\n*%d\r\n", len(cursor), cursor, len(keys))
	for _, key := range keys {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(key), key)
	}
	return []byte(b.String())
}

// TestRunScanPipelineEndToEnd drives the real scan reader -> filter -> writer
// pipeline against fake RESP servers and checks that every scanned key reaches
// the target through RESTORE.
func TestRunScanPipelineEndToEnd(t *testing.T) {
	source := startFakeRedis(t, func(cmd []string) []byte {
		switch strings.ToUpper(cmd[0]) {
		case "PING":
			return respSimple("PONG")
		case "INFO":
			if len(cmd) > 1 && strings.EqualFold(cmd[1], "keyspace") {
				return respBulk("# Keyspace\ndb0:keys=2,expires=0,avg_ttl=0\n")
			}
			return respBulk("# Server\nredis_version:7.2.4\n")
		case "SCAN":
			if cmd[1] == "0" {
				return respScan("1", "k1", "k2")
			}
			return respScan("0")
		case "DUMP":
			return respBulk("payload:" + cmd[1])
		case "PTTL":
			return respInt(-1)
		}
		return respSimple("OK")
	})
	target := startFakeRedis(t, func(cmd []string) []byte {
		switch strings.ToUpper(cmd[0]) {
		case "PING":
			return respSimple("PONG")
		}
		return respSimple("OK")
	})

	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	spec := &Spec{
		ID:       "job-scan",
		Name:     "scan pipeline",
		Mode:     ModeScan,
		Source:   Endpoint{Kind: EndpointStandalone, Address: source.addr},
		Target:   Endpoint{Kind: EndpointStandalone, Address: target.addr},
		Advanced: Advanced{Dir: dir, LogLevel: "warn"},
	}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), spec) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not finish")
	}

	for _, key := range []string{"k1", "k2"} {
		if !target.hasCommand("RESTORE", key, "0", "payload:"+key) {
			t.Fatalf("target did not receive RESTORE for %s, commands = %v", key, target.commands())
		}
	}
	if !source.hasCommand("SCAN", "0") {
		t.Fatalf("source was not scanned: %v", source.commands())
	}
	if _, err := os.Stat(dir + "/" + LogFileName); err != nil {
		t.Fatalf("shake log not written in job dir: %v", err)
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(0) || !processAlive(-1) {
		t.Fatal("non-positive pid must be treated as alive")
	}
	if !processAlive(os.Getpid()) {
		t.Fatal("own pid must be alive")
	}
}
