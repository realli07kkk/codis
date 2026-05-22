// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func newAuthBruteforceTestConfig() *Config {
	config := newProxyConfig()
	config.SessionAuth = "secret"
	config.SessionAuthBruteforceEnabled = true
	config.SessionAuthBruteforceMaxFailures = 3
	config.SessionAuthBruteforceLockDuration.Set(time.Millisecond * 50)
	return config
}

func TestAuthBruteforceClientIP(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:12345": "127.0.0.1",
		"[::1]:12345":     "::1",
		"not-an-ip":       "",
		"":                "",
	}
	for input, expect := range cases {
		if got := authBruteforceClientIP(input); got != expect {
			t.Fatalf("client ip for %q = %q, want %q", input, got, expect)
		}
	}
}

func TestAuthBruteforceConfigValidation(t *testing.T) {
	config := newProxyConfig()
	config.SessionAuthBruteforceEnabled = true
	config.SessionAuthBruteforceMaxFailures = 0
	if err := config.Validate(); err == nil {
		t.Fatalf("expected invalid max failures")
	}

	config = newProxyConfig()
	config.SessionAuthBruteforceEnabled = true
	config.SessionAuthBruteforceMaxFailures = 3
	config.SessionAuthBruteforceLockDuration = 0
	if err := config.Validate(); err == nil {
		t.Fatalf("expected invalid lock duration")
	}

	config = newProxyConfig()
	config.SessionAuthBruteforceEnabled = true
	config.SessionAuthBruteforceMaxFailures = 3
	config.SessionAuthBruteforceLockDuration.Set(time.Second)
	if err := config.Validate(); err != nil {
		t.Fatalf("enabled guard config validate failed: %v", err)
	}
}

func TestAuthBruteforceGuardLockUnlockAndSuccessReset(t *testing.T) {
	config := newAuthBruteforceTestConfig()
	guard := newAuthBruteforceGuard(config)
	now := time.Now()
	ip := "127.0.0.1"

	for i := 0; i < config.SessionAuthBruteforceMaxFailures-1; i++ {
		if locked := guard.RecordAuthFailure(ip, false, now.Add(time.Duration(i)*time.Millisecond)); locked {
			t.Fatalf("failure %d locked too early", i+1)
		}
	}
	if locked := guard.RecordAuthFailure(ip, false, now.Add(time.Millisecond*3)); !locked {
		t.Fatalf("expected lock at threshold")
	}
	if !guard.BeforeAuth(ip, false, now.Add(time.Millisecond*4)) {
		t.Fatalf("expected locked ip before lock duration")
	}
	if guard.BeforeAuth("127.0.0.2", false, now.Add(time.Millisecond*4)) {
		t.Fatalf("different ip should not be locked")
	}

	stats := guard.Stats()
	if stats.Failures != 3 || stats.Locks != 1 || stats.TrackedIPs != 1 || stats.LockedIPs != 1 {
		t.Fatalf("stats after lock = %+v", stats)
	}

	if guard.BeforeAuth(ip, false, now.Add(config.SessionAuthBruteforceLockDuration.Duration()+time.Millisecond*10)) {
		t.Fatalf("expected automatic unlock after lock duration")
	}
	stats = guard.Stats()
	if stats.Unlocks != 1 || stats.TrackedIPs != 0 || stats.LockedIPs != 0 {
		t.Fatalf("stats after unlock = %+v", stats)
	}

	if locked := guard.RecordAuthFailure(ip, false, now); locked {
		t.Fatalf("first failure after unlock locked")
	}
	guard.RecordAuthSuccess(ip)
	stats = guard.Stats()
	if stats.TrackedIPs != 0 {
		t.Fatalf("success should clear tracked ip, stats = %+v", stats)
	}
}

