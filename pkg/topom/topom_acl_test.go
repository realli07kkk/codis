// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func openACLTopom() *Topom {
	cfg := NewDefaultConfig()
	cfg.AdminAddr = "127.0.0.1:0"
	cfg.ProductName = "topom_acl_test"
	cfg.ProductAuth = "topom_auth"
	t, err := New(newDiskClient(), cfg)
	assert.MustNoError(err)
	assert.MustNoError(t.Start(false))
	return t
}

func TestUpdateACLSyncsRedisAndStoresRedactedView(x *testing.T) {
	t := openACLTopom()
	defer t.Close()

	server := redistest.NewServer(x, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	g := &models.Group{Id: 1, Servers: []*models.GroupServer{{Addr: server.Addr()}}}
	t.dirtyGroupCache(g.Id)
	assert.MustNoError(t.storeCreateGroup(g))

	view, err := t.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:        "app_ro",
			Enabled:     true,
			NewPassword: "secret",
			Rules:       []string{"~app:*", "+@read"},
		}},
	})
	assert.MustNoError(err)
	assert.Must(view.Revision == 1)
	assert.Must(view.Enabled)
	assert.Must(view.SyncStatus == "ready")
	assert.Must(len(view.Users) == 1)
	assert.Must(view.Users[0].PasswordCount == 1)
	assert.Must(reflect.DeepEqual(view.Users[0].Rules, []string{"~app:*", "+@read"}))

	hash := models.ACLPasswordHash([]byte("secret"))
	assert.Must(commandExists(server.Commands(), []string{"AUTH", "topom_auth"}))
	assert.Must(commandExists(server.Commands(), []string{"ACL", "SETUSER", "app_ro", "reset", "on", "#" + hash, "~app:*", "+@read"}))

	acl, err := t.store.LoadACL(true)
	assert.MustNoError(err)
	assert.Must(acl.Revision == 1)
	assert.Must(len(acl.Users) == 1)
	assert.Must(acl.Users[0].PasswordHashes[0] == hash)
}

func TestUpdateACLSyncsBackendServiceUserWithHash(x *testing.T) {
	cfg := NewDefaultConfig()
	cfg.AdminAddr = "127.0.0.1:0"
	cfg.ProductName = "topom_acl_service_user_test"
	cfg.ProductAuth = "topom_auth"
	cfg.BackendAuthUsername = "svc"
	cfg.BackendAuthPassword = "backend-secret"
	topom, err := New(newDiskClient(), cfg)
	assert.MustNoError(err)
	assert.MustNoError(topom.Start(false))
	defer topom.Close()

	server := redistest.NewServer(x, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	g := &models.Group{Id: 1, Servers: []*models.GroupServer{{Addr: server.Addr()}}}
	topom.dirtyGroupCache(g.Id)
	assert.MustNoError(topom.storeCreateGroup(g))

	_, err = topom.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:        "app_ro",
			Enabled:     true,
			NewPassword: "secret",
			Rules:       []string{"~app:*", "+@read"},
		}},
	})
	assert.MustNoError(err)

	hash := models.ACLPasswordHash([]byte("backend-secret"))
	assert.Must(commandExists(server.Commands(), []string{"AUTH", "svc", "backend-secret"}))
	assert.Must(commandExists(server.Commands(), []string{"ACL", "SETUSER", "svc", "reset", "on", "#" + hash, "+@all", "~*"}))
}

func TestUpdateACLFailureDoesNotStoreRevision(x *testing.T) {
	t := openACLTopom()
	defer t.Close()

	server := redistest.NewServer(x, func(args []string) *redistest.Resp {
		if strings.EqualFold(args[0], "ACL") {
			return redistest.Error("ERR acl failed")
		}
		return redistest.OK()
	})

	g := &models.Group{Id: 1, Servers: []*models.GroupServer{{Addr: server.Addr()}}}
	t.dirtyGroupCache(g.Id)
	assert.MustNoError(t.storeCreateGroup(g))

	_, err := t.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:        "app_ro",
			Enabled:     true,
			NewPassword: "secret",
			Rules:       []string{"~app:*", "+@read"},
		}},
	})
	assert.Must(err != nil)
	acl, err := t.store.LoadACL(false)
	assert.MustNoError(err)
	assert.Must(acl == nil)
}

