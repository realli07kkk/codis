// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/assert"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// openStartedProxyWithOptions is openStartedProxy with constructor options.
func openStartedProxyWithOptions(t *testing.T, config *Config, opts ...Option) (*Proxy, string) {
	t.Helper()
	s, err := New(config, opts...)
	assert.MustNoError(t, err)
	assert.MustNoError(t, s.Start())
	return s, s.lproxy.Addr().String()
}

// fakeOnlineRegistry stands in for the injected etcdonline implementation: it
// serves the configured records and records how the proxy drives the contract
// (Register on start, Close on shutdown). pkg/proxy never imports the real one.
type fakeOnlineRegistry struct {
	mu        sync.Mutex
	records   []models.OnlineRecord
	listErr   error
	lists     int
	registers int
	closes    int
	probe     func() error
	probeErr  error

	registered chan struct{}
	once       sync.Once
}

func newFakeOnlineRegistry(records ...models.OnlineRecord) *fakeOnlineRegistry {
	return &fakeOnlineRegistry{
		records:    records,
		registered: make(chan struct{}),
	}
}

func (r *fakeOnlineRegistry) setRecords(records ...models.OnlineRecord) {
	r.mu.Lock()
	r.records = records
	r.mu.Unlock()
}

func (r *fakeOnlineRegistry) setListError(err error) {
	r.mu.Lock()
	r.listErr = err
	r.mu.Unlock()
}

func (r *fakeOnlineRegistry) List(ctx context.Context) ([]models.OnlineRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lists++
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]models.OnlineRecord(nil), r.records...), nil
}

func (r *fakeOnlineRegistry) Register(ctx context.Context) {
	r.mu.Lock()
	r.registers++
	r.mu.Unlock()
	r.once.Do(func() { close(r.registered) })
	<-ctx.Done()
}

func (r *fakeOnlineRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes++
	if r.probe != nil {
		if err := r.probe(); err != nil && r.probeErr == nil {
			r.probeErr = err
		}
	}
	return nil
}

func (r *fakeOnlineRegistry) counts() (lists, registers, closes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lists, r.registers, r.closes
}

func (r *fakeOnlineRegistry) listCalls() int {
	lists, _, _ := r.counts()
	return lists
}

func (r *fakeOnlineRegistry) closeCalls() int {
	_, _, closes := r.counts()
	return closes
}

func (r *fakeOnlineRegistry) closeProbeError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.probeErr
}

func (r *fakeOnlineRegistry) waitRegistered(t *testing.T) {
	t.Helper()
	select {
	case <-r.registered:
	case <-time.After(5 * time.Second):
		t.Fatal("Register was not called")
	}
}

func TestClusterNodesAllDiscoversOnlinePeers(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatAll
	config.EtcdRegisterOnline = true
	config.ClusterNodesRefreshPeriod.Set(10 * time.Millisecond)

	registry := newFakeOnlineRegistry(
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.2:19000"},
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.9:19000"},
		models.OnlineRecord{Token: "", Addr: "127.0.0.6:19000"},
	)

	s, addr := openStartedProxyWithOptions(t, config, WithOnlineRegistry(registry))
	defer s.Close()
	registry.waitRegistered(t)
	waitUntil(t, func() bool {
		return registry.listCalls() >= 1
	})

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() {
		t.Fatalf("CLUSTER NODES resp = %s %q", resp.Type, resp.Value)
	}
	lines := parseClusterNodes(resp.Value)
	if len(lines) != 2 {
		t.Fatalf("nodes len = %d, want 2: %q", len(lines), resp.Value)
	}
	var selfCount int
	var hasPeer bool
	for _, line := range lines {
		assertClusterNodeID(t, line[0])
		if line[2] == "myself,master" {
			selfCount++
		}
		if strings.Contains(line[1], "127.0.0.2") {
			hasPeer = true
		}
	}
	if selfCount != 1 || !hasPeer {
		t.Fatalf("nodes = %q", resp.Value)
	}
	// The duplicate token keeps the first record, so its stale address is gone.
	if strings.Contains(string(resp.Value), "127.0.0.9") {
		t.Fatalf("duplicate token leaked: %q", resp.Value)
	}
	if !strings.Contains(string(resp.Value), "0-") {
		t.Fatalf("missing slot ranges: %q", resp.Value)
	}
}