func TestAuthBruteforceGuardConcurrentRecords(t *testing.T) {
	config := newAuthBruteforceTestConfig()
	config.SessionAuthBruteforceMaxFailures = 1000
	guard := newAuthBruteforceGuard(config)
	now := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				guard.RecordAuthFailure("127.0.0.1", false, now.Add(time.Duration(j)*time.Millisecond))
				_ = guard.BeforeAuth("127.0.0.1", false, now.Add(time.Duration(j)*time.Millisecond))
			}
		}()
	}
	wg.Wait()

	stats := guard.Stats()
	if stats.Failures == 0 || stats.TrackedIPs != 1 {
		t.Fatalf("concurrent stats = %+v", stats)
	}
}

func TestAuthBruteforceGuardSkipsEmptyIP(t *testing.T) {
	config := newAuthBruteforceTestConfig()
	guard := newAuthBruteforceGuard(config)
	if locked := guard.RecordAuthFailure("", false, time.Now()); locked {
		t.Fatalf("empty ip should not lock")
	}
	stats := guard.Stats()
	if stats.Failures != 0 || stats.TrackedIPs != 0 {
		t.Fatalf("empty ip stats = %+v", stats)
	}
}

func TestAuthBruteforceGuardBoundsTrackedIPs(t *testing.T) {
	config := newAuthBruteforceTestConfig()
	guard := newAuthBruteforceGuard(config)
	guard.maxTrackedIPs = 3
	now := time.Now()

	for i := 0; i < 10; i++ {
		guard.RecordAuthFailure(fmt.Sprintf("10.0.0.%d:1000", i), false, now.Add(time.Duration(i)*time.Millisecond))
	}
	stats := guard.Stats()
	if stats.TrackedIPs > guard.maxTrackedIPs {
		t.Fatalf("tracked ips = %d, max = %d", stats.TrackedIPs, guard.maxTrackedIPs)
	}
}

func TestAuthBruteforceGuardCleansExpiredBeforeInsert(t *testing.T) {
	config := newAuthBruteforceTestConfig()
	guard := newAuthBruteforceGuard(config)
	guard.maxTrackedIPs = 2
	now := time.Now()

	guard.RecordAuthFailure("10.0.0.1:1000", false, now)
	guard.RecordAuthFailure("10.0.0.2:1000", false, now)
	guard.RecordAuthFailure("10.0.0.3:1000", false, now.Add(config.SessionAuthBruteforceLockDuration.Duration()+time.Millisecond))

	stats := guard.Stats()
	if stats.TrackedIPs != 1 {
		t.Fatalf("tracked ips after insert cleanup = %d, want 1", stats.TrackedIPs)
	}
}

func TestSessionAuthBruteforceDefaultDisabledKeepsAuthErrors(t *testing.T) {
	config := newProxyConfig()
	config.SessionAuth = "secret"
	config.SessionAuthBruteforceEnabled = false

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	for i := 0; i < 4; i++ {
		resp := proxyCall(c, "AUTH", "wrong")
		if !resp.IsError() || string(resp.Value) != "ERR invalid password" {
			t.Fatalf("wrong auth resp = %s %q", resp.Type, resp.Value)
		}
	}
	resp := proxyCall(c, "AUTH", "secret")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("correct auth resp = %s %q", resp.Type, resp.Value)
	}
}

func TestSessionAuthBruteforceWrongArityDoesNotRecordFailure(t *testing.T) {
	config := newAuthBruteforceTestConfig()

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH")
	if !resp.IsError() || string(resp.Value) != "ERR wrong number of arguments for 'AUTH' command" {
		t.Fatalf("wrong arity auth resp = %s %q", resp.Type, resp.Value)
	}
	stats := s.Stats(0).SessionAuthBruteforce
	if stats == nil || stats.Failures != 0 || stats.TrackedIPs != 0 {
		t.Fatalf("wrong arity stats = %+v", stats)
	}
}

