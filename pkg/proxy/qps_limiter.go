// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"sync"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/sync2/atomic2"
)

const qpsLimitExceededMessage = "ERR proxy qps limit exceeded"

type QPSLimitStats struct {
	Revision int64 `json:"revision"`
	Limit    int64 `json:"limit"`
	Enabled  bool  `json:"enabled"`
	Accepted int64 `json:"accepted"`
	Rejected int64 `json:"rejected"`
}

type QPSLimitConfig struct {
	Revision int64 `json:"revision"`
	Limit    int64 `json:"limit"`
}

func (s QPSLimitStats) Visible() bool {
	return s.Enabled || s.Revision != 0 || s.Rejected != 0
}

type QPSLimiter struct {
	mu sync.Mutex

	revision atomic2.Int64
	limit    atomic2.Int64
	accepted atomic2.Int64
	rejected atomic2.Int64

	tokens     float64
	lastRefill time.Time
}

func newQPSLimiter(limit int64) *QPSLimiter {
	l := &QPSLimiter{}
	if err := l.SetLimit(0, limit, time.Now()); err != nil {
		panic(err)
	}
	return l
}

func (l *QPSLimiter) Allow(now time.Time) bool {
	if l == nil {
		return true
	}
	limit := l.limit.Int64()
	if limit <= 0 {
		return true
	}
	if now.IsZero() {
		now = time.Now()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	limit = l.limit.Int64()
	if limit <= 0 {
		return true
	}
	l.refillLocked(now, limit)
	if l.tokens >= 1 {
		l.tokens--
		l.accepted.Incr()
		return true
	}
	l.rejected.Incr()
	return false
}

func (l *QPSLimiter) SetLimit(revision, limit int64, now time.Time) error {
	if limit < 0 {
		return errors.New("invalid proxy_qps_limit")
	}
	if now.IsZero() {
		now = time.Now()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	oldLimit := l.limit.Int64()
	l.revision.Set(revision)
	l.limit.Set(limit)
	switch {
	case limit <= 0:
		l.tokens = 0
	case oldLimit <= 0:
		l.tokens = float64(limit)
	case l.tokens > float64(limit):
		l.tokens = float64(limit)
	}
	l.lastRefill = now
	return nil
}

func (l *QPSLimiter) Stats() QPSLimitStats {
	if l == nil {
		return QPSLimitStats{}
	}
	limit := l.limit.Int64()
	return QPSLimitStats{
		Revision: l.revision.Int64(),
		Limit:    limit,
		Enabled:  limit > 0,
		Accepted: l.accepted.Int64(),
		Rejected: l.rejected.Int64(),
	}
}

func (l *QPSLimiter) ResetStats() {
	if l == nil {
		return
	}
	l.accepted.Set(0)
	l.rejected.Set(0)
}

func (l *QPSLimiter) refillLocked(now time.Time, limit int64) {
	if l.lastRefill.IsZero() {
		l.lastRefill = now
		l.tokens = float64(limit)
		return
	}
	if now.Before(l.lastRefill) {
		return
	}
	elapsed := now.Sub(l.lastRefill).Seconds()
	if elapsed <= 0 {
		return
	}
	l.tokens += elapsed * float64(limit)
	capacity := float64(limit)
	if l.tokens > capacity {
		l.tokens = capacity
	}
	l.lastRefill = now
}
