// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"reflect"
	"strings"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestSetACLInstallsSnapshot(x *testing.T) {
	s, addr := openProxy(x)
	defer s.Close()

	c := NewApiClient(addr)
	c.SetXAuth(config.ProductName, config.ProductAuth, s.Model().Token)

	acl := &models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{"hash"},
			Rules:          []string{"+@read"},
		}},
	}
	assert.MustNoError(x, c.SetACL(acl))

	snapshot := s.ACLSnapshot()
	assert.Must(x, snapshot != nil)
	assert.Must(x, snapshot.Revision == 1)
	assert.Must(x, snapshot.Enabled)
	assert.Must(x, snapshot.Users["app_ro"] != nil)
	assert.Must(x, snapshot.Users["app_ro"].Rules[0] == "+@read")
}

func TestACLAuthNamedUserAndWhoami(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(x, config)
	defer s.Close()

	assert.MustNoError(x, s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@read", "~app:*"},
		}},
	}))

	c := dialProxy(x, addr)
	defer c.Close()

	resp := proxyCall(x, c, "AUTH", "app_ro", "secret")
	assert.Must(x, resp.IsString() && string(resp.Value) == "OK")

	resp = proxyCall(x, c, "ACL", "WHOAMI")
	assert.Must(x, resp.IsBulkBytes() && string(resp.Value) == "app_ro")
}

func TestACLAuthFailureDoesNotInstallIdentity(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(x, config)
	defer s.Close()

	assert.MustNoError(x, s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@read", "~app:*"},
		}},
	}))

	c := dialProxy(x, addr)
	defer c.Close()

	resp := proxyCall(x, c, "AUTH", "app_ro", "wrong")
	assert.Must(x, resp.IsError())
	assert.Must(x, string(resp.Value) == "WRONGPASS invalid username-password pair or user is disabled")

	resp = proxyCall(x, c, "ACL", "WHOAMI")
	assert.Must(x, resp.IsError())
	assert.Must(x, string(resp.Value) == "NOAUTH Authentication required")
}

func TestACLReAuthFailureKeepsExistingIdentity(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(x, config)
	defer s.Close()

	aclInstallAppRO(x, s)

	c := dialProxy(x, addr)
	defer c.Close()

	resp := proxyCall(x, c, "AUTH", "app_ro", "secret")
	assert.Must(x, resp.IsString() && string(resp.Value) == "OK")

	resp = proxyCall(x, c, "AUTH", "app_ro", "wrong")
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.HasPrefix(string(resp.Value), "WRONGPASS"))

	resp = proxyCall(x, c, "ACL", "WHOAMI")
	assert.Must(x, resp.IsBulkBytes() && string(resp.Value) == "app_ro")
}

func TestACLAuthDefaultUserPasswordForm(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(x, config)
	defer s.Close()

	assert.MustNoError(x, s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "default",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}))

	c := dialProxy(x, addr)
	defer c.Close()

	resp := proxyCall(x, c, "AUTH", "secret")
	assert.Must(x, resp.IsString() && string(resp.Value) == "OK")

	resp = proxyCall(x, c, "ACL", "WHOAMI")
	assert.Must(x, resp.IsBulkBytes() && string(resp.Value) == "default")
}

func TestACLRevisionSwitchStalesSession(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(x, config)
	defer s.Close()

	acl := &models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@read", "~app:*"},
		}},
	}
	assert.MustNoError(x, s.SetACL(acl))

	c := dialProxy(x, addr)
	defer c.Close()

	resp := proxyCall(x, c, "AUTH", "app_ro", "secret")
	assert.Must(x, resp.IsString() && string(resp.Value) == "OK")

	acl.Revision = 2
	assert.MustNoError(x, s.SetACL(acl))

	resp = proxyCall(x, c, "ACL", "WHOAMI")
	assert.Must(x, resp.IsError())
	assert.Must(x, string(resp.Value) == "NOAUTH Authentication required")

	resp = proxyCall(x, c, "AUTH", "app_ro", "secret")
	assert.Must(x, resp.IsString() && string(resp.Value) == "OK")
	resp = proxyCall(x, c, "ACL", "WHOAMI")
	assert.Must(x, resp.IsBulkBytes() && string(resp.Value) == "app_ro")
}