func TestClusterNodesAllKeepsSelfUntilFirstSuccessfulRead(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatAll
	config.EtcdRegisterOnline = true
	config.ClusterNodesRefreshPeriod.Set(10 * time.Millisecond)

	registry := newFakeOnlineRegistry()
	registry.setListError(fmt.Errorf("etcd unavailable"))

	s, addr := openStartedProxyWithOptions(t, config, WithOnlineRegistry(registry))
	defer s.Close()
	registry.waitRegistered(t)

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() || len(parseClusterNodes(resp.Value)) != 1 {
		t.Fatalf("first read failed, want self only: %s %q", resp.Type, resp.Value)
	}

	registry.setListError(nil)
	registry.setRecords(models.OnlineRecord{Token: "token-b", Addr: "127.0.0.3:19000"})
	waitUntil(t, func() bool {
		nodes := s.router.clusterNodes.snapshot()
		return len(nodes) == 2 && countSelfNodes(nodes) == 1
	})

	// A later failure keeps the last successful snapshot instead of dropping
	// the peer back to self only.
	registry.setListError(fmt.Errorf("etcd unavailable"))
	time.Sleep(50 * time.Millisecond)
	resp = proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() || len(parseClusterNodes(resp.Value)) != 2 {
		t.Fatalf("later failure should keep the snapshot: %s %q", resp.Type, resp.Value)
	}
}

func TestClusterNodesSelfWithRegistrationReturnsSelfOnly(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.EtcdRegisterOnline = true

	registry := newFakeOnlineRegistry(
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.2:19000"},
	)

	s, addr := openStartedProxyWithOptions(t, config, WithOnlineRegistry(registry))
	defer s.Close()
	registry.waitRegistered(t)

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() {
		t.Fatalf("CLUSTER NODES resp = %s %q", resp.Type, resp.Value)
	}
	lines := parseClusterNodes(resp.Value)
	if len(lines) != 1 || lines[0][2] != "myself,master" || lines[0][8] != "0-16383" {
		t.Fatalf("self mode must return only itself: %q", resp.Value)
	}
	if got := registry.listCalls(); got != 0 {
		t.Fatalf("self mode must not read the online list, got %d reads", got)
	}
}

func TestClusterNodesDisabledWithRegistrationKeepsDisabledResponse(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatDisabled
	config.EtcdRegisterOnline = true

	registry := newFakeOnlineRegistry(
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.2:19000"},
	)

	s, addr := openStartedProxyWithOptions(t, config, WithOnlineRegistry(registry))
	defer s.Close()
	registry.waitRegistered(t)

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsError() || !strings.Contains(string(resp.Value), "command 'CLUSTER' is not allowed") {
		t.Fatalf("CLUSTER NODES disabled resp = %s %q", resp.Type, resp.Value)
	}
	if got := registry.listCalls(); got != 0 {
		t.Fatalf("disabled mode must not read the online list, got %d reads", got)
	}
}

func TestProxyAllWithoutInjectedRegistryReturnsError(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatAll
	config.EtcdRegisterOnline = true

	_, err := New(config)
	if err == nil || !strings.Contains(err.Error(), "cluster_nodes_compat=all requires etcd_register_online and an injected online registry") {
		t.Fatalf("New error = %v", err)
	}
}

func TestProxyEtcdRegisterOnlineWithoutInjectedRegistryReturnsError(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.EtcdRegisterOnline = true

	_, err := New(config)
	if err == nil || !strings.Contains(err.Error(), "etcd_register_online requires an injected online registry") {
		t.Fatalf("New error = %v", err)
	}
}

func TestProxyCloseClosesOnlineRegistryFirst(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.EtcdRegisterOnline = true

	registry := newFakeOnlineRegistry()
	var s *Proxy
	registry.probe = func() error {
		if s == nil {
			return nil
		}
		select {
		case <-s.Done():
			return fmt.Errorf("online registry closed after the proxy shutdown began")
		default:
			return nil
		}
	}

	s, _ = openStartedProxyWithOptions(t, config, WithOnlineRegistry(registry))
	registry.waitRegistered(t)

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := registry.closeCalls(); got != 1 {
		t.Fatalf("online registry Close called %d times, want 1", got)
	}
	if err := registry.closeProbeError(); err != nil {
		t.Fatal(err)
	}

	// Close is idempotent, so the registry must not be closed twice.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := registry.closeCalls(); got != 1 {
		t.Fatalf("online registry Close called %d times after a second proxy Close", got)
	}
}

