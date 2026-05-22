// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/utils/sync2/atomic2"
)

type hotKeyCacheKey struct {
	database int32
	key      string
}

type hotKeyCacheEntry struct {
	key      hotKeyCacheKey
	value    []byte
	slot     int
	expireAt int64
	element  *list.Element
}

type HotKeyCacheStats struct {
	Enabled             bool  `json:"enabled"`
	Entries             int   `json:"entries"`
	Hits                int64 `json:"hits"`
	Misses              int64 `json:"misses"`
	Stores              int64 `json:"stores"`
	Invalidations       int64 `json:"invalidations"`
	Evictions           int64 `json:"evictions"`
	BroadcastAttempts   int64 `json:"broadcast_attempts"`
	BroadcastFailures   int64 `json:"broadcast_failures"`
	BroadcastDropped    int64 `json:"broadcast_dropped"`
	BroadcastCoalesced  int64 `json:"broadcast_coalesced"`
	RemoteInvalidations int64 `json:"remote_invalidations"`
}

func (s HotKeyCacheStats) Visible() bool {
	return s.Enabled || s.Entries != 0 || s.Hits != 0 || s.Misses != 0 ||
		s.Stores != 0 || s.Invalidations != 0 || s.Evictions != 0 ||
		s.BroadcastAttempts != 0 || s.BroadcastFailures != 0 || s.BroadcastDropped != 0 ||
		s.BroadcastCoalesced != 0 || s.RemoteInvalidations != 0
}

type HotKeyCache struct {
	enabled      bool
	ttl          time.Duration
	maxEntries   int
	maxValueSize int
	keys         map[string]struct{}

	mu      sync.Mutex
	entries map[hotKeyCacheKey]*hotKeyCacheEntry
	lru     *list.List

	hits                atomic2.Int64
	misses              atomic2.Int64
	stores              atomic2.Int64
	invalidations       atomic2.Int64
	evictions           atomic2.Int64
	broadcastAttempts   atomic2.Int64
	broadcastFailures   atomic2.Int64
	broadcastDropped    atomic2.Int64
	broadcastCoalesced  atomic2.Int64
	remoteInvalidations atomic2.Int64
	version             atomic2.Int64
}

type hotKeyCacheToken struct {
	ok           bool
	database     int32
	key          string
	slot         int
	slotVersion  int64
	cacheVersion int64
}

type hotKeyCacheInvalidationPlan struct {
	router   *Router
	database int32
	clearDB  bool
	keys     [][]byte
}

func newHotKeyCache(config *Config) *HotKeyCache {
	c := &HotKeyCache{
		enabled:      config.HotKeyCacheEnabled,
		ttl:          config.HotKeyCacheTTL.Duration(),
		maxEntries:   config.HotKeyCacheMaxEntries,
		maxValueSize: config.HotKeyCacheMaxValueSize.AsInt(),
		keys:         make(map[string]struct{}),
		entries:      make(map[hotKeyCacheKey]*hotKeyCacheEntry),
		lru:          list.New(),
	}
	for _, key := range config.HotKeyCacheKeys {
		c.keys[key] = struct{}{}
	}
	return c
}

func (c *HotKeyCache) Enabled() bool {
	return c != nil && c.enabled && c.ttl > 0 && c.maxEntries > 0 && len(c.keys) != 0
}

func (c *HotKeyCache) isConfigured(key []byte) bool {
	if !c.Enabled() {
		return false
	}
	_, ok := c.keys[string(key)]
	return ok
}

func (c *HotKeyCache) get(k hotKeyCacheKey, now int64) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e := c.entries[k]
	if e == nil {
		c.misses.Incr()
		return nil, false
	}
	if now >= e.expireAt {
		c.removeElement(e, false)
		c.misses.Incr()
		return nil, false
	}
	c.lru.MoveToFront(e.element)
	c.hits.Incr()
	return append([]byte(nil), e.value...), true
}

