// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

const (
	ClusterNodesCompatDisabled = "disabled"
	ClusterNodesCompatSelf     = "self"
	ClusterNodesCompatAll      = "all"

	redisClusterSlotCount = 16384
)

type clusterNodesProvider struct {
	mu sync.RWMutex

	config *Config
	self   *models.Proxy
	client models.Client

	nodes []clusterNode

	exit chan struct{}
	once sync.Once
	wg   sync.WaitGroup
}

type clusterNode struct {
	ID        string
	Token     string
	Addr      string
	IsSelf    bool
	SlotStart int
	SlotEnd   int
}

type jodisProxyNode struct {
	Addr  string `json:"addr"`
	Token string `json:"token"`
	State string `json:"state"`
}

func newClusterNodesProviderForProxy(config *Config, self *models.Proxy) (*clusterNodesProvider, error) {
	var client models.Client
	if config.ClusterNodesCompat == ClusterNodesCompatAll {
		c, err := models.NewClient(config.JodisName, config.JodisAddr, config.JodisAuth, config.JodisTimeout.Duration())
		if err != nil {
			return nil, errors.Trace(err)
		}
		client = c
	}
	return newClusterNodesProvider(config, self, client), nil
}

func newClusterNodesProvider(config *Config, self *models.Proxy, client models.Client) *clusterNodesProvider {
	p := &clusterNodesProvider{
		config: config,
		self:   cloneProxyModel(self),
		client: client,
		exit:   make(chan struct{}),
	}
	p.setNodes(p.selfFallbackNodes())
	return p
}

func cloneProxyModel(p *models.Proxy) *models.Proxy {
	if p == nil {
		return nil
	}
	x := *p
	return &x
}

func (p *clusterNodesProvider) Start() {
	if p == nil || p.config.ClusterNodesCompat != ClusterNodesCompatAll || p.client == nil {
		return
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.refresh()
		ticker := time.NewTicker(p.config.ClusterNodesRefreshPeriod.Duration())
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.refresh()
			case <-p.exit:
				return
			}
		}
	}()
}

func (p *clusterNodesProvider) Close() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		close(p.exit)
		p.wg.Wait()
		if p.client != nil {
			if err := p.client.Close(); err != nil {
				log.WarnErrorf(err, "close cluster nodes provider client failed")
			}
		}
	})
}

func (s *Router) clusterNodesDisabled() bool {
	return s == nil || s.clusterNodes == nil ||
		s.clusterNodes.config.ClusterNodesCompat == ClusterNodesCompatDisabled
}

func (s *Router) handleRequestCluster(r *Request) error {
	if s.clusterNodes == nil {
		return fmt.Errorf("command '%s' is not allowed", r.OpStr)
	}
	return s.clusterNodes.handleRequestCluster(r)
}

func (p *clusterNodesProvider) handleRequestCluster(r *Request) error {
	if p.config.ClusterNodesCompat == ClusterNodesCompatDisabled {
		return fmt.Errorf("command '%s' is not allowed", r.OpStr)
	}
	if len(r.Multi) < 2 {
		r.Resp = redis.NewErrorf("ERR wrong number of arguments for 'CLUSTER' command")
		return nil
	}
	subcmd := strings.ToUpper(string(r.Multi[1].Value))
	switch subcmd {
	case "NODES":
		if len(r.Multi) != 2 {
			r.Resp = redis.NewErrorf("ERR wrong number of arguments for 'CLUSTER NODES' command")
			return nil
		}
		r.Resp = redis.NewBulkBytes(formatClusterNodes(p.snapshot()))
	default:
		r.Resp = redis.NewErrorf("ERR unsupported CLUSTER subcommand '%s'", string(r.Multi[1].Value))
	}
	return nil
}

func (p *clusterNodesProvider) refresh() error {
	if p == nil || p.config.ClusterNodesCompat != ClusterNodesCompatAll || p.client == nil {
		return nil
	}
	nodes, err := p.loadJodisNodes()
	if err != nil {
		log.WarnErrorf(err, "refresh cluster nodes from jodis failed")
		return err
	}
	p.setNodes(nodes)
	return nil
}