func TestProxyWithoutOnlineRegistrationDoesNotWireRegistry(t *testing.T) {
	for _, compat := range []string{ClusterNodesCompatDisabled, ClusterNodesCompatSelf} {
		t.Run(compat, func(t *testing.T) {
			config := newProxyConfig()
			config.ClusterNodesCompat = compat

			s, addr := openStartedProxy(t, config)
			defer s.Close()

			if s.onlineRegistry != nil {
				t.Fatal("no registry should be wired when etcd_register_online is off")
			}
			if s.router.clusterNodes == nil || s.router.clusterNodes.lister != nil {
				t.Fatal("the discovery lister must stay nil without an injected registry")
			}

			c := dialProxy(t, addr)
			defer c.Close()

			resp := proxyCall(t, c, "CLUSTER", "NODES")
			switch compat {
			case ClusterNodesCompatDisabled:
				if !resp.IsError() || !strings.Contains(string(resp.Value), "command 'CLUSTER' is not allowed") {
					t.Fatalf("disabled resp = %s %q", resp.Type, resp.Value)
				}
			default:
				if !resp.IsBulkBytes() || len(parseClusterNodes(resp.Value)) != 1 {
					t.Fatalf("self resp = %s %q", resp.Type, resp.Value)
				}
			}
		})
	}
}

// factoryRecorder wraps a fake registry and records how often and with which
// identity the proxy asked for it. The proxy must not ask before the token is
// known, so an unused recorder proves the connection stays lazy.
type factoryRecorder struct {
	mu       sync.Mutex
	calls    int
	product  string
	token    string
	addr     string
	registry OnlineRegistry
}

func (f *factoryRecorder) factory(product, token, addr string) OnlineRegistry {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.product, f.token, f.addr = product, token, addr
	return f.registry
}

func (f *factoryRecorder) counts() (calls int, product, token, addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.product, f.token, f.addr
}

func TestProxyOnlineRegistrationWaitsForToken(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatAll
	config.EtcdRegisterOnline = true
	config.ClusterNodesRefreshPeriod.Set(10 * time.Millisecond)
	config.HostProxy = "127.0.0.1:19099"

	registry := newFakeOnlineRegistry(
		models.OnlineRecord{Token: "token-b", Addr: "127.0.0.3:19000"},
	)
	recorder := &factoryRecorder{registry: registry}

	s, addr := openStartedProxyWithOptions(t, config, WithOnlineRegistryFactory(recorder.factory))
	defer s.Close()

	// Going online must not need the token: no registry is built and no
	// registration or coordinator connection is attempted.
	if calls, _, _, _ := recorder.counts(); calls != 0 {
		t.Fatalf("registry factory called %d times before the token is known", calls)
	}
	if _, registers, _ := registry.counts(); registers != 0 {
		t.Fatal("registration started before the proxy token was known")
	}

	c := dialProxy(t, addr)
	defer c.Close()
	// The proxy serves normally and discovery reports self only.
	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() || len(parseClusterNodes(resp.Value)) != 1 {
		t.Fatalf("nodes before the token is known = %s %q, want self only", resp.Type, resp.Value)
	}

	s.SetModel(s.Model())
	// The record address is the model address. With --host-proxy set it is
	// that option verbatim: setup resolves the bound proxy_addr through
	// utils.ReplaceUnspecifiedIP, which returns the override when non-empty.
	if calls, product, token, gotAddr := recorder.counts(); calls != 1 || product != config.ProductName ||
		token != s.Model().Token || gotAddr != config.HostProxy {
		t.Fatalf("factory = %d calls product %q token %q addr %q, want one call with the proxy identity",
			calls, product, token, gotAddr)
	}
	registry.waitRegistered(t)
}

func TestProxyOnlineRegistrationUsesModelAddr(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.EtcdRegisterOnline = true

	registry := newFakeOnlineRegistry()
	recorder := &factoryRecorder{registry: registry}

	s, _ := openStartedProxyWithOptions(t, config, WithOnlineRegistryFactory(recorder.factory))
	defer s.Close()

	// No --host-proxy (it has no TOML field) and the default proxy_addr: the
	// record address must come from the model the dashboard issued.
	const modelAddr = "10.0.0.5:19000"
	selfAddr := s.Model().ProxyAddr
	m := *s.Model()
	m.ProxyAddr = modelAddr
	s.SetModel(&m)

	if got := s.Model().ProxyAddr; got != selfAddr {
		t.Fatalf("the proxy model was modified: proxy addr %q, want %q", got, selfAddr)
	}
	if calls, product, token, gotAddr := recorder.counts(); calls != 1 || product != config.ProductName ||
		token != m.Token || gotAddr != modelAddr {
		t.Fatalf("factory = %d calls product %q token %q addr %q, want addr %q from the model",
			calls, product, token, gotAddr, modelAddr)
	}
	registry.waitRegistered(t)
}

