// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcdonline

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/etcd"
)

// The integration tests run a real single-member etcd on a random port in a
// temporary data directory. They are skipped unless CODIS_ETCD_SERVER_BINARY
// names the etcd binary, exactly like pkg/models/etcd's integration tests.
const integrationTTL = 2 * time.Second

func etcdServerBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("CODIS_ETCD_SERVER_BINARY")
	if bin == "" {
		t.Skip("CODIS_ETCD_SERVER_BINARY is not set; skipping etcd integration test")
	}
	if info, err := os.Stat(bin); err != nil || info.IsDir() {
		t.Skipf("CODIS_ETCD_SERVER_BINARY = %q is not an executable file", bin)
	}
	return bin
}

func freeTCPPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)
	if err := ln.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type integrationServer struct {
	endpoint string
	cmd      *exec.Cmd
	logs     *syncBuffer
	once     sync.Once
}

func startIntegrationServer(t *testing.T) *integrationServer {
	t.Helper()
	bin := etcdServerBinary(t)
	clientPort := freeTCPPort(t)
	peerPort := freeTCPPort(t)
	dir := t.TempDir()
	endpoint := "http://127.0.0.1:" + clientPort
	peerEndpoint := "http://127.0.0.1:" + peerPort
	logs := &syncBuffer{}
	cmd := exec.Command(bin,
		"--data-dir", filepath.Join(dir, "data"),
		"--listen-client-urls", endpoint,
		"--advertise-client-urls", endpoint,
		"--listen-peer-urls", peerEndpoint,
		"--initial-advertise-peer-urls", peerEndpoint,
		"--initial-cluster", "default="+peerEndpoint,
		"--initial-cluster-state", "new",
		"--initial-cluster-token", "codis-etcdonline-test-"+clientPort,
		"--log-level", "error",
	)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start etcd: %v", err)
	}
	s := &integrationServer{endpoint: endpoint, cmd: cmd, logs: logs}
	t.Cleanup(s.stop)
	s.waitReady(t)
	return s
}

func (s *integrationServer) stop() {
	s.once.Do(func() {
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		_ = s.cmd.Wait()
	})
}

func (s *integrationServer) waitReady(t *testing.T) {
	t.Helper()
	u, err := url.Parse(s.endpoint)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", u.Host, time.Second); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("etcd at %s not ready within 10s; log:\n%s", s.endpoint, s.logs.String())
}

