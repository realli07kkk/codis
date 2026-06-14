// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"strings"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

// aclBoundDB reports the bound db that the proxy's snapshot currently
// holds for the given user. Used to assert snapshot deep-copy preserved it.
func aclBoundDB(s *Proxy, name string) *int {
	snap := s.ACLSnapshot()
	if snap == nil || snap.Users == nil {
		return nil
	}
	u := snap.Users[name]
	if u == nil {
		return nil
	}
	return u.DB
}

func TestACLSnapshotCopiesBoundDB(t *testing.T) {
	s, _ := openProxy()
	defer s.Close()

	db3 := 3
	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app1",
			Enabled:        true,
			DB:             &db3,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}))

	got := aclBoundDB(s, "app1")
	assert.Must(got != nil && *got == 3)
}

// TestACLDBBoundUserRoutesToBoundDB: AUTH app1 (bound db=3) -> SET k v must
// reach backend at db 3 (backend observes SELECT 3 + the SET).
func TestACLDBBoundUserRoutesToBoundDB(t *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true
	assert.Must(config.BackendNumberDatabases >= 4)

	backend := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH":
			return redistest.OK()
		case "SELECT":
			return redistest.OK()
		case "SET":
			return redistest.OK()
		case "GET":
			return redistest.Bulk("from-backend")
		default:
			return redistest.OK()
		}
	})

	s, addr := openStartedProxy(config)
	defer s.Close()

	db3 := 3
	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app1",
			Enabled:        true,
			DB:             &db3,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}))
	fillProxySlotForKey(t, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app1", "secret").IsString())

	resp := proxyCall(c, "SET", "app:k", "v")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	// Backend user-pool must have issued SELECT 3 on its bound conn for app1.
	assert.Must(commandExists(backend.Commands(), []string{"SELECT", "3"}))
}

// TestACLDBBoundSessionSelectIsNoOp: after AUTH, SELECT 5 returns OK but
// subsequent GET still routes to the bound db. With BackendNumberDatabases=4
// and bound db=3, the proxy only ever opens pool connections on db 0..3, so a
// forwarded client SELECT 5 would be the sole source of a backend SELECT 5.
func TestACLDBBoundSessionSelectIsNoOp(t *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true
	config.BackendNumberDatabases = 4

	backend := redistest.NewServer(t, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	s, addr := openStartedProxy(config)
	defer s.Close()

	db3 := 3
	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app1",
			Enabled:        true,
			DB:             &db3,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}))
	fillProxySlotForKey(t, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app1", "secret").IsString())

	// SELECT 5 must return OK (client compatibility) — but must be a no-op.
	resp := proxyCall(c, "SELECT", "5")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	// Trigger a backend round-trip. The bound db=3 conn is opened by the
	// pool; a forwarded SELECT 5 would open a separate db=5 conn — but
	// pool only spans db 0..3, so any SELECT 5 here can only come from the
	// client command being forwarded.
	assert.Must(proxyCall(c, "GET", "app:k").IsString())
	assert.Must(commandExists(backend.Commands(), []string{"SELECT", "3"}))
	assert.Must(!commandExists(backend.Commands(), []string{"SELECT", "5"}))
}

// TestACLDBBoundDefaultUser: AUTH <pwd> (no username) resolves to default user
// and routes to default's bound db.
func TestACLDBBoundDefaultUser(t *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	backend := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH", "SELECT", "GET", "SET":
			return redistest.OK()
		default:
			return redistest.OK()
		}
	})

	s, addr := openStartedProxy(config)
	defer s.Close()

	db2 := 2
	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "default",
			Enabled:        true,
			DB:             &db2,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}))
	fillProxySlotForKey(t, s, []byte("k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "secret").IsString())

	resp := proxyCall(c, "SET", "k", "v")
	assert.Must(resp.IsString())

	assert.Must(commandExists(backend.Commands(), []string{"SELECT", "2"}))
}

// TestACLDBUnboundUserSelectTakesEffect: a user without DB binding keeps
// legacy SELECT semantics (SELECT 4 reaches backend).
func TestACLDBUnboundUserSelectTakesEffect(t *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	backend := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH", "SELECT", "GET":
			return redistest.OK()
		default:
			return redistest.OK()
		}
	})

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
	fillProxySlotForKey(t, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app_ro", "secret").IsString())

	// Unbound session: SELECT 4 must change routing db.
	resp := proxyCall(c, "SELECT", "4")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")
	assert.Must(commandExists(backend.Commands(), []string{"SELECT", "4"}))
}