func (p *clusterNodesProvider) loadJodisNodes() ([]clusterNode, error) {
	paths, err := p.client.List(p.jodisPrefix(), false)
	if err != nil {
		return nil, errors.Trace(err)
	}
	nodes := make([]clusterNode, 0, len(paths)+1)
	for _, path := range paths {
		b, err := p.client.Read(path, true)
		if err != nil {
			return nil, errors.Trace(err)
		}
		var node jodisProxyNode
		if err := json.Unmarshal(b, &node); err != nil {
			log.WarnErrorf(err, "decode jodis proxy node %s failed", path)
			continue
		}
		if node.Addr == "" {
			continue
		}
		if node.State != "" && strings.ToLower(node.State) != "online" {
			continue
		}
		if node.Token == "" {
			node.Token = tokenFromJodisPath(path)
		}
		nodes = append(nodes, clusterNode{
			Token: node.Token,
			Addr:  node.Addr,
		})
	}
	nodes = p.normalizeClusterNodes(nodes)
	nodes = p.ensureSelfNode(nodes)
	return assignClusterSlots(p.fillClusterNodeIDs(nodes)), nil
}

func (p *clusterNodesProvider) normalizeClusterNodes(nodes []clusterNode) []clusterNode {
	if len(nodes) == 0 {
		return nil
	}
	normalized := make([]clusterNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Addr == "" {
			continue
		}
		if sameClusterNode(node, p.self) {
			node.Token = p.self.Token
			node.Addr = p.self.ProxyAddr
			node.IsSelf = true
		}
		normalized = append(normalized, node)
	}
	if len(normalized) == 0 {
		return nil
	}
	sortClusterNodesForDedup(normalized)

	byToken := make(map[string]clusterNode, len(normalized))
	tokenOrder := make([]string, 0, len(normalized))
	addrOnly := make([]clusterNode, 0)
	for _, node := range normalized {
		if node.Token == "" {
			addrOnly = append(addrOnly, node)
			continue
		}
		if kept, ok := byToken[node.Token]; ok {
			log.Warnf("ignore duplicate cluster node token %s addr=%s kept=%s", node.Token, node.Addr, kept.Addr)
			continue
		}
		byToken[node.Token] = node
		tokenOrder = append(tokenOrder, node.Token)
	}

	tokenDeduped := make([]clusterNode, 0, len(tokenOrder)+len(addrOnly))
	for _, token := range tokenOrder {
		tokenDeduped = append(tokenDeduped, byToken[token])
	}
	tokenDeduped = append(tokenDeduped, addrOnly...)
	sortClusterNodesForDedup(tokenDeduped)

	byAddr := make(map[string]clusterNode, len(tokenDeduped))
	addrOrder := make([]string, 0, len(tokenDeduped))
	for _, node := range tokenDeduped {
		if kept, ok := byAddr[node.Addr]; ok {
			log.Warnf("ignore duplicate cluster node addr %s token=%s kept=%s", node.Addr, node.Token, kept.Token)
			continue
		}
		byAddr[node.Addr] = node
		addrOrder = append(addrOrder, node.Addr)
	}

	out := make([]clusterNode, 0, len(addrOrder))
	for _, addr := range addrOrder {
		out = append(out, byAddr[addr])
	}
	return out
}

func sortClusterNodesForDedup(nodes []clusterNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsSelf != nodes[j].IsSelf {
			return nodes[i].IsSelf
		}
		if nodes[i].Token != nodes[j].Token {
			return nodes[i].Token < nodes[j].Token
		}
		return nodes[i].Addr < nodes[j].Addr
	})
}

func (p *clusterNodesProvider) jodisPrefix() string {
	if p.config.JodisCompatible {
		return filepath.Join("/zk/codis", fmt.Sprintf("db_%s", p.config.ProductName), "proxy")
	}
	return filepath.Join(models.JodisDir, p.config.ProductName)
}