func integrationClient(t *testing.T, endpoint string) *etcd.Client {
	t.Helper()
	c, err := etcd.New(&etcd.Config{
		Endpoints:      []string{endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// integrationRegistry returns a registry wired to a real client with the short
// integration TTL and a fast retry cadence, so a takeover is observable in a
// couple of seconds.
func integrationRegistry(t *testing.T, endpoint, product, token, addr string) *Registry {
	t.Helper()
	r := New(Options{Client: integrationClient(t, endpoint), Product: product, Token: token, Addr: addr})
	r.ttl = integrationTTL
	r.stopWait = 5 * time.Second
	r.retry = backoff{minStep: 1, maxStep: 2, unit: 100 * time.Millisecond}
	return r
}

// publish starts the registration loop and waits until the record is visible
// to the probe client.
func publish(t *testing.T, r *Registry, probe *etcd.Client, product, token, addr string) context.CancelFunc {
	t.Helper()
	cancel := startRegister(t, r)
	waitOnlineRecord(t, probe, product, token, addr, 10*time.Second)
	return cancel
}

func waitOnlineRecord(t *testing.T, probe *etcd.Client, product, token, addr string, timeout time.Duration) {
	t.Helper()
	key := models.OnlineProxyPath(product, token)
	eventually(t, timeout, fmt.Sprintf("online record of %s addr %s", token, addr), func() bool {
		raw, err := probe.Read(key, false)
		if err != nil || raw == nil {
			return false
		}
		var rec models.OnlineRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return false
		}
		return rec.Token == token && rec.Addr == addr
	})
}

func keyStillExists(t *testing.T, probe *etcd.Client, product, token string) bool {
	t.Helper()
	raw, err := probe.Read(models.OnlineProxyPath(product, token), false)
	if err != nil {
		t.Fatalf("read online record: %v", err)
	}
	return raw != nil
}

func TestIntegrationRegisterListAndClose(t *testing.T) {
	s := startIntegrationServer(t)
	probe := integrationClient(t, s.endpoint)

	const (
		product = "it-online"
		token   = "token-a"
		addr    = "127.0.0.1:11080"
	)
	reg := integrationRegistry(t, s.endpoint, product, token, addr)
	publish(t, reg, probe, product, token, addr)

	raw, err := probe.Read(models.OnlineProxyPath(product, token), true)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var rec models.OnlineRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("record value %q is not a json OnlineRecord: %v", raw, err)
	}
	if rec.Token != token || rec.Addr != addr {
		t.Fatalf("record = %+v, want token %q addr %q", rec, token, addr)
	}

	leaseID, exists, err := probe.LeaseOfKey(models.OnlineProxyPath(product, token))
	if err != nil || !exists || leaseID == 0 {
		t.Fatalf("key lease = %d exists %v err %v, want a lease-bound record", leaseID, exists, err)
	}
	ttl, err := probe.TimeToLive(leaseID)
	if err != nil {
		t.Fatalf("ttl: %v", err)
	}
	if ttl < 1 || ttl > int64(integrationTTL/time.Second) {
		t.Fatalf("ttl of the online lease = %d, want about %d", ttl, int64(integrationTTL/time.Second))
	}
	t.Logf("online record bound to lease %d, remaining ttl %ds (requested %s)", leaseID, ttl, integrationTTL)

	lister := New(Options{Client: probe, Product: product, Token: token, Addr: addr})
	records, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []models.OnlineRecord{{Token: token, Addr: addr}}
	if len(records) != 1 || records[0] != want[0] {
		t.Fatalf("list = %+v, want %+v", records, want)
	}

	if err := reg.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The revoke deletes the record before the client is closed, so a normal
	// exit is visible immediately, not after the TTL.
	if keyStillExists(t, probe, product, token) {
		t.Fatal("online record survived a normal close")
	}
	if ttl, err := probe.TimeToLive(leaseID); err != nil || ttl != -1 {
		t.Fatalf("ttl after close = %d, %v; want -1, nil", ttl, err)
	}
	if records, err := lister.List(context.Background()); err != nil {
		t.Fatalf("list after close: %v", err)
	} else if len(records) != 0 {
		t.Fatalf("list after close = %+v, want empty", records)
	}
}

func TestIntegrationTwoTokensList(t *testing.T) {
	s := startIntegrationServer(t)
	probe := integrationClient(t, s.endpoint)

	const product = "it-online-two"
	ra := integrationRegistry(t, s.endpoint, product, "token-a", "127.0.0.1:11080")
	publish(t, ra, probe, product, "token-a", "127.0.0.1:11080")
	defer func() { _ = ra.Close() }()

	rb := integrationRegistry(t, s.endpoint, product, "token-b", "127.0.0.1:11081")
	publish(t, rb, probe, product, "token-b", "127.0.0.1:11081")
	defer func() { _ = rb.Close() }()

	lister := New(Options{Client: probe, Product: product, Token: "token-a", Addr: "127.0.0.1:11080"})
	records, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []models.OnlineRecord{
		{Token: "token-a", Addr: "127.0.0.1:11080"},
		{Token: "token-b", Addr: "127.0.0.1:11081"},
	}
	if len(records) != 2 || records[0] != want[0] || records[1] != want[1] {
		t.Fatalf("list = %+v, want %+v", records, want)
	}
}

func TestIntegrationSameTokenRestartTakeover(t *testing.T) {
	s := startIntegrationServer(t)
	probe := integrationClient(t, s.endpoint)

	const product = "it-online-dup"
	first := integrationRegistry(t, s.endpoint, product, "dup", "127.0.0.1:11080")
	publish(t, first, probe, product, "dup", "127.0.0.1:11080")
	firstLease, exists, err := probe.LeaseOfKey(models.OnlineProxyPath(product, "dup"))
	if err != nil || !exists {
		t.Fatalf("lease of the first record = %d, %v, %v", firstLease, exists, err)
	}

	second := integrationRegistry(t, s.endpoint, product, "dup", "127.0.0.1:11081")
	startRegister(t, second)

	// While the first instance owns the record, the second must not claim it.
	time.Sleep(500 * time.Millisecond)
	rec, lease, ok := readRecord(t, probe, product, "dup")
	if !ok || lease != firstLease || rec.Addr != "127.0.0.1:11080" {
		t.Fatalf("record with a conflicting instance = %+v lease %d exists %v, want the first instance's", rec, lease, ok)
	}

	// A normal exit of the first instance frees the record at once.
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	if keyStillExists(t, probe, product, "dup") {
		t.Fatal("record survived the close of its owner")
	}
	eventually(t, 10*time.Second, "takeover by the second instance", func() bool {
		rec, lease, ok := readRecord(t, probe, product, "dup")
		return ok && lease != firstLease && rec.Addr == "127.0.0.1:11081"
	})
	takeoverLease, _, _ := probe.LeaseOfKey(models.OnlineProxyPath(product, "dup"))
	t.Logf("takeover: the second instance now owns the record with lease %d (first lease %d)", takeoverLease, firstLease)

	// The renewed lease keeps the record alive well past the first TTL.
	time.Sleep(integrationTTL + 1500*time.Millisecond)
	rec, lease, ok = readRecord(t, probe, product, "dup")
	if !ok || lease != takeoverLease || rec.Addr != "127.0.0.1:11081" {
		t.Fatalf("record after a ttl of renewals = %+v lease %d exists %v, want the second instance's", rec, lease, ok)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second: %v", err)
	}
	if keyStillExists(t, probe, product, "dup") {
		t.Fatal("record survived the close of the second instance")
	}
}

func readRecord(t *testing.T, probe *etcd.Client, product, token string) (models.OnlineRecord, int64, bool) {
	t.Helper()
	key := models.OnlineProxyPath(product, token)
	raw, err := probe.Read(key, false)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if raw == nil {
		return models.OnlineRecord{}, 0, false
	}
	var rec models.OnlineRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("record value %q: %v", raw, err)
	}
	lease, _, err := probe.LeaseOfKey(key)
	if err != nil {
		t.Fatalf("lease of record: %v", err)
	}
	return rec, lease, true
}

// TestIntegrationCrashHelper is the child process of the crash test: it
// registers, reports that the record is visible and then blocks until the
// parent kills it. It is a no-op in a normal test run.
func TestIntegrationCrashHelper(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "-test.run=^TestIntegrationCrashHelper$") {
		t.Skip("online registration crash helper process")
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
		}
	}
	if len(args) != 5 {
		fmt.Fprintln(os.Stderr, "crash helper: want -- <endpoint> <product> <token> <addr> <ttl-seconds>")
		os.Exit(2)
	}
	endpoint, product, token, addr, ttlText := args[0], args[1], args[2], args[3], args[4]
	ttlSeconds, err := strconv.ParseInt(ttlText, 10, 64)
	if err != nil || ttlSeconds <= 0 {
		fmt.Fprintln(os.Stderr, "crash helper: bad ttl:", ttlText)
		os.Exit(2)
	}

	c, err := etcd.New(&etcd.Config{
		Endpoints:      []string{endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "crash helper: new client:", err)
		os.Exit(2)
	}
	reg := New(Options{Client: c, Product: product, Token: token, Addr: addr})
	reg.ttl = time.Duration(ttlSeconds) * time.Second
	rctx, rcancel := context.WithCancel(context.Background())
	defer rcancel()
	go reg.Register(rctx)

	key := models.OnlineProxyPath(product, token)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, exists, err := c.LeaseOfKey(key); err == nil && exists {
			fmt.Println("registered")
			// Sleep rather than block forever: the runtime deadlock detector
			// must never fire in the child.
			for {
				time.Sleep(time.Second)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "crash helper: record never became visible")
	os.Exit(3)
}

// TestIntegrationCrashExpiresKey kills a registering child process with
// SIGKILL and measures how long its record survives: the server must remove it
// about one TTL after the last renewal (contract §4).
func TestIntegrationCrashExpiresKey(t *testing.T) {
	s := startIntegrationServer(t)
	probe := integrationClient(t, s.endpoint)

	const (
		product = "it-online-crash"
		token   = "crash"
		addr    = "127.0.0.1:11097"
	)
	// A 5s TTL makes the renewals observable from the outside: the renewing
	// SDK keeps the remaining ttl in the upper two thirds of the granted value,
	// so each renewal raises the reported seconds by one. A 2s TTL reads the
	// same value from renewal to renewal and would hide the last renewal.
	const crashTTL = 5 * time.Second
	key := models.OnlineProxyPath(product, token)

	bin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}
	cmd := exec.Command(bin, "-test.run=^TestIntegrationCrashHelper$", "--", s.endpoint, product, token, addr, strconv.Itoa(int(crashTTL/time.Second)))
	var stderr syncBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	started := true
	defer func() {
		if started {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			default:
			}
		}
		close(lines)
	}()

	registered := false
	timeout := time.After(30 * time.Second)
	for !registered {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("helper exited before registering; stderr:\n%s", stderr.String())
			}
			if strings.TrimSpace(line) == "registered" {
				registered = true
			}
		case <-timeout:
			t.Fatalf("helper did not register within 30s; stderr:\n%s", stderr.String())
		}
	}

	// Watch the renewals: each one raises the remaining ttl, so an increase
	// marks a successful renewal. Once two are seen, wait one second before
	// the kill: a keepalive is sent about every TTL/3, so nothing is in flight
	// at the kill and "last renewal -> key gone" measures the server-side TTL
	// alone instead of a request the server applied after the kill.
	var (
		lastRenewal time.Time
		renewals    int
		prevTTL     int64 = -2
	)
	watchUntil := time.Now().Add(2*crashTTL + 2*time.Second)
	for time.Now().Before(watchUntil) && renewals < 2 {
		lease, exists, err := probe.LeaseOfKey(key)
		if err != nil {
			t.Fatalf("lease of record: %v", err)
		}
		if !exists {
			t.Fatalf("record disappeared before the kill")
		}
		ttl, err := probe.TimeToLive(lease)
		if err != nil {
			t.Fatalf("ttl of lease %d: %v", lease, err)
		}
		if ttl > prevTTL {
			lastRenewal = time.Now()
			renewals++
		}
		prevTTL = ttl
		time.Sleep(50 * time.Millisecond)
	}
	if renewals < 2 {
		t.Fatalf("observed %d renewals of the online lease, want at least 2 to measure the last one", renewals)
	}
	time.Sleep(time.Second)

	killedAt := time.Now()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_ = cmd.Wait()
	started = false

	var goneAt time.Time
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, exists, err := probe.LeaseOfKey(key); err != nil {
			t.Fatalf("lease of record: %v", err)
		} else if !exists {
			goneAt = time.Now()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if goneAt.IsZero() {
		t.Fatalf("record of a killed instance still present 15s after the kill (ttl %s)", crashTTL)
	}

	afterRenewal := goneAt.Sub(lastRenewal)
	afterKill := goneAt.Sub(killedAt)
	t.Logf("crash: %d renewals observed, the last one %s before kill -9; the key disappeared %s after that renewal (%s after the kill), granted ttl %s",
		renewals, killedAt.Sub(lastRenewal).Round(time.Millisecond), afterRenewal.Round(time.Millisecond), afterKill.Round(time.Millisecond), crashTTL)

	if afterKill > crashTTL+2*time.Second {
		t.Fatalf("key disappeared %s after kill -9, want within ttl+2s = %s", afterKill, crashTTL+2*time.Second)
	}
	if afterRenewal > crashTTL+2*time.Second {
		t.Fatalf("key disappeared %s after the last renewal, want about one ttl (%s)", afterRenewal, crashTTL)
	}
}
