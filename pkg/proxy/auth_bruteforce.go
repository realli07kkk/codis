// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/log"
)

const (
	authBruteforceLockedMessage = "ERR too many invalid AUTH attempts, try again later"
	authBruteforceMaxTrackedIPs = 4096
)

type AuthBruteforceStats struct {
	Enabled    bool  `json:"enabled"`
	TrackedIPs int   `json:"tracked_ips"`
	LockedIPs  int   `json:"locked_ips"`
	Failures   int64 `json:"failures"`
	Locks      int64 `json:"locks"`
	Unlocks    int64 `json:"unlocks"`
}

func (s AuthBruteforceStats) Visible() bool {
	return s.Enabled || s.TrackedIPs != 0 || s.LockedIPs != 0 ||
		s.Failures != 0 || s.Locks != 0 || s.Unlocks != 0
}

type authFailureRecord struct {
	failures      int
	lastFailureAt time.Time
	lockedUntil   time.Time
}

type AuthBruteforceGuard struct {
	mu sync.Mutex

	enabled       bool
	maxFailures   int
	lockDuration  time.Duration
	maxTrackedIPs int

	records map[string]*authFailureRecord
	stats   AuthBruteforceStats
}

func newAuthBruteforceGuard(config *Config) *AuthBruteforceGuard {
	g := &AuthBruteforceGuard{
		enabled:       config.SessionAuthBruteforceEnabled,
		maxFailures:   config.SessionAuthBruteforceMaxFailures,
		lockDuration:  config.SessionAuthBruteforceLockDuration.Duration(),
		maxTrackedIPs: authBruteforceMaxTrackedIPs,
		records:       make(map[string]*authFailureRecord),
	}
	g.stats.Enabled = g.enabled
	return g
}

func authBruteforceClientIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = strings.Trim(remoteAddr, "[]")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	return ip.String()
}

func (g *AuthBruteforceGuard) active() bool {
	return g != nil && g.enabled && g.maxFailures > 0 && g.lockDuration > 0
}

func (g *AuthBruteforceGuard) BeforeAuth(remoteAddr string, authorized bool, now time.Time) bool {
	if !g.active() || authorized {
		return false
	}
	ip := authBruteforceClientIP(remoteAddr)
	if ip == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	record := g.records[ip]
	if record == nil {
		return false
	}
	if record.isLocked(now) {
		return true
	}
	if g.expireRecordLocked(ip, record, now) {
		return false
	}
	return false
}

func (g *AuthBruteforceGuard) RecordAuthFailure(remoteAddr string, authorized bool, now time.Time) bool {
	if !g.active() || authorized {
		return false
	}
	ip := authBruteforceClientIP(remoteAddr)
	if ip == "" {
		log.Warnf("session auth brute-force skip untrackable remote addr=%q", remoteAddr)
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	record := g.records[ip]
	if record != nil && record.isLocked(now) {
		return true
	}
	if record == nil || g.expireRecordLocked(ip, record, now) {
		if !g.ensureCapacityLocked(now) {
			return false
		}
		record = &authFailureRecord{}
		g.records[ip] = record
	}
	if !record.lastFailureAt.IsZero() && now.Sub(record.lastFailureAt) >= g.lockDuration {
		record.failures = 0
	}

	record.failures++
	record.lastFailureAt = now
	g.stats.Failures++
	if record.failures >= g.maxFailures {
		record.lockedUntil = now.Add(g.lockDuration)
		g.stats.Locks++
		log.Warnf("session auth brute-force locked ip=%s failures=%d until=%s", ip, record.failures, record.lockedUntil.Format(time.RFC3339))
		return true
	}
	return false
}

func (g *AuthBruteforceGuard) RecordAuthSuccess(remoteAddr string) {
	if !g.active() {
		return
	}
	ip := authBruteforceClientIP(remoteAddr)
	if ip == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.records, ip)
}

func (g *AuthBruteforceGuard) Stats() AuthBruteforceStats {
	if g == nil {
		return AuthBruteforceStats{}
	}
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()

	for ip, record := range g.records {
		g.expireRecordLocked(ip, record, now)
	}
	stats := g.stats
	stats.TrackedIPs = len(g.records)
	for _, record := range g.records {
		if record.isLocked(now) {
			stats.LockedIPs++
		}
	}
	return stats
}

func (g *AuthBruteforceGuard) ensureCapacityLocked(now time.Time) bool {
	g.cleanupExpiredLocked(now)
	if len(g.records) < g.maxTrackedIPs {
		return true
	}
	var (
		evictIP string
		evictAt time.Time
	)
	for ip, record := range g.records {
		if record.isLocked(now) {
			continue
		}
		if evictIP == "" || record.lastFailureAt.Before(evictAt) {
			evictIP = ip
			evictAt = record.lastFailureAt
		}
	}
	if evictIP == "" {
		return false
	}
	delete(g.records, evictIP)
	return true
}

func (g *AuthBruteforceGuard) cleanupExpiredLocked(now time.Time) {
	for ip, record := range g.records {
		g.expireRecordLocked(ip, record, now)
	}
}

func (r *authFailureRecord) isLocked(now time.Time) bool {
	return !r.lockedUntil.IsZero() && now.Before(r.lockedUntil)
}

func (g *AuthBruteforceGuard) expireRecordLocked(ip string, record *authFailureRecord, now time.Time) bool {
	if record == nil {
		return false
	}
	if !record.lockedUntil.IsZero() {
		if now.Before(record.lockedUntil) {
			return false
		}
		delete(g.records, ip)
		g.stats.Unlocks++
		log.Warnf("session auth brute-force unlocked ip=%s", ip)
		return true
	}
	if !record.lastFailureAt.IsZero() && now.Sub(record.lastFailureAt) >= g.lockDuration {
		delete(g.records, ip)
		return true
	}
	return false
}