func TestProxyOnlineRegistrationSkipsEmptyAddr(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.EtcdRegisterOnline = true

	registry := newFakeOnlineRegistry()
	recorder := &factoryRecorder{registry: registry}

	logs := &lockedLogBuffer{}
	logger := log.New(logs, "")
	logger.SetLevel(log.LevelWarn)
	std := log.SwapStdLog(logger)
	t.Cleanup(func() {
		log.SetStdLog(std)
	})

	s, err := New(config, WithOnlineRegistryFactory(recorder.factory))
	assert.MustNoError(t, err)
	defer s.Close()

	// The model carries no address, no --host-proxy was given and the
	// configured proxy_addr is cleared too (config is owned by this test and
	// proxy_addr is only read while New runs, so this cannot race): there is
	// nothing a client could reach, so no registry is built and no record is
	// ever published.
	start := logs.Len()
	s.config.ProxyAddr = ""
	s.SetModel(&models.Proxy{Token: s.Model().Token})

	if calls, _, _, _ := recorder.counts(); calls != 0 {
		t.Fatalf("registry factory called %d times without a client-facing addr", calls)
	}
	if _, registers, closes := registry.counts(); registers != 0 || closes != 0 {
		t.Fatalf("registry was used without an addr: registers %d closes %d", registers, closes)
	}
	if out := logs.Since(start); !strings.Contains(out, "no client-facing addr") {
		t.Fatalf("missing the warning about the skipped registration: %q", out)
	}
}

func TestProxyOnlineRegistrationSetModelBeforeStart(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.EtcdRegisterOnline = true

	registry := newFakeOnlineRegistry()
	recorder := &factoryRecorder{registry: registry}

	s, err := New(config, WithOnlineRegistryFactory(recorder.factory))
	assert.MustNoError(t, err)
	defer s.Close()

	if calls, _, _, _ := recorder.counts(); calls != 0 {
		t.Fatalf("registry factory called %d times before SetModel", calls)
	}
	s.SetModel(s.Model())
	if calls, _, _, _ := recorder.counts(); calls != 1 {
		t.Fatalf("registry factory called %d times by SetModel, want 1", calls)
	}
	// The token is known but the proxy is not online yet, so the record must
	// not be published.
	if _, registers, _ := registry.counts(); registers != 0 {
		t.Fatal("registration started before the proxy was online")
	}
	assert.MustNoError(t, s.Start())
	registry.waitRegistered(t)
}

func TestProxyCloseWithoutTokenNeverBuildsRegistry(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatDisabled
	config.EtcdRegisterOnline = true

	registry := newFakeOnlineRegistry()
	recorder := &factoryRecorder{registry: registry}

	s, _ := openStartedProxyWithOptions(t, config, WithOnlineRegistryFactory(recorder.factory))

	// The proxy shuts down before any model was issued: no registry is built
	// and closing it must neither panic nor wait for anything.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if calls, _, _, _ := recorder.counts(); calls != 0 {
		t.Fatalf("registry factory called %d times, want 0", calls)
	}
	if _, registers, closes := registry.counts(); registers != 0 || closes != 0 {
		t.Fatalf("registry was used without a token: registers %d closes %d", registers, closes)
	}
}

func TestProxyCloseClosesLazilyCreatedRegistry(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.EtcdRegisterOnline = true

	registry := newFakeOnlineRegistry()
	var s *Proxy
	registry.probe = func() error {
		if s == nil {
			return nil
		}
		select {
		case <-s.Done():
			return fmt.Errorf("online registry closed after the proxy shutdown began")
		default:
			return nil
		}
	}
	recorder := &factoryRecorder{registry: registry}

	s, _ = openStartedProxyWithOptions(t, config, WithOnlineRegistryFactory(recorder.factory))
	s.SetModel(s.Model())
	registry.waitRegistered(t)

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, registers, closes := registry.counts(); registers != 1 || closes != 1 {
		t.Fatalf("registry registers %d closes %d, want 1 and 1", registers, closes)
	}
	if err := registry.closeProbeError(); err != nil {
		t.Fatal(err)
	}
}
