// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
	"github.com/CodisLabs/codis/pkg/utils/sync2"
)

const (
	hotKeyCacheBroadcastMinTimeout     = 10 * time.Millisecond
	hotKeyCacheBroadcastDefaultTimeout = 100 * time.Millisecond
	hotKeyCacheBroadcastMaxTimeout     = time.Second
)

func normalizeHotKeyCacheBroadcastTimeout(timeoutMillis int64) time.Duration {
	if timeoutMillis <= 0 {
		return hotKeyCacheBroadcastDefaultTimeout
	}
	if timeoutMillis < int64(hotKeyCacheBroadcastMinTimeout/time.Millisecond) {
		return hotKeyCacheBroadcastMinTimeout
	}
	if timeoutMillis > int64(hotKeyCacheBroadcastMaxTimeout/time.Millisecond) {
		return hotKeyCacheBroadcastMaxTimeout
	}
	return time.Duration(timeoutMillis) * time.Millisecond
}

func (s *Topom) HotKeyCacheInvalidate(req *proxy.HotKeyCacheBroadcastRequest) (*proxy.HotKeyCacheBroadcastResult, error) {
	if req == nil || req.SourceProxyToken == "" {
		return nil, errors.New("missing source proxy token")
	}
	if len(req.Keys) == 0 {
		return &proxy.HotKeyCacheBroadcastResult{}, nil
	}
	if len(req.Keys) > proxy.HotKeyCacheBroadcastMaxKeys {
		return nil, errors.Errorf("too many hot key cache invalidation keys: %d", len(req.Keys))
	}

	targets, err := s.hotKeyCacheFanoutTargets(req.SourceProxyToken)
	if err != nil {
		return nil, err
	}

	timeout := normalizeHotKeyCacheBroadcastTimeout(req.TimeoutMillis)
	var fut sync2.Future
	for _, p := range targets {
		fut.Add()
		go func(p *models.Proxy) {
			c := s.newProxyClient(p)
			c.SetTimeout(timeout)
			result, err := c.InvalidateHotKeyCache(&proxy.HotKeyCacheInvalidationRequest{
				Database: req.Database,
				Keys:     req.Keys,
			})
			if err != nil {
				log.WarnErrorf(err, "proxy-[%s] hot key cache invalidation failed", p.Token)
				fut.Done(p.Token, err)
				return
			}
			fut.Done(p.Token, result)
		}(p)
	}

	result := &proxy.HotKeyCacheBroadcastResult{TotalProxies: len(targets)}
	for token, v := range fut.Wait() {
		switch x := v.(type) {
		case error:
			if x != nil {
				result.FailedProxies++
			}
		case *proxy.HotKeyCacheInvalidationResult:
			if x != nil {
				result.Invalidated += x.Invalidated
			}
		default:
			log.Warnf("proxy-[%s] hot key cache invalidation returned unexpected result", token)
			result.FailedProxies++
		}
	}
	return result, nil
}

func (s *Topom) hotKeyCacheFanoutTargets(sourceProxyToken string) ([]*models.Proxy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosedTopom
	}
	ctx, err := s.newContext()
	if err != nil {
		return nil, err
	}
	if ctx.proxy[sourceProxyToken] == nil {
		return nil, errors.Errorf("proxy-[%s] does not exist", sourceProxyToken)
	}

	targets := make([]*models.Proxy, 0, len(ctx.proxy))
	for _, p := range ctx.proxy {
		if p.Token == sourceProxyToken {
			continue
		}
		cp := *p
		targets = append(targets, &cp)
	}
	return targets, nil
}