func TestSessionAuthBruteforceLocksAndUnlocks(t *testing.T) {
	config := newAuthBruteforceTestConfig()

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	for i := 0; i < config.SessionAuthBruteforceMaxFailures; i++ {
		resp := proxyCall(c, "AUTH", "wrong")
		if !resp.IsError() || string(resp.Value) != "ERR invalid password" {
			t.Fatalf("wrong auth %d resp = %s %q", i+1, resp.Type, resp.Value)
		}
	}
	resp := proxyCall(c, "AUTH", "secret")
	if !resp.IsError() || string(resp.Value) != authBruteforceLockedMessage {
		t.Fatalf("locked auth resp = %s %q", resp.Type, resp.Value)
	}

	assertEventually(func() bool {
		resp = proxyCall(c, "AUTH", "secret")
		return resp.IsString() && string(resp.Value) == "OK"
	})
}

func TestSessionAuthBruteforceSuccessClearsFailureCount(t *testing.T) {
	config := newAuthBruteforceTestConfig()

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	for i := 0; i < config.SessionAuthBruteforceMaxFailures-1; i++ {
		resp := proxyCall(c, "AUTH", "wrong")
		if !resp.IsError() || string(resp.Value) != "ERR invalid password" {
			t.Fatalf("wrong auth before success resp = %s %q", resp.Type, resp.Value)
		}
	}
	resp := proxyCall(c, "AUTH", "secret")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("auth success resp = %s %q", resp.Type, resp.Value)
	}

	for i := 0; i < config.SessionAuthBruteforceMaxFailures-1; i++ {
		resp := proxyCall(c, "AUTH", "wrong")
		if !resp.IsError() || string(resp.Value) != "ERR invalid password" {
			t.Fatalf("wrong auth after success resp = %s %q", resp.Type, resp.Value)
		}
	}
	resp = proxyCall(c, "AUTH", "secret")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("auth after reset resp = %s %q", resp.Type, resp.Value)
	}
}

func TestSessionAuthBruteforceNoSessionAuthNoFailureRecord(t *testing.T) {
	config := newAuthBruteforceTestConfig()
	config.SessionAuth = ""

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "anything")
	if !resp.IsError() || string(resp.Value) != "ERR Client sent AUTH, but no password is set" {
		t.Fatalf("auth without session_auth resp = %s %q", resp.Type, resp.Value)
	}
	stats := s.Stats(0).SessionAuthBruteforce
	if stats == nil || !stats.Enabled || stats.Failures != 0 || stats.TrackedIPs != 0 {
		t.Fatalf("session auth brute-force stats = %+v", stats)
	}
}

func TestProxyAuthBruteforceAuthorizedSessionUnaffected(t *testing.T) {
	config := newAuthBruteforceTestConfig()

	s, addr := openStartedProxy(config)
	defer s.Close()

	authorized := dialProxy(addr)
	defer authorized.Close()
	resp := proxyCall(authorized, "AUTH", "secret")
	if !resp.IsString() || string(resp.Value) != "OK" {
		t.Fatalf("initial auth resp = %s %q", resp.Type, resp.Value)
	}

	attacker := dialProxy(addr)
	defer attacker.Close()
	for i := 0; i < config.SessionAuthBruteforceMaxFailures; i++ {
		resp = proxyCall(attacker, "AUTH", "wrong")
		if !resp.IsError() || string(resp.Value) != "ERR invalid password" {
			t.Fatalf("wrong auth resp = %s %q", resp.Type, resp.Value)
		}
	}

	resp = proxyCall(authorized, "CLIENT", "LIST")
	if !resp.IsBulkBytes() || len(parseClientList(resp.Value)) == 0 {
		t.Fatalf("authorized CLIENT LIST resp = %s %q", resp.Type, resp.Value)
	}
}

func TestProxyAuthBruteforceStats(t *testing.T) {
	config := newAuthBruteforceTestConfig()

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	for i := 0; i < config.SessionAuthBruteforceMaxFailures; i++ {
		_ = proxyCall(c, "AUTH", "wrong")
	}
	stats := s.Stats(0).SessionAuthBruteforce
	if stats == nil || !stats.Enabled || stats.Failures != 3 || stats.Locks != 1 || stats.LockedIPs != 1 || stats.TrackedIPs != 1 {
		t.Fatalf("session auth brute-force stats = %+v", stats)
	}
}