func (p *clusterNodesProvider) selfFallbackNodes() []clusterNode {
	if p.self == nil || p.self.ProxyAddr == "" {
		return nil
	}
	nodes := []clusterNode{{
		Token:  p.self.Token,
		Addr:   p.self.ProxyAddr,
		IsSelf: true,
	}}
	return assignClusterSlots(p.fillClusterNodeIDs(nodes))
}

func (p *clusterNodesProvider) ensureSelfNode(nodes []clusterNode) []clusterNode {
	if p.self == nil || p.self.ProxyAddr == "" {
		return nodes
	}
	for i := range nodes {
		if sameClusterNode(nodes[i], p.self) {
			nodes[i].Token = p.self.Token
			nodes[i].Addr = p.self.ProxyAddr
			nodes[i].IsSelf = true
			return nodes
		}
	}
	return append(nodes, clusterNode{
		Token:  p.self.Token,
		Addr:   p.self.ProxyAddr,
		IsSelf: true,
	})
}

func (p *clusterNodesProvider) fillClusterNodeIDs(nodes []clusterNode) []clusterNode {
	for i := range nodes {
		nodes[i].ID = fakeClusterNodeID(p.config.ProductName, nodes[i].Token, nodes[i].Addr)
		if p.self != nil && sameClusterNode(nodes[i], p.self) {
			nodes[i].IsSelf = true
		}
	}
	return nodes
}

func sameClusterNode(node clusterNode, self *models.Proxy) bool {
	if self == nil {
		return false
	}
	if node.Token != "" && self.Token != "" && node.Token == self.Token {
		return true
	}
	return node.Addr != "" && node.Addr == self.ProxyAddr
}

func tokenFromJodisPath(path string) string {
	token := filepath.Base(path)
	return strings.TrimPrefix(token, "proxy-")
}

func fakeClusterNodeID(product, token, addr string) string {
	sum := sha1.Sum([]byte(product + "\x00" + token + "\x00" + addr))
	return fmt.Sprintf("%x", sum)
}

func assignClusterSlots(nodes []clusterNode) []clusterNode {
	if len(nodes) == 0 {
		return nil
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].ID != nodes[j].ID {
			return nodes[i].ID < nodes[j].ID
		}
		if nodes[i].Token != nodes[j].Token {
			return nodes[i].Token < nodes[j].Token
		}
		return nodes[i].Addr < nodes[j].Addr
	})
	base := redisClusterSlotCount / len(nodes)
	remainder := redisClusterSlotCount % len(nodes)
	start := 0
	for i := range nodes {
		width := base
		if i < remainder {
			width++
		}
		nodes[i].SlotStart = start
		nodes[i].SlotEnd = start + width - 1
		start += width
	}
	return nodes
}

func (p *clusterNodesProvider) setNodes(nodes []clusterNode) {
	if len(nodes) == 0 {
		nodes = p.selfFallbackNodes()
	}
	p.mu.Lock()
	p.nodes = append([]clusterNode(nil), nodes...)
	p.mu.Unlock()
}

func (p *clusterNodesProvider) snapshot() []clusterNode {
	p.mu.RLock()
	nodes := append([]clusterNode(nil), p.nodes...)
	p.mu.RUnlock()
	if len(nodes) == 0 {
		return p.selfFallbackNodes()
	}
	return nodes
}

func formatClusterNodes(nodes []clusterNode) []byte {
	var b strings.Builder
	for _, node := range nodes {
		flags := "master"
		if node.IsSelf {
			flags = "myself,master"
		}
		fmt.Fprintf(&b, "%s %s@%d %s - 0 0 0 connected %s\n",
			node.ID, node.Addr, clusterBusPort(node.Addr), flags, formatClusterSlotRange(node.SlotStart, node.SlotEnd))
	}
	return []byte(b.String())
}

func formatClusterSlotRange(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

func clusterBusPort(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return n + 10000
}