func TestACLDisableClearsAuthorizedSession(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true
	config.SessionAuth = "legacy-secret"

	s, addr := openStartedProxy(x, config)
	defer s.Close()

	aclInstallAppRO(x, s)

	c := dialProxy(x, addr)
	defer c.Close()

	resp := proxyCall(x, c, "AUTH", "app_ro", "secret")
	assert.Must(x, resp.IsString() && string(resp.Value) == "OK")

	assert.MustNoError(x, s.SetACL(&models.ACL{
		Revision: 2,
		Enabled:  false,
	}))

	resp = proxyCall(x, c, "CLIENT", "LIST")
	assert.Must(x, resp.IsError())
	assert.Must(x, string(resp.Value) == "NOAUTH Authentication required")

	resp = proxyCall(x, c, "AUTH", "legacy-secret")
	assert.Must(x, resp.IsString() && string(resp.Value) == "OK")
}

func TestACLReAuthFailureTriggersBruteforceLock(x *testing.T) {
	config := newAuthBruteforceTestConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(x, config)
	defer s.Close()

	aclInstallAppRO(x, s)

	c := dialProxy(x, addr)
	defer c.Close()

	resp := proxyCall(x, c, "AUTH", "app_ro", "secret")
	assert.Must(x, resp.IsString() && string(resp.Value) == "OK")

	for i := 0; i < config.SessionAuthBruteforceMaxFailures; i++ {
		resp = proxyCall(x, c, "AUTH", "app_ro", "wrong")
		assert.Must(x, resp.IsError())
		assert.Must(x, strings.HasPrefix(string(resp.Value), "WRONGPASS"))
	}

	resp = proxyCall(x, c, "AUTH", "app_ro", "secret")
	assert.Must(x, resp.IsError())
	assert.Must(x, string(resp.Value) == authBruteforceLockedMessage)

	stats := s.Stats(0).SessionAuthBruteforce
	assert.Must(x, stats != nil)
	assert.Must(x, stats.Failures == int64(config.SessionAuthBruteforceMaxFailures))
	assert.Must(x, stats.Locks == 1)
}

func TestACLCommandNeverFallsThroughToBackend(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	backend := redistest.NewServer(x, func(args []string) *redistest.Resp {
		if strings.EqualFold(args[0], "ACL") {
			x.Fatalf("client ACL command reached backend: %v", args)
		}
		if strings.EqualFold(args[0], "AUTH") {
			return redistest.OK()
		}
		return redistest.OK()
	})

	s, addr := openStartedProxy(x, config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(x, addr)
	defer c.Close()
	assert.Must(x, proxyCall(x, c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(x, c, "ACL", "SETUSER", "evil", "on", "nopass", "+@all")
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.Contains(string(resp.Value), "unsupported ACL subcommand"))

	resp = proxyCall(x, c, "ACL", "DRYRUN", "app_ro", "GET", "app:k")
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.Contains(string(resp.Value), "unsupported ACL subcommand"))
}

func TestACLCommandRejectedWhenCodisACLDisabled(x *testing.T) {
	config := newProxyConfig()

	s, addr := openStartedProxy(x, config)
	defer s.Close()

	c := dialProxy(x, addr)
	defer c.Close()

	resp := proxyCall(x, c, "ACL", "SETUSER", "evil", "on", "nopass", "+@all")
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.Contains(string(resp.Value), "unsupported ACL subcommand"))
}