func (c *HotKeyCache) store(k hotKeyCacheKey, slot int, value []byte, now int64) {
	if !c.Enabled() || value == nil || len(value) > c.maxValueSize {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if e := c.entries[k]; e != nil {
		e.value = append(e.value[:0], value...)
		e.slot = slot
		e.expireAt = now + int64(c.ttl)
		c.lru.MoveToFront(e.element)
		c.stores.Incr()
		return
	}

	e := &hotKeyCacheEntry{
		key:      k,
		value:    append([]byte(nil), value...),
		slot:     slot,
		expireAt: now + int64(c.ttl),
	}
	e.element = c.lru.PushFront(e)
	c.entries[k] = e
	c.stores.Incr()

	for len(c.entries) > c.maxEntries {
		back := c.lru.Back()
		if back == nil {
			break
		}
		c.removeElement(back.Value.(*hotKeyCacheEntry), true)
	}
}

func (c *HotKeyCache) invalidateKey(database int32, key []byte) {
	if !c.Enabled() {
		return
	}
	c.version.Incr()
	k := hotKeyCacheKey{database: database, key: string(key)}

	c.mu.Lock()
	if e := c.entries[k]; e != nil {
		c.removeElement(e, false)
		c.invalidations.Incr()
	}
	c.mu.Unlock()
}

func (c *HotKeyCache) invalidateRemote(database int32, keys [][]byte) int64 {
	if !c.Enabled() {
		return 0
	}
	var configured bool
	for _, key := range keys {
		if c.isConfigured(key) {
			configured = true
			break
		}
	}
	if !configured {
		return 0
	}
	c.version.Incr()

	c.mu.Lock()
	var n int64
	for _, key := range keys {
		ktext := string(key)
		if _, ok := c.keys[ktext]; !ok {
			continue
		}
		k := hotKeyCacheKey{database: database, key: ktext}
		if e := c.entries[k]; e != nil {
			c.removeElement(e, false)
			n++
		}
	}
	if n != 0 {
		c.invalidations.Add(n)
		c.remoteInvalidations.Add(n)
	}
	c.mu.Unlock()
	return n
}

func (c *HotKeyCache) invalidateDatabase(database int32) {
	if !c.Enabled() {
		return
	}
	c.version.Incr()

	c.mu.Lock()
	var n int64
	for _, e := range c.entries {
		if e.key.database == database {
			c.removeElement(e, false)
			n++
		}
	}
	if n != 0 {
		c.invalidations.Add(n)
	}
	c.mu.Unlock()
}

func (c *HotKeyCache) invalidateSlot(slot int) {
	if !c.Enabled() {
		return
	}
	c.version.Incr()

	c.mu.Lock()
	var n int64
	for _, e := range c.entries {
		if e.slot == slot {
			c.removeElement(e, false)
			n++
		}
	}
	if n != 0 {
		c.invalidations.Add(n)
	}
	c.mu.Unlock()
}

func (c *HotKeyCache) removeElement(e *hotKeyCacheEntry, eviction bool) {
	delete(c.entries, e.key)
	c.lru.Remove(e.element)
	if eviction {
		c.evictions.Incr()
	}
}

func (c *HotKeyCache) Stats() HotKeyCacheStats {
	if c == nil {
		return HotKeyCacheStats{}
	}
	c.mu.Lock()
	entries := len(c.entries)
	c.mu.Unlock()
	return HotKeyCacheStats{
		Enabled:             c.Enabled(),
		Entries:             entries,
		Hits:                c.hits.Int64(),
		Misses:              c.misses.Int64(),
		Stores:              c.stores.Int64(),
		Invalidations:       c.invalidations.Int64(),
		Evictions:           c.evictions.Int64(),
		BroadcastAttempts:   c.broadcastAttempts.Int64(),
		BroadcastFailures:   c.broadcastFailures.Int64(),
		BroadcastDropped:    c.broadcastDropped.Int64(),
		BroadcastCoalesced:  c.broadcastCoalesced.Int64(),
		RemoteInvalidations: c.remoteInvalidations.Int64(),
	}
}

func (s *Router) hotKeyCacheEnabled() bool {
	return s != nil && s.hotKeyCache != nil && s.hotKeyCache.Enabled()
}

func (s *Router) hotKeyCacheInvalidateRemote(database int32, keys [][]byte) int64 {
	if s == nil || s.hotKeyCache == nil {
		return 0
	}
	return s.hotKeyCache.invalidateRemote(database, keys)
}

func (s *Router) hotKeyCacheToken(database int32, key []byte) hotKeyCacheToken {
	if !s.hotKeyCacheEnabled() || !s.hotKeyCache.isConfigured(key) {
		return hotKeyCacheToken{}
	}
	slot := int(Hash(key) % MaxSlotNum)
	slotVersion := s.slotVersions[slot].Int64()
	cacheVersion := s.hotKeyCache.version.Int64()
	if !s.hotKeyCacheSlotStable(slot) {
		return hotKeyCacheToken{}
	}
	return hotKeyCacheToken{
		ok:           true,
		database:     database,
		key:          string(key),
		slot:         slot,
		slotVersion:  slotVersion,
		cacheVersion: cacheVersion,
	}
}

func (s *Router) hotKeyCacheSlotStable(slot int) bool {
	if slot < 0 || slot >= MaxSlotNum {
		return false
	}
	x := &s.slots[slot]
	if !x.lock.TryRLock() {
		return false
	}
	stable := !x.lock.hold && x.backend.bc != nil && x.migrate.bc == nil
	x.lock.RUnlock()
	return stable
}

func (s *Router) hotKeyCacheLookup(database int32, key []byte) (*redis.Resp, hotKeyCacheToken, bool) {
	token := s.hotKeyCacheToken(database, key)
	if !token.ok {
		return nil, token, false
	}
	value, ok := s.hotKeyCache.get(hotKeyCacheKey{database: database, key: token.key}, time.Now().UnixNano())
	if !ok {
		return nil, token, false
	}
	return redis.NewBulkBytes(value), token, true
}

func (s *Router) hotKeyCacheStore(token hotKeyCacheToken, resp *redis.Resp) {
	if !token.ok || resp == nil || !resp.IsBulkBytes() || resp.Value == nil {
		return
	}
	if s.slotVersions[token.slot].Int64() != token.slotVersion ||
		s.hotKeyCache.version.Int64() != token.cacheVersion ||
		!s.hotKeyCacheSlotStable(token.slot) {
		return
	}
	s.hotKeyCache.store(
		hotKeyCacheKey{database: token.database, key: token.key},
		token.slot, resp.Value, time.Now().UnixNano(),
	)
}

func (s *Router) handleRequestGet(r *Request) error {
	if len(r.Multi) != 2 || !s.hotKeyCacheEnabled() {
		return s.dispatch(r)
	}
	if resp, _, ok := s.hotKeyCacheLookup(r.Database, r.Multi[1].Value); ok {
		r.Resp = resp
		return nil
	}
	token := s.hotKeyCacheToken(r.Database, r.Multi[1].Value)
	if err := s.dispatch(r); err != nil {
		return err
	}
	if token.ok {
		r.Coalesce = func() error {
			if r.Err != nil {
				return nil
			}
			s.hotKeyCacheStore(token, r.Resp)
			return nil
		}
	}
	return nil
}

func (s *Session) handleRequestMGetWithHotKeyCache(r *Request, d *Router) error {
	nkeys := len(r.Multi) - 1
	switch {
	case nkeys == 0:
		r.Resp = redis.NewErrorf("ERR wrong number of arguments for 'MGET' command")
		return nil
	case !d.hotKeyCacheEnabled():
		return s.handleRequestMGet(r, d)
	}

	type miss struct {
		index int
		token hotKeyCacheToken
		req   *Request
	}

	values := make([]*redis.Resp, nkeys)
	misses := make([]miss, 0, nkeys)
	sub := r.MakeSubRequest(nkeys)

	for i := 0; i < nkeys; i++ {
		key := r.Multi[i+1].Value
		if resp, _, ok := d.hotKeyCacheLookup(r.Database, key); ok {
			values[i] = resp
			continue
		}
		token := d.hotKeyCacheToken(r.Database, key)
		x := &sub[i]
		x.Multi = []*redis.Resp{
			r.Multi[0],
			r.Multi[i+1],
		}
		if err := d.dispatch(x); err != nil {
			return err
		}
		misses = append(misses, miss{index: i, token: token, req: x})
	}

	r.Coalesce = func() error {
		for _, m := range misses {
			if err := m.req.Err; err != nil {
				return err
			}
			switch resp := m.req.Resp; {
			case resp == nil:
				return ErrRespIsRequired
			case resp.IsArray() && len(resp.Array) == 1:
				values[m.index] = resp.Array[0]
				d.hotKeyCacheStore(m.token, resp.Array[0])
			default:
				return fmt.Errorf("bad mget resp: %s array.len = %d", resp.Type, len(resp.Array))
			}
		}
		r.Resp = redis.NewArray(values)
		return nil
	}
	return nil
}

func (p hotKeyCacheInvalidationPlan) active() bool {
	return p.router != nil && (p.clearDB || len(p.keys) != 0)
}

func (p hotKeyCacheInvalidationPlan) apply(resp *redis.Resp) {
	if !p.active() {
		return
	}
	if p.clearDB {
		p.router.hotKeyCache.invalidateDatabase(p.database)
		return
	}
	var keys [][]byte
	if resp != nil && !resp.IsError() {
		keys = p.broadcastKeys()
	}
	for _, key := range p.keys {
		p.router.hotKeyCache.invalidateKey(p.database, key)
	}
	p.router.hotKeyCacheBroadcastKeys(p.database, keys)
}

func (p hotKeyCacheInvalidationPlan) broadcastKeys() [][]byte {
	if !p.active() || p.clearDB || p.router == nil || p.router.hotKeyCache == nil {
		return nil
	}
	keys := make([][]byte, 0, len(p.keys))
	seen := make(map[string]struct{}, len(p.keys))
	for _, key := range p.keys {
		ktext := string(key)
		if _, ok := seen[ktext]; ok {
			continue
		}
		if !p.router.hotKeyCache.isConfigured(key) {
			continue
		}
		seen[ktext] = struct{}{}
		keys = append(keys, append([]byte(nil), key...))
	}
	return keys
}

func (s *Router) hotKeyCacheInvalidationPlanForRequest(r *Request) hotKeyCacheInvalidationPlan {
	if !s.hotKeyCacheEnabled() || r == nil || r.OpFlag.IsReadOnly() {
		return hotKeyCacheInvalidationPlan{}
	}

	plan := hotKeyCacheInvalidationPlan{router: s, database: r.Database}

	switch r.OpStr {
	case "GET", "MGET":
		return hotKeyCacheInvalidationPlan{}
	case "EVAL", "EVALSHA":
		plan.clearDB = true
		return plan
	case "MSET":
		for i := 1; i+1 < len(r.Multi); i += 2 {
			plan.keys = append(plan.keys, append([]byte(nil), r.Multi[i].Value...))
		}
		return plan
	case "DEL":
		for i := 1; i < len(r.Multi); i++ {
			plan.keys = append(plan.keys, append([]byte(nil), r.Multi[i].Value...))
		}
		return plan
	}

	if (r.OpFlag&FlagMayWrite) != 0 && (r.OpFlag&FlagWrite) == 0 {
		plan.clearDB = true
		return plan
	}
	if len(r.Multi) > 1 {
		plan.keys = append(plan.keys, append([]byte(nil), r.Multi[1].Value...))
	}
	return plan
}

func (s *Router) handleRequestWithHotKeyCacheInvalidation(r *Request, dispatch func() error) error {
	plan := s.hotKeyCacheInvalidationPlanForRequest(r)
	if err := dispatch(); err != nil {
		return err
	}
	if !plan.active() || (r.Resp != nil && r.Coalesce == nil) {
		return nil
	}
	coalesce := r.Coalesce
	r.Coalesce = func() (err error) {
		if coalesce != nil {
			err = coalesce()
		}
		plan.apply(r.Resp)
		return err
	}
	return nil
}

func (s *Router) hotKeyCacheInvalidateSlot(slot int) {
	if s == nil || s.hotKeyCache == nil {
		return
	}
	s.slotVersions[slot].Incr()
	s.hotKeyCache.invalidateSlot(slot)
}

func (s *Router) hotKeyCacheBroadcastKeys(database int32, keys [][]byte) {
	if s == nil || s.hotKeyCache == nil || s.hotKeyCacheBroadcast == nil || len(keys) == 0 {
		return
	}
	if !s.hotKeyCacheBroadcast.Enabled() {
		return
	}
	for len(keys) != 0 {
		n := len(keys)
		if n > HotKeyCacheBroadcastMaxKeys {
			n = HotKeyCacheBroadcastMaxKeys
		}
		s.hotKeyCacheBroadcast.Enqueue(database, keys[:n])
		keys = keys[n:]
	}
}
