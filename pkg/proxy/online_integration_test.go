// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

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
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/etcd"
	"github.com/CodisLabs/codis/pkg/proxy/etcdonline"
)

// The A5 end-to-end test (contract §7) runs a real single-member etcd and the
// real registration implementation against started proxies. It is skipped
// unless CODIS_ETCD_SERVER_BINARY names the etcd binary, like the integration
// tests of pkg/models/etcd and pkg/proxy/etcdonline.
//
// The imports of pkg/proxy/etcdonline and pkg/models/etcd live in a _test.go
// file on purpose: pkg/proxy production code never links the coordinator
// (architecture rule D2).
const (
	a5Product = "it-online-a5"
	a5TTL     = 2 * time.Second
	a5Refresh = 200 * time.Millisecond
)

func a5EtcdBinary(t *testing.T) string {
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

func a5FreePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	if err := ln.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}

// a5LogBuffer collects the server output so a failed start can show why.
type a5LogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *a5LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *a5LogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// a5Server owns one single-member etcd process. The data directory and the
// ports stay the same across a kill/restart, so a client that survived the
// outage reconnects to the same member.
type a5Server struct {
	mu           sync.Mutex
	bin          string
	dataDir      string
	endpoint     string
	peerEndpoint string
	clusterToken string
	logs         *a5LogBuffer
	cmd          *exec.Cmd
}

func a5StartServer(t *testing.T) *a5Server {
	t.Helper()
	clientPort := a5FreePort(t)
	peerPort := a5FreePort(t)
	srv := &a5Server{
		bin:          a5EtcdBinary(t),
		dataDir:      t.TempDir(),
		endpoint:     "http://127.0.0.1:" + clientPort,
		peerEndpoint: "http://127.0.0.1:" + peerPort,
		clusterToken: "codis-a5-test-" + clientPort,
		logs:         &a5LogBuffer{},
	}
	srv.start(t)
	t.Cleanup(srv.stop)
	return srv
}

func (s *a5Server) start(t *testing.T) {
	t.Helper()
	if s.bin == "" {
		t.Fatal("no etcd binary")
	}
	cmd := exec.Command(s.bin,
		"--data-dir", filepath.Join(s.dataDir, "data"),
		"--listen-client-urls", s.endpoint,
		"--advertise-client-urls", s.endpoint,
		"--listen-peer-urls", s.peerEndpoint,
		"--initial-advertise-peer-urls", s.peerEndpoint,
		"--initial-cluster", "default="+s.peerEndpoint,
		"--initial-cluster-state", "new",
		"--initial-cluster-token", s.clusterToken,
		"--log-level", "error",
	)
	cmd.Stdout = s.logs
	cmd.Stderr = s.logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start etcd: %v", err)
	}
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	s.waitReady(t)
}

