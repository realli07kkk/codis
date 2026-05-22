// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
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

	s, addr := openStartedProxy(newProxyConfig())
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "CLUSTER", "NODES")
	if !resp.IsError() || !strings.Contains(string(resp.Value), "command 'CLUSTER' is not allowed") {
		t.Fatalf("CLUSTER NODES default resp = %s %q", resp.Type, resp.Value)
	}
}

func TestClusterNodesSelfCommand(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "CLUSTER", "NODES")
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

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "CLUSTER", "SLOTS")
	if !resp.IsError() || !strings.Contains(string(resp.Value), "unsupported CLUSTER subcommand") {
		t.Fatalf("CLUSTER SLOTS resp = %s %q", resp.Type, resp.Value)
	}
}

func TestClusterNodesRequiresAuth(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatSelf
	config.SessionAuth = "secret"

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "CLUSTER", "NODES")
	if !resp.IsError() || string(resp.Value) != "NOAUTH Authentication required" {
		t.Fatalf("unauthorized CLUSTER NODES resp = %s %q", resp.Type, resp.Value)
	}

	resp = proxyCall(c, "AUTH", "secret")
	if !resp.IsString() {
		t.Fatalf("AUTH resp = %s %q", resp.Type, resp.Value)
	}

	resp = proxyCall(c, "CLUSTER", "NODES")
	if !resp.IsBulkBytes() || len(parseClusterNodes(resp.Value)) != 1 {
		t.Fatalf("authorized CLUSTER NODES resp = %s %q", resp.Type, resp.Value)
	}
}

func TestClusterNodesAllRequiresJodisConfig(t *testing.T) {
	config := newProxyConfig()
	config.ClusterNodesCompat = ClusterNodesCompatAll
	config.JodisName = ""
	config.JodisAddr = ""
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "all mode requires jodis_name and jodis_addr") {
		t.Fatalf("Validate error = %v", err)
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

func TestClusterNodesNormalizeDuplicateJodisNodes(t *testing.T) {
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
	config.JodisName = "fake"
	config.JodisAddr = "fake"
	config.ClusterNodesRefreshPeriod.Set(10 * time.Millisecond)

	self := &models.Proxy{Token: "self", ProxyAddr: "127.0.0.1:19000", ProductName: config.ProductName}
	client := newFakeClusterNodesClient(config.ProductName)
	client.putJodis("proxy-a", "token-a", "127.0.0.2:19000", "online")
	client.putJodis("proxy-self", "self", "127.0.0.1:19000", "online")
	client.putJodis("proxy-b", "token-b", "127.0.0.3:19000", "online")
	client.putRaw("bad-json", []byte("{"))
	client.putJodis("empty-addr", "empty", "", "online")
	client.putJodis("offline", "offline", "127.0.0.4:19000", "offline")

	provider := newClusterNodesProvider(config, self, client)
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

	client.setListError(fmt.Errorf("list failed"))
	if err := provider.refresh(); err == nil {
		t.Fatal("expected refresh error")
	}
	if got := provider.snapshot(); len(got) != 3 {
		t.Fatalf("snapshot should keep last good nodes, got %d", len(got))
	}

	client.setListError(nil)
	client.reset()
	client.putJodis("proxy-self", "self", "127.0.0.1:19000", "online")
	client.putJodis("proxy-a", "token-a", "127.0.0.2:19000", "online")
	provider.Start()
	defer provider.Close()
	waitUntil(t, func() bool {
		return len(provider.snapshot()) == 2
	})
	client.putJodis("proxy-b", "token-b", "127.0.0.3:19000", "online")
	waitUntil(t, func() bool {
		return len(provider.snapshot()) == 3
	})

	client2 := newFakeClusterNodesClient(config.ProductName)
	client2.setListError(fmt.Errorf("list failed"))
	provider2 := newClusterNodesProvider(config, self, client2)
	if err := provider2.refresh(); err == nil {
		t.Fatal("expected initial refresh error")
	}
	if got := provider2.snapshot(); len(got) != 1 || !got[0].IsSelf {
		t.Fatalf("initial failure should keep self fallback, got %+v", got)
	}
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

type fakeClusterNodesClient struct {
	mu      sync.RWMutex
	product string
	data    map[string][]byte
	listErr error
	closed  bool
}

func newFakeClusterNodesClient(product string) *fakeClusterNodesClient {
	return &fakeClusterNodesClient{
		product: product,
		data:    make(map[string][]byte),
	}
}

func (c *fakeClusterNodesClient) putJodis(name, token, addr, state string) {
	node := jodisProxyNode{Token: token, Addr: addr, State: state}
	b, err := json.Marshal(node)
	if err != nil {
		panic(err)
	}
	c.putRaw(name, b)
}

func (c *fakeClusterNodesClient) putRaw(name string, b []byte) {
	c.mu.Lock()
	c.data[filepath.Join(models.JodisDir, c.product, name)] = b
	c.mu.Unlock()
}

func (c *fakeClusterNodesClient) reset() {
	c.mu.Lock()
	c.data = make(map[string][]byte)
	c.mu.Unlock()
}

func (c *fakeClusterNodesClient) setListError(err error) {
	c.mu.Lock()
	c.listErr = err
	c.mu.Unlock()
}

func (c *fakeClusterNodesClient) Create(path string, data []byte) error { return nil }
func (c *fakeClusterNodesClient) Update(path string, data []byte) error { return nil }
func (c *fakeClusterNodesClient) Delete(path string) error              { return nil }

func (c *fakeClusterNodesClient) Read(path string, must bool) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	b := c.data[path]
	if b == nil && must {
		return nil, fmt.Errorf("path %s not found", path)
	}
	return b, nil
}

func (c *fakeClusterNodesClient) List(path string, must bool) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.listErr != nil {
		return nil, c.listErr
	}
	var paths []string
	prefix := path + "/"
	for p := range c.data {
		if strings.HasPrefix(p, prefix) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 && must {
		return nil, fmt.Errorf("path %s not found", path)
	}
	return paths, nil
}

func (c *fakeClusterNodesClient) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *fakeClusterNodesClient) WatchInOrder(path string) (<-chan struct{}, []string, error) {
	return make(chan struct{}), nil, nil
}

func (c *fakeClusterNodesClient) CreateEphemeral(path string, data []byte) (<-chan struct{}, error) {
	return make(chan struct{}), nil
}

func (c *fakeClusterNodesClient) CreateEphemeralInOrder(path string, data []byte) (<-chan struct{}, string, error) {
	return make(chan struct{}), path, nil
}
