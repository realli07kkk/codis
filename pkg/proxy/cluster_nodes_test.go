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
	"github.com/CodisLabs/codis/pkg/proxy/redis"
)

func parseClusterNodes(value []byte) [][]string {
	text := strings.TrimSuffix(string(value), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	nodes := make([][]string, len(lines))
	for i, line := range lines {
		nodes[i] = strings.Fields(line)
	}
	return nodes
}

func assertClusterNodeID(t *testing.T, id string) {
	t.Helper()
	if len(id) != 40 {
		t.Fatalf("node id length = %d, want 40: %q", len(id), id)
	}
	for _, ch := range id {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			t.Fatalf("node id contains non-hex char: %q", id)
		}
	}
}

func assertClusterSlotCoverage(t *testing.T, nodes []clusterNode) {
	t.Helper()
	if len(nodes) == 0 {
		t.Fatal("empty cluster nodes")
	}
	for i, node := range nodes {
		if node.SlotStart > node.SlotEnd {
			t.Fatalf("node %d invalid range %d-%d", i, node.SlotStart, node.SlotEnd)
		}
		if i == 0 {
			if node.SlotStart != 0 {
				t.Fatalf("first range starts at %d, want 0", node.SlotStart)
			}
		} else if node.SlotStart != nodes[i-1].SlotEnd+1 {
			t.Fatalf("range gap/overlap at %d: prev=%d current=%d", i, nodes[i-1].SlotEnd, node.SlotStart)
		}
	}
	if last := nodes[len(nodes)-1].SlotEnd; last != redisClusterSlotCount-1 {
		t.Fatalf("last range ends at %d, want %d", last, redisClusterSlotCount-1)
	}
}

func TestClusterCommandDefaultDisabled(t *testing.T) {
	opstr, flag, err := getOpInfo(redisMultiBulk("cluster"))
	if err != nil {
		t.Fatal(err)
	}
	if opstr != "CLUSTER" || flag.IsNotAllowed() {
		t.Fatalf("CLUSTER op = %s flag = %v", opstr, flag)
	}

	s, addr := openStartedProxy(t, newProxyConfig())
	defer s.Close()

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsError() || !strings.Contains(string(resp.Value), "command 'CLUSTER' is not allowed") {
		t.Fatalf("CLUSTER NODES default resp = %s %q", resp.Type, resp.Value)
	}
}

func TestClusterNodesSelfCommand(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf

	s, addr := openStartedProxy(t, config)
	defer s.Close()

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() {
		t.Fatalf("CLUSTER NODES resp type = %s value = %q", resp.Type, resp.Value)
	}
	nodes := parseClusterNodes(resp.Value)
	if len(nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1: %q", len(nodes), resp.Value)
	}
	node := nodes[0]
	if len(node) != 9 {
		t.Fatalf("node field count = %d, want 9: %v", len(node), node)
	}
	assertClusterNodeID(t, node[0])
	if !strings.Contains(node[1], "@") {
		t.Fatalf("node address missing bus port: %v", node)
	}
	if node[2] != "myself,master" || node[3] != "-" || node[7] != "connected" || node[8] != "0-16383" {
		t.Fatalf("unexpected self node fields: %v", node)
	}
}

func TestClusterNodesUnsupportedSubcommand(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf

	s, addr := openStartedProxy(t, config)
	defer s.Close()

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLUSTER", "SLOTS")
	if !resp.IsError() || !strings.Contains(string(resp.Value), "unsupported CLUSTER subcommand") {
		t.Fatalf("CLUSTER SLOTS resp = %s %q", resp.Type, resp.Value)
	}
}

func TestClusterNodesRequiresAuth(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.SessionAuth = "secret"

	s, addr := openStartedProxy(t, config)
	defer s.Close()

	c := dialProxy(t, addr)
	defer c.Close()

	resp := proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsError() || string(resp.Value) != "NOAUTH Authentication required" {
		t.Fatalf("unauthorized CLUSTER NODES resp = %s %q", resp.Type, resp.Value)
	}

	resp = proxyCall(t, c, "AUTH", "secret")
	if !resp.IsString() {
		t.Fatalf("AUTH resp = %s %q", resp.Type, resp.Value)
	}

	resp = proxyCall(t, c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() || len(parseClusterNodes(resp.Value)) != 1 {
		t.Fatalf("authorized CLUSTER NODES resp = %s %q", resp.Type, resp.Value)
	}
}

func TestClusterNodesAllRequiresEtcdRegisterOnline(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatAll
	config.EtcdRegisterOnline = false
	err := config.Validate()
	if err == nil || !strings.Contains(err.Error(), "etcd_register_online") {
		t.Fatalf("Validate error = %v", err)
	}

	config.EtcdRegisterOnline = true
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate failed with etcd_register_online enabled: %v", err)
	}
}