func TestACLStableCommandUsesUserBoundBackend(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	var backend *redistest.Server
	backend = redistest.NewServer(x, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH":
			if len(args) != 3 || args[1] != "app_ro" || args[2] != "secret" {
				x.Fatalf("unexpected auth args: %v", args)
			}
			return redistest.OK()
		case "SELECT":
			return redistest.OK()
		case "GET":
			return redistest.Bulk("value")
		case "SET":
			return redistest.Error("NOPERM this user has no permissions to run the 'set' command")
		default:
			return redistest.Error("ERR unexpected")
		}
	})

	s, addr := openStartedProxy(x, config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(x, addr)
	defer c.Close()
	assert.Must(x, proxyCall(x, c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(x, c, "GET", "app:k")
	assert.Must(x, resp.IsBulkBytes() && string(resp.Value) == "value")

	resp = proxyCall(x, c, "SET", "app:k", "v")
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.HasPrefix(string(resp.Value), "NOPERM"))

	assert.Must(x, commandExists(backend.Commands(), []string{"AUTH", "app_ro", "secret"}))
}

func TestACLLocalCommandDryRunDenied(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	backend := redistest.NewServer(x, func(args []string) *redistest.Resp {
		if strings.EqualFold(args[0], "ACL") {
			return redistest.Error("NOPERM this user has no permissions to run the 'client|list' command")
		}
		if strings.EqualFold(args[0], "SELECT") {
			return redistest.OK()
		}
		return redistest.OK()
	})

	s, addr := openStartedProxy(x, config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(x, addr)
	defer c.Close()
	assert.Must(x, proxyCall(x, c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(x, c, "CLIENT", "LIST")
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.HasPrefix(string(resp.Value), "NOPERM"))
	assert.Must(x, commandExists(backend.Commands(), []string{"ACL", "DRYRUN", "app_ro", "CLIENT", "LIST"}))
}

func TestACLSelectDryRunDenied(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	backend := redistest.NewServer(x, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "ACL":
			return redistest.Error("NOPERM this user has no permissions to run the 'select' command")
		default:
			return redistest.OK()
		}
	})

	s, addr := openStartedProxy(x, config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(x, addr)
	defer c.Close()
	assert.Must(x, proxyCall(x, c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(x, c, "SELECT", "1")
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.HasPrefix(string(resp.Value), "NOPERM"))
	assert.Must(x, commandExists(backend.Commands(), []string{"ACL", "DRYRUN", "app_ro", "SELECT", "1"}))
}

func TestACLAddrSpecificCommandUsesUserBoundBackend(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	backend := redistest.NewServer(x, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH":
			if len(args) != 3 || args[1] != "app_ro" || args[2] != "secret" {
				x.Fatalf("unexpected auth args: %v", args)
			}
			return redistest.OK()
		case "INFO":
			return redistest.Error("NOPERM this user has no permissions to run the 'info' command")
		case "PING":
			return redistest.Error("NOPERM this user has no permissions to run the 'ping' command")
		case "SLOTSINFO":
			return redistest.Error("NOPERM this user has no permissions to run the 'slotsinfo' command")
		default:
			return redistest.OK()
		}
	})

	s, addr := openStartedProxy(x, config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(x, addr)
	defer c.Close()
	assert.Must(x, proxyCall(x, c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(x, c, "INFO", backend.Addr())
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.Contains(string(resp.Value), "NOPERM"))

	resp = proxyCall(x, c, "PING", backend.Addr())
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.Contains(string(resp.Value), "NOPERM"))

	resp = proxyCall(x, c, "SLOTSINFO", backend.Addr())
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.Contains(string(resp.Value), "NOPERM"))

	assert.Must(x, commandExists(backend.Commands(), []string{"AUTH", "app_ro", "secret"}))
	assert.Must(x, commandExists(backend.Commands(), []string{"INFO"}))
	assert.Must(x, commandExists(backend.Commands(), []string{"PING"}))
	assert.Must(x, commandExists(backend.Commands(), []string{"SLOTSINFO"}))
}

func TestACLHotKeyCacheHitDryRunDenied(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true
	config.HotKeyCacheEnabled = true
	config.HotKeyCacheKeys = []string{"hot:key"}

	var dryrunDeny bool
	backend := redistest.NewServer(x, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH":
			return redistest.OK()
		case "SELECT":
			return redistest.OK()
		case "ACL":
			if dryrunDeny {
				return redistest.Error("NOPERM this user has no permissions to access one of the keys")
			}
			return redistest.OK()
		case "GET":
			return redistest.Bulk("cached")
		default:
			return redistest.Error("ERR unexpected")
		}
	})

	s, addr := openStartedProxy(x, config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("hot:key"), backend.Addr())

	c := dialProxy(x, addr)
	defer c.Close()
	assert.Must(x, proxyCall(x, c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(x, c, "GET", "hot:key")
	assert.Must(x, resp.IsBulkBytes() && string(resp.Value) == "cached")

	dryrunDeny = true
	resp = proxyCall(x, c, "GET", "hot:key")
	assert.Must(x, resp.IsError())
	assert.Must(x, strings.HasPrefix(string(resp.Value), "NOPERM"))
	assert.Must(x, commandExists(backend.Commands(), []string{"ACL", "DRYRUN", "app_ro", "GET", "hot:key"}))
}

func aclInstallAppRO(t testing.TB, s *Proxy) {
	t.Helper()
	assert.MustNoError(t, s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@read", "~app:*", "~hot:*"},
		}},
	}))
}

func fillProxySlotForKey(t testing.TB, s *Proxy, key []byte, addr string) {
	t.Helper()
	slot := int(Hash(key) % MaxSlotNum)
	assert.MustNoError(t, s.FillSlot(&models.Slot{
		Id:                 slot,
		BackendAddr:        addr,
		BackendAddrGroupId: 1,
		ForwardMethod:      models.ForwardSync,
	}))
}

func commandExists(commands [][]string, want []string) bool {
	for _, cmd := range commands {
		if reflect.DeepEqual(cmd, want) {
			return true
		}
	}
	return false
}