func TestUpdateACLProxySyncFailureRecordsAllFailedTokens(x *testing.T) {
	topom := openACLTopom()
	defer topom.Close()

	received := make(chan *models.ACL, 1)
	okProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		acl := &models.ACL{}
		assert.MustNoError(json.NewDecoder(r.Body).Decode(acl))
		received <- acl
		w.WriteHeader(http.StatusOK)
	}))
	defer okProxy.Close()

	failedProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer failedProxy.Close()

	failedProxy2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer failedProxy2.Close()

	okURL, err := url.Parse(okProxy.URL)
	assert.MustNoError(err)
	failedURL, err := url.Parse(failedProxy.URL)
	assert.MustNoError(err)
	failedURL2, err := url.Parse(failedProxy2.URL)
	assert.MustNoError(err)

	contextCreateProxy(topom, &models.Proxy{Token: "a_failed", AdminAddr: failedURL.Host})
	contextCreateProxy(topom, &models.Proxy{Token: "b_ok", AdminAddr: okURL.Host})
	contextCreateProxy(topom, &models.Proxy{Token: "c_failed", AdminAddr: failedURL2.Host})

	_, err = topom.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:        "app_ro",
			Enabled:     true,
			NewPassword: "secret",
			Rules:       []string{"~app:*", "+@read"},
		}},
	})
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "a_failed"))
	assert.Must(strings.Contains(err.Error(), "c_failed"))

	select {
	case acl := <-received:
		assert.Must(acl.Revision == 1)
	default:
		x.Fatal("successful proxy did not receive ACL sync")
	}

	view, err := topom.GetACL()
	assert.MustNoError(err)
	assert.Must(view.SyncStatus == "proxy_sync_failed:a_failed,c_failed")
}

func TestSyncACLRetriesStoredRevision(x *testing.T) {
	topom := openACLTopom()
	defer topom.Close()

	var failedProxyOK atomic.Bool
	failedProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !failedProxyOK.Load() {
			http.Error(w, "failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer failedProxy.Close()

	failedURL, err := url.Parse(failedProxy.URL)
	assert.MustNoError(err)
	contextCreateProxy(topom, &models.Proxy{Token: "failed", AdminAddr: failedURL.Host})

	_, err = topom.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:        "app_ro",
			Enabled:     true,
			NewPassword: "secret",
			Rules:       []string{"~app:*", "+@read"},
		}},
	})
	assert.Must(err != nil)

	view, err := topom.GetACL()
	assert.MustNoError(err)
	assert.Must(view.Revision == 1)
	assert.Must(view.SyncStatus == "proxy_sync_failed:failed")

	failedProxyOK.Store(true)
	view, err = topom.SyncACL()
	assert.MustNoError(err)
	assert.Must(view.Revision == 1)
	assert.Must(view.SyncStatus == "ready")
}

func TestGroupAddServerSyncsStoredACL(x *testing.T) {
	topom := openACLTopom()
	defer topom.Close()

	assert.MustNoError(topom.CreateGroup(1))
	_, err := topom.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:        "app_ro",
			Enabled:     true,
			NewPassword: "secret",
			Rules:       []string{"~app:*", "+@read"},
		}},
	})
	assert.MustNoError(err)

	server := redistest.NewServer(x, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	assert.MustNoError(topom.GroupAddServer(1, "", server.Addr()))

	hash := models.ACLPasswordHash([]byte("secret"))
	assert.Must(commandExists(server.Commands(), []string{"AUTH", "topom_auth"}))
	assert.Must(commandExists(server.Commands(), []string{"ACL", "SETUSER", "app_ro", "reset", "on", "#" + hash, "~app:*", "+@read"}))
}