func TestClusterNodesConfigEmptyCompatIsDisabled(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ""
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate failed for empty cluster_nodes_compat: %v", err)
	}
	if config.ClusterNodesCompat != ClusterNodesCompatDisabled {
		t.Fatalf("ClusterNodesCompat = %q, want %q", config.ClusterNodesCompat, ClusterNodesCompatDisabled)
	}
}

func TestFormatClusterNodesAndSlotDistribution(t *testing.T) {
	nodes := assignClusterSlots([]clusterNode{
		{ID: strings.Repeat("3", 40), Addr: "127.0.0.3:19000"},
		{ID: strings.Repeat("1", 40), Addr: "127.0.0.1:19000", IsSelf: true},
		{ID: strings.Repeat("2", 40), Addr: "127.0.0.2:19000"},
	})
	assertClusterSlotCoverage(t, nodes)
	if got := []string{
		formatClusterSlotRange(nodes[0].SlotStart, nodes[0].SlotEnd),
		formatClusterSlotRange(nodes[1].SlotStart, nodes[1].SlotEnd),
		formatClusterSlotRange(nodes[2].SlotStart, nodes[2].SlotEnd),
	}; got[0] != "0-5461" || got[1] != "5462-10922" || got[2] != "10923-16383" {
		t.Fatalf("slot ranges = %v", got)
	}

	lines := parseClusterNodes(formatClusterNodes(nodes))
	if len(lines) != 3 {
		t.Fatalf("lines len = %d", len(lines))
	}
	if lines[0][0] != strings.Repeat("1", 40) || lines[0][2] != "myself,master" || lines[0][8] != "0-5461" {
		t.Fatalf("line[0] = %v", lines[0])
	}
	if !strings.Contains(lines[0][1], "@29000") {
		t.Fatalf("line[0] address = %s", lines[0][1])
	}
}

func TestClusterNodesNormalizeDuplicateNodes(t *testing.T) {
	config := newProxyConfig()
	config.ProductName = "codis-test"
	self := &models.Proxy{Token: "self", ProxyAddr: "127.0.0.1:19000", ProductName: config.ProductName}
	provider := newClusterNodesProvider(config, self, nil)

	nodes := provider.normalizeClusterNodes([]clusterNode{
		{Token: "token-a", Addr: "127.0.0.9:19000"},
		{Token: "token-a", Addr: "127.0.0.2:19000"},
		{Token: "token-c", Addr: "127.0.0.3:19000"},
		{Token: "token-d", Addr: "127.0.0.3:19000"},
		{Token: "stale-self", Addr: self.ProxyAddr},
	})
	if len(nodes) != 3 {
		t.Fatalf("nodes len = %d, want 3: %+v", len(nodes), nodes)
	}
	if countSelfNodes(nodes) != 1 {
		t.Fatalf("self node count = %d, nodes = %+v", countSelfNodes(nodes), nodes)
	}
	assertHasClusterNode(t, nodes, "self", "127.0.0.1:19000")
	assertHasClusterNode(t, nodes, "token-a", "127.0.0.2:19000")
	assertHasClusterNode(t, nodes, "token-c", "127.0.0.3:19000")
}