// TestACLDBBoundOutOfRangeFailClosed: db=99 with backend_number_databases=16
// must reject AUTH without writing identity; subsequent command is NOAUTH.
func TestACLDBBoundOutOfRangeFailClosed(t *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	backend := redistest.NewServer(t, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	s, addr := openStartedProxy(config)
	defer s.Close()

	dbBad := 99
	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app_bad",
			Enabled:        true,
			DB:             &dbBad,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}))
	fillProxySlotForKey(t, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()

	resp := proxyCall(c, "AUTH", "app_bad", "secret")
	assert.Must(resp.IsError())
	assert.Must(strings.Contains(string(resp.Value), "exceeds backend db count"))

	// No identity written -> next non-AUTH command must be NOAUTH.
	resp = proxyCall(c, "GET", "app:k")
	assert.Must(resp.IsError())
	assert.Must(string(resp.Value) == "NOAUTH Authentication required")

	// No backend command beyond AUTH/SELECT must have been emitted for app_bad.
	for _, cmd := range backend.Commands() {
		if len(cmd) >= 1 && strings.EqualFold(cmd[0], "GET") {
			t.Fatalf("GET reached backend despite fail-closed AUTH: %v", cmd)
		}
	}
}

// TestACLDBBoundUserRevisionReAuthUsesNewDB: switching the bound db and
// bumping revision must stale the old session; re-auth routes to the new db.
func TestACLDBBoundUserRevisionReAuthUsesNewDB(t *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	backend := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH", "SELECT", "GET", "SET":
			return redistest.OK()
		default:
			return redistest.OK()
		}
	})

	s, addr := openStartedProxy(config)
	defer s.Close()

	db3 := 3
	acl := &models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app1",
			Enabled:        true,
			DB:             &db3,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}
	assert.MustNoError(s.SetACL(acl))
	fillProxySlotForKey(t, s, []byte("app:k"), backend.Addr())

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app1", "secret").IsString())

	// Flip bound db 3 -> 7 and bump revision; old session must go stale.
	db7 := 7
	acl.Revision = 2
	acl.Users[0].DB = &db7
	assert.MustNoError(s.SetACL(acl))

	// Pre-re-auth non-AUTH command: NOAUTH (stale).
	resp := proxyCall(c, "GET", "app:k")
	assert.Must(resp.IsError())
	assert.Must(string(resp.Value) == "NOAUTH Authentication required")

	// Re-auth routes to the new bound db 7.
	assert.Must(proxyCall(c, "AUTH", "app1", "secret").IsString())
	assert.Must(proxyCall(c, "SET", "app:k", "v").IsString())
	assert.Must(commandExists(backend.Commands(), []string{"SELECT", "7"}))
}

// TestACLDBBoundUserMigratingSlotDryRunSucceeds: a DB-bound user issuing an
// allowed command on a slot mid-migration (ForwardSemiAsync) must succeed, and
// the migration wrapper's ACL DRYRUN on the source backend must use the bound
// db — proving ForcedDB does not corrupt the internal migration/DRYRUN path.
func TestACLDBBoundUserMigratingSlotDryRunSucceeds(t *testing.T) {
	config := newProxyConfig()
	config.CodisACLEnabled = true

	var sourceSawDryRunOnBoundDB bool
	source := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH", "SELECT":
			return redistest.OK()
		case "ACL":
			// DRYRUN is dispatched on the (addr, db) conn matching the
			// request's r.Database — i.e. the bound db. We assert the
			// sequence [ACL, DRYRUN, app1, GET, app:k].
			if len(args) >= 5 && strings.EqualFold(args[1], "DRYRUN") &&
				args[2] == "app1" && strings.EqualFold(args[3], "GET") {
				sourceSawDryRunOnBoundDB = true
				return redistest.OK()
			}
			return redistest.Error("ERR unexpected ACL")
		case "SLOTSMGRT-EXEC-WRAPPER":
			// Migration wrapper reached source; reply (2, OK) = migrated.
			return redistest.Array(redistest.Int("2"), redistest.OK())
		default:
			return redistest.OK()
		}
	})
	target := redistest.NewServer(t, func(args []string) *redistest.Resp {
		switch strings.ToUpper(args[0]) {
		case "AUTH", "SELECT", "GET", "SET":
			return redistest.OK()
		default:
			return redistest.OK()
		}
	})

	s, addr := openStartedProxy(config)
	defer s.Close()

	db3 := 3
	assert.MustNoError(s.SetACL(&models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "app1",
			Enabled:        true,
			DB:             &db3,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"+@all", "~*"},
		}},
	}))

	// Place the key's slot in migration: target = backend, source = migrate-from.
	key := []byte("app:k")
	slot := int(Hash(key) % MaxSlotNum)
	assert.MustNoError(s.FillSlot(&models.Slot{
		Id:                 slot,
		BackendAddr:        target.Addr(),
		BackendAddrGroupId: 1,
		MigrateFrom:        source.Addr(),
		ForwardMethod:      models.ForwardSemiAsync,
	}))

	c := dialProxy(addr)
	defer c.Close()
	assert.Must(proxyCall(c, "AUTH", "app1", "secret").IsString())

	// Allowed command on a migrating slot must succeed (DRYRUN passes on
	// source, then the wrapper routes the command).
	resp := proxyCall(c, "GET", "app:k")
	assert.Must(resp.IsString() && string(resp.Value) == "OK")

	// The migration wrapper issued ACL DRYRUN on the source backend using the
	// bound db 3 conn — ForcedDB propagated consistently, not corrupted.
	assert.Must(sourceSawDryRunOnBoundDB)
	assert.Must(commandExists(source.Commands(), []string{"SELECT", "3"}))
}
