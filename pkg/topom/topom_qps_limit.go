// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"sort"
	"strings"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
	"github.com/CodisLabs/codis/pkg/utils/sync2"
)

type ProxyQPSLimitView struct {
	Revision   int64  `json:"revision"`
	Limit      int64  `json:"limit"`
	Enabled    bool   `json:"enabled"`
	SyncStatus string `json:"sync_status"`
}

type ProxyQPSLimitUpdateRequest struct {
	Limit int64 `json:"limit"`
}

func (s *Topom) GetProxyQPSLimit() (*ProxyQPSLimitView, error) {
	config, proxies, err := s.snapshotProxyQPSLimit()
	if err != nil {
		return nil, err
	}
	return newProxyQPSLimitView(config, s.proxyQPSLimitSyncStatus(proxies, config)), nil
}

func (s *Topom) UpdateProxyQPSLimit(req *ProxyQPSLimitUpdateRequest) (*ProxyQPSLimitView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, err := s.newContext()
	if err != nil {
		return nil, err
	}
	config, err := s.buildProxyQPSLimit(ctx.proxyQPSLimit, req)
	if err != nil {
		return nil, err
	}
	if err := s.storeUpdateProxyQPSLimit(config); err != nil {
		return nil, err
	}
	s.dirtyProxyQPSLimitCache()
	if _, err := s.syncProxyQPSLimitToProxies(ctx, config); err != nil {
		return nil, err
	}
	return newProxyQPSLimitView(config, "ready"), nil
}

func (s *Topom) snapshotProxyQPSLimit() (*models.ProxyQPSLimit, map[string]*models.Proxy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, err := s.newContext()
	if err != nil {
		return nil, nil, err
	}
	config := cloneProxyQPSLimit(ctx.proxyQPSLimit)
	proxies := cloneProxyQPSLimitProxies(ctx.proxy)
	return config, proxies, nil
}

func cloneProxyQPSLimit(config *models.ProxyQPSLimit) *models.ProxyQPSLimit {
	if config == nil {
		return nil
	}
	cp := *config
	return &cp
}

func cloneProxyQPSLimitProxies(input map[string]*models.Proxy) map[string]*models.Proxy {
	proxies := make(map[string]*models.Proxy, len(input))
	for token, p := range input {
		if p == nil {
			continue
		}
		cp := *p
		proxies[token] = &cp
	}
	return proxies
}

func (s *Topom) buildProxyQPSLimit(current *models.ProxyQPSLimit, req *ProxyQPSLimitUpdateRequest) (*models.ProxyQPSLimit, error) {
	if req == nil {
		return nil, errors.New("missing proxy qps limit request")
	}
	if req.Limit < 0 {
		return nil, errors.New("proxy qps limit must be >= 0")
	}
	revision := int64(1)
	if current != nil && current.Revision >= revision {
		revision = current.Revision + 1
	}
	return &models.ProxyQPSLimit{
		Revision:  revision,
		Limit:     req.Limit,
		UpdatedAt: time.Now().Format(time.RFC3339Nano),
	}, nil
}

func newProxyQPSLimitView(config *models.ProxyQPSLimit, status string) *ProxyQPSLimitView {
	view := &ProxyQPSLimitView{SyncStatus: status}
	if config == nil {
		view.SyncStatus = "not_configured"
		return view
	}
	view.Revision = config.Revision
	view.Limit = config.Limit
	view.Enabled = config.Limit > 0
	if config.Revision == 0 && config.Limit == 0 {
		view.SyncStatus = "not_configured"
	}
	return view
}

func (s *Topom) proxyQPSLimitSyncStatus(proxies map[string]*models.Proxy, config *models.ProxyQPSLimit) string {
	if !shouldSyncProxyQPSLimit(config) {
		return "not_configured"
	}
	var tokens []string
	for token := range proxies {
		if proxies[token] != nil {
			tokens = append(tokens, token)
		}
	}
	sort.Strings(tokens)

	var fut sync2.Future
	for _, token := range tokens {
		p := proxies[token]
		fut.Add()
		go func(token string, p *models.Proxy) {
			fut.Done(token, s.newProxyStats(p, time.Second))
		}(token, p)
	}
	stats := fut.Wait()

	var failed []string
	for _, token := range tokens {
		if !proxyQPSLimitApplied(stats[token], config) {
			failed = append(failed, token)
		}
	}
	if len(failed) != 0 {
		return "proxy_sync_failed:" + strings.Join(failed, ",")
	}
	return "ready"
}

func proxyQPSLimitApplied(v interface{}, config *models.ProxyQPSLimit) bool {
	stats, ok := v.(*ProxyStats)
	if !ok || stats == nil || stats.Timeout || stats.Error != nil || stats.Stats == nil {
		return false
	}
	qpsLimit := stats.Stats.QPSLimit
	return qpsLimit != nil && qpsLimit.Revision == config.Revision && qpsLimit.Limit == config.Limit
}

func shouldSyncProxyQPSLimit(config *models.ProxyQPSLimit) bool {
	return config != nil && config.Revision > 0
}

func (s *Topom) syncProxyQPSLimitToProxies(ctx *context, config *models.ProxyQPSLimit) ([]string, error) {
	if !shouldSyncProxyQPSLimit(config) {
		return nil, nil
	}
	var tokens []string
	for token := range ctx.proxy {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)

	var failed []string
	for _, token := range tokens {
		p := ctx.proxy[token]
		if p == nil {
			continue
		}
		if err := s.newProxyClient(p).SetQPSLimit(config.Revision, config.Limit); err != nil {
			log.ErrorErrorf(err, "proxy-[%s] sync qps limit failed", p.Token)
			failed = append(failed, p.Token)
		}
	}
	if len(failed) != 0 {
		return failed, errors.Errorf("proxy qps limit sync failed: %s", strings.Join(failed, ","))
	}
	return nil, nil
}