// TestUpdateACLEchoesDBAndKeepsRedisACLEndpointClean: submitting a user with a
// bound db must (a) echo db back in the GET view, (b) persist it in the store,
// and (c) NOT render any db/select token into the Redis ACL SETUSER command.
func TestUpdateACLEchoesDBAndKeepsRedisACLEndpointClean(x *testing.T) {
	t := openACLTopom()
	defer t.Close()

	server := redistest.NewServer(x, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	g := &models.Group{Id: 1, Servers: []*models.GroupServer{{Addr: server.Addr()}}}
	t.dirtyGroupCache(g.Id)
	assert.MustNoError(t.storeCreateGroup(g))

	db3 := 3
	view, err := t.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:        "app1",
			Enabled:     true,
			NewPassword: "secret",
			DB:          &db3,
			Rules:       []string{"~app:*", "+@read"},
		}},
	})
	assert.MustNoError(err)
	assert.Must(len(view.Users) == 1)
	assert.Must(view.Users[0].DB != nil && *view.Users[0].DB == 3)

	// Store must persist db so proxy sync sees it.
	acl, err := t.store.LoadACL(true)
	assert.MustNoError(err)
	assert.Must(len(acl.Users) == 1)
	assert.Must(acl.Users[0].DB != nil && *acl.Users[0].DB == 3)

	// Redis backend ACL SETUSER must NOT contain any db token (db is a
	// proxy-only routing attribute).
	hash := models.ACLPasswordHash([]byte("secret"))
	setuser := []string{"ACL", "SETUSER", "app1", "reset", "on", "#" + hash, "~app:*", "+@read"}
	assert.Must(commandExists(server.Commands(), setuser))
	for _, cmd := range server.Commands() {
		if len(cmd) >= 4 && strings.EqualFold(cmd[0], "ACL") && strings.EqualFold(cmd[1], "SETUSER") {
			for _, tok := range cmd[3:] {
				lower := strings.ToLower(tok)
				if lower == "db" || strings.HasPrefix(lower, "select") {
					x.Fatalf("ACL SETUSER must not contain db token, got %v", cmd)
				}
			}
		}
	}
}

// TestUpdateACLRejectsNegativeDB: topom validates db>=0 before persisting.
func TestUpdateACLRejectsNegativeDB(x *testing.T) {
	t := openACLTopom()
	defer t.Close()

	dbBad := -1
	_, err := t.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:        "app1",
			Enabled:     true,
			NewPassword: "secret",
			DB:          &dbBad,
			Rules:       []string{"~app:*", "+@read"},
		}},
	})
	assert.Must(err != nil)
	assert.Must(strings.Contains(err.Error(), "invalid db"))
}

// TestUpdateACLEditDBToUnbound: a user previously bound to a db must become
// unbound when the follow-up request omits the db field (request db==nil).
func TestUpdateACLEditDBToUnbound(x *testing.T) {
	t := openACLTopom()
	defer t.Close()

	server := redistest.NewServer(x, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	g := &models.Group{Id: 1, Servers: []*models.GroupServer{{Addr: server.Addr()}}}
	t.dirtyGroupCache(g.Id)
	assert.MustNoError(t.storeCreateGroup(g))

	db3 := 3
	_, err := t.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:           "app1",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			DB:             &db3,
			Rules:          []string{"~app:*", "+@read"},
		}},
	})
	assert.MustNoError(err)

	// Follow-up without DB field → unbound (DB == nil).
	view, err := t.UpdateACL(&ACLUpdateRequest{
		Enabled: true,
		Users: []*ACLUserUpdate{{
			Name:           "app1",
			Enabled:        true,
			PasswordHashes: []string{models.ACLPasswordHash([]byte("secret"))},
			Rules:          []string{"~app:*", "+@read"},
		}},
	})
	assert.MustNoError(err)
	assert.Must(len(view.Users) == 1)
	assert.Must(view.Users[0].DB == nil)
}

func commandExists(commands [][]string, want []string) bool {
	for _, cmd := range commands {
		if len(cmd) != len(want) {
			continue
		}
		match := true
		for i := range cmd {
			if !strings.EqualFold(cmd[i], want[i]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