func TestClusterNodesDiscoveryRefreshAndFallback(t *testing.T) {
	config := newProxyConfig()
	config.ProductName = "codis-test"
	config.ClusterNodesCompat = ClusterNodesCompatAll
	config.EtcdRegisterOnline = true
	config.ClusterNodesRefreshPeriod.Set(10 * time.Millisecond)

	self := &models.Proxy{Token: "self", ProxyAddr: "127.0.0.1:19000", ProductName: config.ProductName}
	lister := newFakeOnlineLister(
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.2:19000"},
		models.OnlineRecord{Token: "self", Addr: "127.0.0.1:19000"},
		models.OnlineRecord{Token: "token-b", Addr: "127.0.0.3:19000"},
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.9:19000"},
		models.OnlineRecord{Token: "bad-addr", Addr: ""},
		models.OnlineRecord{Token: "", Addr: "127.0.0.4:19000"},
	)

	provider := newClusterNodesProvider(config, self, lister)
	defer provider.Close()
	if err := provider.refresh(); err != nil {
		t.Fatal(err)
	}
	nodes := provider.snapshot()
	if len(nodes) != 3 {
		t.Fatalf("nodes len = %d, want 3: %+v", len(nodes), nodes)
	}
	assertClusterSlotCoverage(t, nodes)
	if countSelfNodes(nodes) != 1 {
		t.Fatalf("self node count = %d, nodes = %+v", countSelfNodes(nodes), nodes)
	}
	for _, node := range nodes {
		assertClusterNodeID(t, node.ID)
	}
	assertHasClusterNode(t, nodes, "self", "127.0.0.1:19000")
	assertHasClusterNode(t, nodes, "token-a", "127.0.0.2:19000")
	assertHasClusterNode(t, nodes, "token-b", "127.0.0.3:19000")

	lister.setListError(fmt.Errorf("list failed"))
	if err := provider.refresh(); err == nil {
		t.Fatal("expected refresh error")
	}
	if got := provider.snapshot(); len(got) != 3 {
		t.Fatalf("snapshot should keep last good nodes, got %d", len(got))
	}

	lister.setListError(nil)
	lister.setRecords(
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.2:19000"},
	)
	provider.Start()
	waitUntil(t, func() bool {
		return len(provider.snapshot()) == 2
	})
	lister.setRecords(
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.2:19000"},
		models.OnlineRecord{Token: "token-b", Addr: "127.0.0.3:19000"},
	)
	waitUntil(t, func() bool {
		return len(provider.snapshot()) == 3
	})

	lister2 := newFakeOnlineLister()
	lister2.setListError(fmt.Errorf("list failed"))
	provider2 := newClusterNodesProvider(config, self, lister2)
	defer provider2.Close()
	if err := provider2.refresh(); err == nil {
		t.Fatal("expected initial refresh error")
	}
	if got := provider2.snapshot(); len(got) != 1 || !got[0].IsSelf {
		t.Fatalf("initial failure should keep self fallback, got %+v", got)
	}

	lister3 := newFakeOnlineLister()
	provider3 := newClusterNodesProvider(config, self, lister3)
	defer provider3.Close()
	if err := provider3.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := provider3.snapshot(); len(got) != 1 || !got[0].IsSelf {
		t.Fatalf("empty online list should only return self, got %+v", got)
	}

	// A server-side removal disappears no later than the next successful read.
	lister.setRecords(
		models.OnlineRecord{Token: "token-a", Addr: "127.0.0.2:19000"},
	)
	waitUntil(t, func() bool {
		got := provider.snapshot()
		return len(got) == 2 && countSelfNodes(got) == 1
	})
}

func assertHasClusterNode(t *testing.T, nodes []clusterNode, token, addr string) {
	t.Helper()
	for _, node := range nodes {
		if node.Token == token && node.Addr == addr {
			return
		}
	}
	t.Fatalf("missing cluster node token=%s addr=%s in %+v", token, addr, nodes)
}

func countSelfNodes(nodes []clusterNode) int {
	n := 0
	for _, node := range nodes {
		if node.IsSelf {
			n++
		}
	}
	return n
}

func redisMultiBulk(args ...string) []*redis.Resp {
	multi := make([]*redis.Resp, len(args))
	for i, arg := range args {
		multi[i] = redis.NewBulkBytes([]byte(arg))
	}
	return multi
}

// fakeOnlineLister is the in-process stand-in for an injected OnlineLister
// (the real etcd-backed one lives outside pkg/proxy, rule D2).
type fakeOnlineLister struct {
	mu      sync.RWMutex
	records []models.OnlineRecord
	err     error
	calls   int
}

func newFakeOnlineLister(records ...models.OnlineRecord) *fakeOnlineLister {
	return &fakeOnlineLister{records: records}
}

func (l *fakeOnlineLister) setRecords(records ...models.OnlineRecord) {
	l.mu.Lock()
	l.records = records
	l.mu.Unlock()
}

func (l *fakeOnlineLister) setListError(err error) {
	l.mu.Lock()
	l.err = err
	l.mu.Unlock()
}

func (l *fakeOnlineLister) callCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.calls
}

func (l *fakeOnlineLister) List(ctx context.Context) ([]models.OnlineRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	return append([]models.OnlineRecord(nil), l.records...), nil
}
