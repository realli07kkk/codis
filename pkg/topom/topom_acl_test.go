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
