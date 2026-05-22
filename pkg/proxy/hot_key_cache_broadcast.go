// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"fmt"
	"sync"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/log"
	"github.com/CodisLabs/codis/pkg/utils/rpc"
)

const (
	HotKeyCacheBroadcastMaxKeys = 1024

	defaultHotKeyCacheBroadcastQueueSize = 1024
	hotKeyCacheBroadcastFlushInterval    = 10 * time.Millisecond
)

type HotKeyCacheBroadcastRequest struct {
	SourceProxyToken string   `json:"source_proxy_token"`
	Database         int32    `json:"database"`
	Keys             [][]byte `json:"keys_base64"`
	TimeoutMillis    int64    `json:"timeout_millis,omitempty"`
}

type HotKeyCacheBroadcastResult struct {
	TotalProxies  int   `json:"total_proxies"`
	FailedProxies int   `json:"failed_proxies"`
	Invalidated   int64 `json:"invalidated,omitempty"`
}

type HotKeyCacheInvalidationRequest struct {
	Database int32    `json:"database"`
	Keys     [][]byte `json:"keys_base64"`
}

type HotKeyCacheInvalidationResult struct {
	TotalKeys   int   `json:"total_keys"`
	Invalidated int64 `json:"invalidated,omitempty"`
}

type hotKeyCacheBroadcastEvent struct {
	database int32
	keys     [][]byte
}

type hotKeyCacheBroadcastReporter struct {
	enabled bool
	timeout time.Duration

	stats *HotKeyCache
	queue chan hotKeyCacheBroadcastEvent
	stop  chan struct{}
	once  sync.Once

	mu               sync.RWMutex
	topomAdminAddr   string
	sourceProxyToken string
	xauth            string
}

func newHotKeyCacheBroadcastReporter(config *Config, stats *HotKeyCache) *hotKeyCacheBroadcastReporter {
	queueSize := config.HotKeyCacheBroadcastQueueSize
	if queueSize == 0 {
		queueSize = defaultHotKeyCacheBroadcastQueueSize
	}
	r := &hotKeyCacheBroadcastReporter{
		enabled: config.HotKeyCacheBroadcastEnabled,
		timeout: config.HotKeyCacheBroadcastTimeout.Duration(),
		stats:   stats,
	}
	if r.enabled && r.timeout > 0 && queueSize > 0 {
		r.queue = make(chan hotKeyCacheBroadcastEvent, queueSize)
		r.stop = make(chan struct{})
		go r.loop()
	}
	return r
}

func cloneHotKeyCacheKeys(keys [][]byte) [][]byte {
	if len(keys) == 0 {
		return nil
	}
	copied := make([][]byte, 0, len(keys))
	for _, key := range keys {
		copied = append(copied, append([]byte(nil), key...))
	}
	return copied
}

func (r *hotKeyCacheBroadcastReporter) Close() {
	if r == nil || r.stop == nil {
		return
	}
	r.once.Do(func() {
		close(r.stop)
	})
}

func (r *hotKeyCacheBroadcastReporter) SetSource(token, xauth string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.sourceProxyToken = token
	r.xauth = xauth
	r.mu.Unlock()
}

func (r *hotKeyCacheBroadcastReporter) SetTopomAdminAddr(addr string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.topomAdminAddr = addr
	r.mu.Unlock()
}

func (r *hotKeyCacheBroadcastReporter) Enabled() bool {
	if r == nil || !r.enabled || r.timeout <= 0 || r.queue == nil {
		return false
	}
	r.mu.RLock()
	ok := r.topomAdminAddr != "" && r.sourceProxyToken != "" && r.xauth != ""
	r.mu.RUnlock()
	return ok
}

func (r *hotKeyCacheBroadcastReporter) Enqueue(database int32, keys [][]byte) bool {
	if len(keys) == 0 || !r.Enabled() {
		return false
	}
	event := hotKeyCacheBroadcastEvent{
		database: database,
		keys:     cloneHotKeyCacheKeys(keys),
	}
	select {
	case r.queue <- event:
		return true
	default:
		if r.stats != nil {
			r.stats.broadcastDropped.Incr()
		}
		return false
	}
}

func (r *hotKeyCacheBroadcastReporter) loop() {
	pending := make(map[int32]map[string][]byte)
	var timer *time.Timer
	var timerC <-chan time.Time

	for {
		select {
		case event := <-r.queue:
			r.mergePending(pending, event)
			if timer == nil {
				timer = time.NewTimer(hotKeyCacheBroadcastFlushInterval)
				timerC = timer.C
			}
		case <-timerC:
			r.flushPending(pending)
			pending = make(map[int32]map[string][]byte)
			timer = nil
			timerC = nil
		case <-r.stop:
			if timer != nil && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func (r *hotKeyCacheBroadcastReporter) mergePending(pending map[int32]map[string][]byte, event hotKeyCacheBroadcastEvent) {
	keys := pending[event.database]
	if keys == nil {
		keys = make(map[string][]byte)
		pending[event.database] = keys
	}
	for _, key := range event.keys {
		k := string(key)
		if _, ok := keys[k]; ok {
			if r.stats != nil {
				r.stats.broadcastCoalesced.Incr()
			}
			continue
		}
		keys[k] = append([]byte(nil), key...)
	}
}

func (r *hotKeyCacheBroadcastReporter) flushPending(pending map[int32]map[string][]byte) {
	for database, m := range pending {
		keys := make([][]byte, 0, len(m))
		for _, key := range m {
			keys = append(keys, key)
		}
		if len(keys) == 0 || !r.Enabled() {
			continue
		}
		for len(keys) != 0 {
			n := len(keys)
			if n > HotKeyCacheBroadcastMaxKeys {
				n = HotKeyCacheBroadcastMaxKeys
			}
			if r.stats != nil {
				r.stats.broadcastAttempts.Incr()
			}
			if _, err := r.report(database, keys[:n]); err != nil {
				if r.stats != nil {
					r.stats.broadcastFailures.Incr()
				}
				log.WarnErrorf(err, "hot key cache invalidation broadcast failed")
			}
			keys = keys[n:]
		}
	}
}

func (r *hotKeyCacheBroadcastReporter) report(database int32, keys [][]byte) (*HotKeyCacheBroadcastResult, error) {
	if len(keys) == 0 {
		return &HotKeyCacheBroadcastResult{}, nil
	}
	r.mu.RLock()
	enabled := r.enabled && r.timeout > 0
	topomAdminAddr := r.topomAdminAddr
	sourceProxyToken := r.sourceProxyToken
	xauth := r.xauth
	timeout := r.timeout
	r.mu.RUnlock()

	if !enabled || topomAdminAddr == "" || sourceProxyToken == "" || xauth == "" {
		return &HotKeyCacheBroadcastResult{}, nil
	}

	req := &HotKeyCacheBroadcastRequest{
		SourceProxyToken: sourceProxyToken,
		Database:         database,
		Keys:             cloneHotKeyCacheKeys(keys),
		TimeoutMillis:    int64(timeout / time.Millisecond),
	}
	url := rpc.EncodeURL(topomAdminAddr, "/api/topom/hot-key-cache/invalidate/%s", xauth)
	result := &HotKeyCacheBroadcastResult{}
	if err := rpc.ApiPutJsonWithTimeout(url, req, result, timeout); err != nil {
		return nil, err
	}
	if result.FailedProxies != 0 {
		return result, fmt.Errorf("hot key cache invalidation failed on %d proxy(s)", result.FailedProxies)
	}
	return result, nil
}
