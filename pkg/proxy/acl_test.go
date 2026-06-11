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
	s, addr := openProxy()
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
	assert.MustNoError(c.SetACL(acl))

	snapshot := s.ACLSnapshot()
	assert.Must(snapshot != nil)
	assert.Must(snapshot.Revision == 1)
	assert.Must(snapshot.Enabled)
	assert.Must(snapshot.Users["app_ro"] != nil)
	assert.Must(snapshot.Users["app_ro"].Rules[0] == "+@read")
}

func TestACLAuthNamedUserAndWhoami(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(config)
	defer s.Close()

	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@read", "~app:*"},
		}},
	}))

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "app_ro", "secret")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	resp = proxyCall(c, "ACL", "WHOAMI")
	assert.Must(resp.IsBulkBytes() && string(resp.Value) == "app_ro")
}

func TestACLAuthFailureDoesNotInstallIdentity(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(config)
	defer s.Close()

	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@read", "~app:*"},
		}},
	}))

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "app_ro", "wrong")
	assert.Must(resp.IsError())
	assert.Must(string(resp.Value) == "WRONGPASS invalid username-password pair or user is disabled")

	resp = proxyCall(c, "ACL", "WHOAMI")
	assert.Must(resp.IsError())
	assert.Must(string(resp.Value) == "NOAUTH Authentication required")
}

func TestACLReAuthFailureKeepsExistingIdentity(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(config)
	defer s.Close()

	aclInstallAppRO(x, s)

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "app_ro", "secret")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	resp = proxyCall(c, "AUTH", "app_ro", "wrong")
	assert.Must(resp.IsError())
	assert.Must(strings.HasPrefix(string(resp.Value), "WRONGPASS"))

	resp = proxyCall(c, "ACL", "WHOAMI")
	assert.Must(resp.IsBulkBytes() && string(resp.Value) == "app_ro")
}

func TestACLAuthDefaultUserPasswordForm(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(config)
	defer s.Close()

	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "default",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}))

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "secret")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	resp = proxyCall(c, "ACL", "WHOAMI")
	assert.Must(resp.IsBulkBytes() && string(resp.Value) == "default")
}

func TestACLRevisionSwitchStalesSession(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(config)
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
	assert.MustNoError(s.SetACL(acl))

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "app_ro", "secret")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	acl.Revision = 2
	assert.MustNoError(s.SetACL(acl))

	resp = proxyCall(c, "ACL", "WHOAMI")
	assert.Must(resp.IsError())
	assert.Must(string(resp.Value) == "NOAUTH Authentication required")

	resp = proxyCall(c, "AUTH", "app_ro", "secret")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")
	resp = proxyCall(c, "ACL", "WHOAMI")
	assert.Must(resp.IsBulkBytes() && string(resp.Value) == "app_ro")
}

func TestACLDisableClearsAuthorizedSession(x *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true
	config.SessionAuth = "legacy-secret"

	s, addr := openStartedProxy(config)
	defer s.Close()

	aclInstallAppRO(x, s)

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "app_ro", "secret")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 2,
		Enabled:  false,
	}))

	resp = proxyCall(c, "CLIENT", "LIST")
	assert.Must(resp.IsError())
	assert.Must(string(resp.Value) == "NOAUTH Authentication required")

	resp = proxyCall(c, "AUTH", "legacy-secret")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")
}

func TestACLReAuthFailureTriggersBruteforceLock(x *testing.T) {
	config := newAuthBruteforceTestConfig()
	config.CodisACLEnabled = true

	s, addr := openStartedProxy(config)
	defer s.Close()

	aclInstallAppRO(x, s)

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "app_ro", "secret")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	for i := 0; i < config.SessionAuthBruteforceMaxFailures; i++ {
		resp = proxyCall(c, "AUTH", "app_ro", "wrong")
		assert.Must(resp.IsError())
		assert.Must(strings.HasPrefix(string(resp.Value), "WRONGPASS"))
	}

	resp = proxyCall(c, "AUTH", "app_ro", "secret")
	assert.Must(resp.IsError())
	assert.Must(string(resp.Value) == authBruteforceLockedMessage)

	stats := s.Stats(0).SessionAuthBruteforce
	assert.Must(stats != nil)
	assert.Must(stats.Failures == int64(config.SessionAuthBruteforceMaxFailures))
	assert.Must(stats.Locks == 1)
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

	s, addr := openStartedProxy(config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(c, "ACL", "SETUSER", "evil", "on", "nopass", "+@all")
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "unsupported ACL subcommand"))

	resp = proxyCall(c, "ACL", "DRYRUN", "app_ro", "GET", "app:k")
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "unsupported ACL subcommand"))
}

func TestACLCommandRejectedWhenCodisACLDisabled(x *testing.T) {
	config := newProxyConfig()

	s, addr := openStartedProxy(config)
	defer s.Close()

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "ACL", "SETUSER", "evil", "on", "nopass", "+@all")
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "unsupported ACL subcommand"))
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

	s, addr := openStartedProxy(config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(c, "GET", "app:k")
	assert.Must(resp.IsBulkBytes() && string(resp.Value) == "value")

	resp = proxyCall(c, "SET", "app:k", "v")
	assert.Must(resp.IsError())
	assert.Must(strings.HasPrefix(string(resp.Value), "NOPERM"))

	assert.Must(commandExists(backend.Commands(), []string{"AUTH", "app_ro", "secret"}))
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

	s, addr := openStartedProxy(config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(c, "CLIENT", "LIST")
	assert.Must(resp.IsError())
	assert.Must(strings.HasPrefix(string(resp.Value), "NOPERM"))
	assert.Must(commandExists(backend.Commands(), []string{"ACL", "DRYRUN", "app_ro", "CLIENT", "LIST"}))
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

	s, addr := openStartedProxy(config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(c, "SELECT", "1")
	assert.Must(resp.IsError())
	assert.Must(strings.HasPrefix(string(resp.Value), "NOPERM"))
	assert.Must(commandExists(backend.Commands(), []string{"ACL", "DRYRUN", "app_ro", "SELECT", "1"}))
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

	s, addr := openStartedProxy(config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(c, "INFO", backend.Addr())
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "NOPERM"))

	resp = proxyCall(c, "PING", backend.Addr())
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "NOPERM"))

	resp = proxyCall(c, "SLOTSINFO", backend.Addr())
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "NOPERM"))

	assert.Must(commandExists(backend.Commands(), []string{"AUTH", "app_ro", "secret"}))
	assert.Must(commandExists(backend.Commands(), []string{"INFO"}))
	assert.Must(commandExists(backend.Commands(), []string{"PING"}))
	assert.Must(commandExists(backend.Commands(), []string{"SLOTSINFO"}))
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

	s, addr := openStartedProxy(config)
	defer s.Close()
	aclInstallAppRO(x, s)
	fillProxySlotForKey(x, s, []byte("hot:key"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app_ro", "secret").IsString())

	resp := proxyCall(c, "GET", "hot:key")
	assert.Must(resp.IsBulkBytes() && string(resp.Value) == "cached")

	dryrunDeny = true
	resp = proxyCall(c, "GET", "hot:key")
	assert.Must(resp.IsError())
	assert.Must(strings.HasPrefix(string(resp.Value), "NOPERM"))
	assert.Must(commandExists(backend.Commands(), []string{"ACL", "DRYRUN", "app_ro", "GET", "hot:key"}))
}

func aclInstallAppRO(t testing.TB, s *Proxy) {
	t.Helper()
	assert.MustNoError(s.SetACL(&models.ACL{
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
	assert.MustNoError(s.FillSlot(&models.Slot{
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