func (s *a5Server) stop() {
	s.mu.Lock()
	cmd := s.cmd
	s.cmd = nil
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func (s *a5Server) restart(t *testing.T) {
	t.Helper()
	s.stop()
	s.start(t)
}

func (s *a5Server) waitReady(t *testing.T) {
	t.Helper()
	u, err := url.Parse(s.endpoint)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", u.Host, time.Second); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("etcd at %s not ready within 15s; log:\n%s", s.endpoint, s.logs.String())
}

func a5Client(endpoint string) (*etcd.Client, error) {
	return etcd.New(&etcd.Config{
		Endpoints:      []string{endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})
}

func a5MustClient(t *testing.T, endpoint string) *etcd.Client {
	t.Helper()
	c, err := a5Client(endpoint)
	if err != nil {
		t.Fatalf("new etcd client: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// a5Factory mirrors the entrypoint wiring: the registration implementation is
// built lazily and connects through the coordinator connection selection.
func a5Factory(endpoint string) OnlineRegistryFactory {
	return func(product, token, addr string) OnlineRegistry {
		return etcdonline.New(etcdonline.Options{
			NewClient: func() (etcdonline.LeaseClient, error) {
				client, err := a5Client(endpoint)
				if err != nil {
					return nil, err
				}
				return client, nil
			},
			Product: product,
			Token:   token,
			Addr:    addr,
			TTL:     a5TTL,
		})
	}
}

type a5Proxy struct {
	s         *Proxy
	dialAddr  string
	modelAddr string // client-facing address, also the online record addr
	token     string
}

func a5ProxyConfig(t *testing.T, product, compat string, register bool, refresh time.Duration) *Config {
	t.Helper()
	port := a5FreePort(t)
	config := newProxyConfig()
	config.ProductName = product
	config.ProxyAddr = "127.0.0.1:" + port
	config.HostProxy = "127.0.0.1:" + port
	config.ClusterNodesCompat = compat
	config.EtcdRegisterOnline = register
	if refresh > 0 {
		config.ClusterNodesRefreshPeriod.Set(refresh)
	}
	return config
}

func a5StartProxy(t *testing.T, config *Config, endpoint string) *a5Proxy {
	t.Helper()
	var opts []Option
	if config.EtcdRegisterOnline {
		opts = append(opts, WithOnlineRegistryFactory(a5Factory(endpoint)))
	}
	s, err := New(config, opts...)
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// The entrypoint learns the model (and therefore the token) right after
	// construction and publishes the record only once it is online.
	s.SetModel(s.Model())
	if err := s.Start(); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	return &a5Proxy{
		s:         s,
		dialAddr:  s.lproxy.Addr().String(),
		modelAddr: s.Model().ProxyAddr,
		token:     s.Model().Token,
	}
}

// a5ClusterNodes dials the client port and asks for CLUSTER NODES.
func a5ClusterNodes(t *testing.T, p *a5Proxy) [][]string {
	t.Helper()
	c := dialProxy(t, p.dialAddr)
	defer c.Close()
	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() {
		t.Fatalf("CLUSTER NODES = %s %q", resp.Type, resp.Value)
	}
	return parseClusterNodes(resp.Value)
}

// a5Nodes returns the advertised addresses this proxy reports and the number
// of nodes flagged as itself.
func a5Nodes(t *testing.T, p *a5Proxy) (map[string]bool, int) {
	t.Helper()
	addrs := map[string]bool{}
	var self int
	for _, line := range a5ClusterNodes(t, p) {
		if len(line) < 3 {
			t.Fatalf("malformed CLUSTER NODES line %v", line)
		}
		addr := strings.SplitN(line[1], "@", 2)[0]
		if addrs[addr] {
			t.Fatalf("duplicate node addr %s", addr)
		}
		addrs[addr] = true
		if strings.Contains(line[2], "myself") {
			self++
		}
	}
	return addrs, self
}

func a5Sorted(addrs map[string]bool) []string {
	out := make([]string, 0, len(addrs))
	for addr := range addrs {
		out = append(out, addr)
	}
	slices.Sort(out)
	return out
}

// a5WaitNodes polls CLUSTER NODES until want is satisfied and returns how long
// that took.
func a5WaitNodes(t *testing.T, p *a5Proxy, timeout time.Duration, what string, want func(addrs map[string]bool, self int) bool) time.Duration {
	t.Helper()
	start := time.Now()
	deadline := start.Add(timeout)
	for {
		addrs, self := a5Nodes(t, p)
		if want(addrs, self) {
			return time.Since(start)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s: nodes %v (self %d)", timeout, what, a5Sorted(addrs), self)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func a5Wait(t *testing.T, timeout time.Duration, what string, cond func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	deadline := start.Add(timeout)
	for {
		if cond() {
			return time.Since(start)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// a5Record reads the online record of one token with the probe client.
func a5Record(t *testing.T, probe *etcd.Client, product, token string) (models.OnlineRecord, int64, bool) {
	t.Helper()
	key := models.OnlineProxyPath(product, token)
	raw, err := probe.Read(key, false)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	if raw == nil {
		return models.OnlineRecord{}, 0, false
	}
	var rec models.OnlineRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("record value %q is not a json OnlineRecord: %v", raw, err)
	}
	lease, exists, err := probe.LeaseOfKey(key)
	if err != nil {
		t.Fatalf("lease of %s: %v", key, err)
	}
	if !exists {
		return rec, 0, false
	}
	return rec, lease, true
}

// a5Standalone runs only the registration implementation, for the same-token
// restart and conflict scenarios that do not need a full proxy.
type a5Standalone struct {
	reg  *etcdonline.Registry
	addr string
}

func a5StartStandalone(t *testing.T, endpoint, product, token, addr string) *a5Standalone {
	t.Helper()
	client, err := a5Client(endpoint)
	if err != nil {
		t.Fatalf("new etcd client: %v", err)
	}
	reg := etcdonline.New(etcdonline.Options{Client: client, Product: product, Token: token, Addr: addr, TTL: a5TTL})
	go reg.Register(context.Background())
	t.Cleanup(func() { _ = reg.Close() })
	return &a5Standalone{reg: reg, addr: addr}
}

func TestIntegrationA5MixedOnlineProxies(t *testing.T) {
	server := a5StartServer(t)
	product := a5Product
	probe := a5MustClient(t, server.endpoint)

	// A: all + registration (discovers every online peer of the product).
	a := a5StartProxy(t, a5ProxyConfig(t, product, ClusterNodesCompatAll, true, a5Refresh), server.endpoint)
	// B: self + registration (registers, answers with itself only).
	b := a5StartProxy(t, a5ProxyConfig(t, product, ClusterNodesCompatSelf, true, 0), server.endpoint)
	// C: disabled + registration (registers, refuses CLUSTER).
	c := a5StartProxy(t, a5ProxyConfig(t, product, ClusterNodesCompatDisabled, true, 0), server.endpoint)
	// D: registration off + self (neither registers nor is discovered).
	d := a5StartProxy(t, a5ProxyConfig(t, product, ClusterNodesCompatSelf, false, 0), server.endpoint)

	// Each registering proxy published its own record.
	for _, p := range []*a5Proxy{a, b, c} {
		a5Wait(t, 15*time.Second, "record of "+p.token, func() bool {
			rec, lease, ok := a5Record(t, probe, product, p.token)
			return ok && lease != 0 && rec.Token == p.token && rec.Addr == p.modelAddr
		})
	}
	if rec, lease, ok := a5Record(t, probe, product, b.token); ok {
		ttl, err := probe.TimeToLive(lease)
		if err != nil {
			t.Fatalf("ttl of the online lease: %v", err)
		}
		t.Logf("online record of B bound to lease %d, server-granted ttl %ds (requested %s), addr %s",
			lease, ttl, a5TTL, rec.Addr)
	}

	discover := a5WaitNodes(t, a, 15*time.Second, "A discovers B and C and never D", func(addrs map[string]bool, self int) bool {
		return self == 1 && len(addrs) == 3 &&
			addrs[a.modelAddr] && addrs[b.modelAddr] && addrs[c.modelAddr] && !addrs[d.modelAddr]
	})
	t.Logf("A5 discovery: A(all, registered) listed itself+B+C and no D after %s: %v",
		discover.Round(time.Millisecond), a5Sorted(a5MustNodes(t, a)))

	for _, p := range []*a5Proxy{b, d} {
		addrs, self := a5Nodes(t, p)
		if len(addrs) != 1 || self != 1 || !addrs[p.modelAddr] {
			t.Fatalf("self proxy must answer with itself only: nodes %v (self %d)", a5Sorted(addrs), self)
		}
	}
	conn := dialProxy(t, c.dialAddr)
	resp := proxyCall(t, conn, "CLUSTER", "NODES")
	conn.Close()
	if !resp.IsError() || !strings.Contains(string(resp.Value), "command 'CLUSTER' is not allowed") {
		t.Fatalf("disabled proxy CLUSTER NODES = %s %q, want the disabled response", resp.Type, resp.Value)
	}

	// --- normal exit -------------------------------------------------------
	closedAt := time.Now()
	if err := b.s.Close(); err != nil {
		t.Fatalf("close B: %v", err)
	}
	removed := a5WaitNodes(t, a, time.Second, "A drops B after its normal exit", func(addrs map[string]bool, self int) bool {
		return self == 1 && len(addrs) == 2 && !addrs[b.modelAddr] && addrs[c.modelAddr] && addrs[a.modelAddr]
	})
	t.Logf("A5 normal exit: B closed; A removed the peer after %s with a %s refresh period (revoke at %s)",
		removed.Round(time.Millisecond), a5Refresh, closedAt.Format("15:04:05.000"))

	// --- refresh failure ---------------------------------------------------
	before, beforeSelf := a5Nodes(t, a)
	server.stop()
	time.Sleep(4 * a5Refresh)
	kept, keptSelf := a5Nodes(t, a)
	if keptSelf != beforeSelf || len(kept) != len(before) {
		t.Fatalf("refresh failure changed the snapshot: %v -> %v", a5Sorted(before), a5Sorted(kept))
	}
	for addr := range before {
		if !kept[addr] {
			t.Fatalf("refresh failure dropped peer %s: %v", addr, a5Sorted(kept))
		}
	}
	t.Logf("A5 refresh failure: etcd stopped; A kept the last successful snapshot %v (warn logged, self %d)",
		a5Sorted(kept), keptSelf)

	restartedAt := time.Now()
	server.restart(t)
	// A's snapshot survived the outage, so its presence proves nothing about
	// the refresh. A peer registered only after the restart does: it can only
	// appear through a successful refresh.
	recoveredPeerAddr := "127.0.0.1:" + a5FreePort(t)
	recoveredPeer := a5StartStandalone(t, server.endpoint, product, "recovered-token", recoveredPeerAddr)
	recover := a5WaitNodes(t, a, 15*time.Second, "A refreshes again after etcd restarted", func(addrs map[string]bool, self int) bool {
		return self == 1 && addrs[a.modelAddr] && addrs[c.modelAddr] && addrs[recoveredPeerAddr]
	})
	t.Logf("A5 refresh recovery: etcd restarted; A refreshed again and listed a peer registered after the restart %s later (%s after the restart)",
		recover.Round(time.Millisecond), time.Since(restartedAt).Round(time.Millisecond))
	_ = recoveredPeer.reg.Close()
	a5WaitNodes(t, a, 2*time.Second, "A drops the peer registered after the restart", func(addrs map[string]bool, self int) bool {
		return self == 1 && !addrs[recoveredPeerAddr] && addrs[c.modelAddr] && addrs[a.modelAddr]
	})

	// --- same token restart and conflict -----------------------------------
	b2 := a5StartStandalone(t, server.endpoint, product, b.token, "127.0.0.1:"+a5FreePort(t))
	a5WaitNodes(t, a, 15*time.Second, "A sees the restarted B token at its new address", func(addrs map[string]bool, self int) bool {
		return self == 1 && addrs[a.modelAddr] && addrs[c.modelAddr] && addrs[b2.addr] && !addrs[b.modelAddr]
	})
	t.Logf("A5 same token restart: B's token is published at %s again", b2.addr)

	b3 := a5StartStandalone(t, server.endpoint, product, b.token, "127.0.0.1:"+a5FreePort(t))
	time.Sleep(6 * a5Refresh)
	addrs, self := a5Nodes(t, a)
	if self != 1 || !addrs[b2.addr] || addrs[b3.addr] {
		t.Fatalf("a conflicting instance overwrote the record: nodes %v (self %d), want the record of %s", a5Sorted(addrs), self, b2.addr)
	}
	if _, _, ok := a5Record(t, probe, product, b.token); !ok {
		t.Fatalf("the record of %s disappeared", b.token)
	}
	t.Logf("A5 same token conflict: %s kept the record while %s retried in the background", b2.addr, b3.addr)

	_ = b3.reg.Close()
	_ = b2.reg.Close()
	a5WaitNodes(t, a, 2*time.Second, "A drops the restarted B token", func(addrs map[string]bool, self int) bool {
		return self == 1 && !addrs[b2.addr] && addrs[c.modelAddr] && addrs[a.modelAddr]
	})

	// --- crash -------------------------------------------------------------
	crashAddr := "127.0.0.1:" + a5FreePort(t)
	crashToken := "crash-token"
	child := a5StartCrashChild(t, server.endpoint, product, crashToken, crashAddr)
	a5WaitNodes(t, a, 15*time.Second, "A discovers the crashing peer", func(addrs map[string]bool, self int) bool {
		return self == 1 && addrs[crashAddr]
	})
	key := models.OnlineProxyPath(product, crashToken)
	crashLease, exists, err := probe.LeaseOfKey(key)
	if err != nil || !exists {
		t.Fatalf("lease of the crashing peer = %d, %v, %v", crashLease, exists, err)
	}
	ttl, err := probe.TimeToLive(crashLease)
	if err != nil {
		t.Fatalf("ttl of the crashing peer: %v", err)
	}
	t.Logf("A5 crash: the child owns lease %d, remaining ttl %ds right after the grant (requested %s)", crashLease, ttl, a5TTL)

	// Watch the renewals. The SDK renews about every granted ttl/3, so with a
	// 2s lease the reported remaining ttl usually stays at the same value from
	// one renewal to the next; an increase is a visible renewal whenever the
	// sampling catches one. The baseline read above is not a renewal.
	var (
		lastRenewalAt time.Time
		renewals      int
		prevTTL       = ttl
	)
	watchUntil := time.Now().Add(4 * time.Second)
	for time.Now().Before(watchUntil) {
		if ttl, err := probe.TimeToLive(crashLease); err == nil {
			if ttl > prevTTL {
				lastRenewalAt = time.Now()
				renewals++
			}
			prevTTL = ttl
		}
		time.Sleep(50 * time.Millisecond)
	}

	killedAt := time.Now()
	if err := child.Process.Kill(); err != nil {
		t.Fatalf("kill -9 the child: %v", err)
	}
	_ = child.Wait()

	var keyGoneAt, peerGoneAt time.Time
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && (keyGoneAt.IsZero() || peerGoneAt.IsZero()) {
		if keyGoneAt.IsZero() {
			if _, exists, err := probe.LeaseOfKey(key); err == nil && !exists {
				keyGoneAt = time.Now()
			}
		}
		if peerGoneAt.IsZero() {
			if nodes, _ := a5Nodes(t, a); !nodes[crashAddr] {
				peerGoneAt = time.Now()
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if keyGoneAt.IsZero() || peerGoneAt.IsZero() {
		t.Fatalf("after kill -9 the record survived: key gone %v, peer removed %v", !keyGoneAt.IsZero(), !peerGoneAt.IsZero())
	}
	afterKill := peerGoneAt.Sub(killedAt)
	renewalSlack := a5TTL / 3
	var renewalToRemoval string
	switch {
	case !lastRenewalAt.IsZero() && killedAt.Sub(lastRenewalAt) <= renewalSlack:
		d := peerGoneAt.Sub(lastRenewalAt)
		renewalToRemoval = d.Round(time.Millisecond).String()
	default:
		// No renewal was visible close to the kill, so the last one happened
		// at most one renewal interval before it: report the bound.
		renewalToRemoval = fmt.Sprintf("%s..%s (last renewal within %s before the kill)",
			afterKill.Round(time.Millisecond), (afterKill + renewalSlack).Round(time.Millisecond), renewalSlack)
	}
	t.Logf("A5 crash: %d visible renewals; the key disappeared %s after kill -9 and A removed the peer %s after it (%s after the last renewal, refresh %s)",
		renewals, keyGoneAt.Sub(killedAt).Round(time.Millisecond), afterKill.Round(time.Millisecond), renewalToRemoval, a5Refresh)

	if gone := keyGoneAt.Sub(killedAt); gone > a5TTL+2*time.Second {
		t.Fatalf("the key disappeared %s after kill -9, want within ttl+2s (%s)", gone, a5TTL+2*time.Second)
	}
	if gone := peerGoneAt.Sub(killedAt); gone > a5TTL+a5Refresh+2*time.Second {
		t.Fatalf("A removed the crashed peer %s after kill -9, want within ttl+refresh+2s (%s)",
			gone, a5TTL+a5Refresh+2*time.Second)
	}
}

func a5MustNodes(t *testing.T, p *a5Proxy) map[string]bool {
	t.Helper()
	addrs, _ := a5Nodes(t, p)
	return addrs
}

// a5StartProxyWithoutHostProxy starts a proxy shaped like the default
// proxy.toml: only proxy_addr is configured, because --host-proxy has no TOML
// field and the test does not pass the option either. The address published in
// the online record can therefore only come from the model the dashboard
// issues (contract §4). It returns the proxy and its client port.
func a5StartProxyWithoutHostProxy(t *testing.T, product, compat string, register bool, refresh time.Duration, endpoint string) (*a5Proxy, string) {
	t.Helper()
	port := a5FreePort(t)
	config := newProxyConfig()
	config.ProductName = product
	config.ProxyAddr = "0.0.0.0:" + port
	config.ClusterNodesCompat = compat
	config.EtcdRegisterOnline = register
	if refresh > 0 {
		config.ClusterNodesRefreshPeriod.Set(refresh)
	}
	var opts []Option
	if register {
		opts = append(opts, WithOnlineRegistryFactory(a5Factory(endpoint)))
	}
	s, err := New(config, opts...)
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// The entrypoint learns the model (and therefore the token) right after
	// construction and publishes the record only once it is online.
	s.SetModel(s.Model())
	if err := s.Start(); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	return &a5Proxy{
		s:         s,
		dialAddr:  "127.0.0.1:" + port,
		modelAddr: s.Model().ProxyAddr,
		token:     s.Model().Token,
	}, port
}

// TestIntegrationA5RegistrationAddrWithoutHostProxy pins F1: with the default
// entrypoint configuration the online record carries the model address, and
// another proxy in all mode discovers it through CLUSTER NODES. Before the fix
// the record addr was the empty --host-proxy and discovery skipped it.
func TestIntegrationA5RegistrationAddrWithoutHostProxy(t *testing.T) {
	server := a5StartServer(t)
	product := a5Product
	probe := a5MustClient(t, server.endpoint)

	// B registers (self mode); A discovers every online record (all mode).
	// Neither proxy gets --host-proxy.
	b, port := a5StartProxyWithoutHostProxy(t, product, ClusterNodesCompatSelf, true, 0, server.endpoint)
	a, _ := a5StartProxyWithoutHostProxy(t, product, ClusterNodesCompatAll, true, a5Refresh, server.endpoint)

	// The model address is the bound proxy_addr with the wildcard resolved, so
	// it must be a real client-facing address and never the unspecified one.
	if b.modelAddr == "" || strings.HasPrefix(b.modelAddr, "0.0.0.0") || strings.HasPrefix(b.modelAddr, "[::]") {
		t.Fatalf("model address of the proxy listening on 0.0.0.0:%s = %q", port, b.modelAddr)
	}

	recorded := a5Wait(t, 15*time.Second, "online record of "+b.token, func() bool {
		rec, lease, ok := a5Record(t, probe, product, b.token)
		return ok && lease != 0 && rec.Token == b.token && rec.Addr == b.modelAddr
	})
	rec, _, ok := a5Record(t, probe, product, b.token)
	if !ok || rec.Addr == "" {
		t.Fatalf("online record of %s = %+v (exists %t), want a non-empty addr", b.token, rec, ok)
	}
	t.Logf("F1 recheck: record of %s published addr %q %s after going online (model addr %q)",
		b.token, rec.Addr, recorded.Round(time.Millisecond), b.modelAddr)

	discover := a5WaitNodes(t, a, 15*time.Second, "A discovers B at its model address", func(addrs map[string]bool, self int) bool {
		return self == 1 && len(addrs) == 2 && addrs[a.modelAddr] && addrs[b.modelAddr]
	})
	t.Logf("F1 recheck: A(all) listed itself and B(%q) after %s: %v",
		b.modelAddr, discover.Round(time.Millisecond), a5Sorted(a5MustNodes(t, a)))
}

// a5StartCrashChild starts a subprocess that runs only the registration
// implementation and blocks until the parent kills it. Reusing the helper of
// pkg/proxy/etcdonline is impossible across packages, so this is a slim copy.
func a5StartCrashChild(t *testing.T, endpoint, product, token, addr string) *exec.Cmd {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}
	cmd := exec.Command(bin, "-test.run=^TestOnlineRegistrationCrashHelper$", "--",
		endpoint, product, token, addr, strconv.Itoa(int(a5TTL/time.Second)))
	var stderr a5LogBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start crash helper: %v", err)
	}
	started := true
	t.Cleanup(func() {
		if started {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

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
	timeout := time.After(30 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("crash helper exited before registering; stderr:\n%s", stderr.String())
			}
			if strings.TrimSpace(line) == "registered" {
				return cmd
			}
		case <-timeout:
			t.Fatalf("crash helper did not register within 30s; stderr:\n%s", stderr.String())
		}
	}
}

// TestOnlineRegistrationCrashHelper is the child process of the crash
// scenario: it registers, reports that the record exists and then blocks until
// the parent kills it. It is a no-op in a normal test run.
func TestOnlineRegistrationCrashHelper(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "-test.run=^TestOnlineRegistrationCrashHelper$") {
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

	c, err := a5Client(endpoint)
	if err != nil {
		fmt.Fprintln(os.Stderr, "crash helper: new client:", err)
		os.Exit(2)
	}
	reg := etcdonline.New(etcdonline.Options{
		Client:  c,
		Product: product,
		Token:   token,
		Addr:    addr,
		TTL:     time.Duration(ttlSeconds) * time.Second,
	})
	go reg.Register(context.Background())

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
