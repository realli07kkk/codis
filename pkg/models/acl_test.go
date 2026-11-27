// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// acl_test.go exercises the exported ACL model and store API, so the store
// cases run over the in-memory coordinator fake instead of a real backend.
package models_test

import (
	"encoding/json"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/modelstest"
)

func TestACLModelEncodeDecode(t *testing.T) {
	acl := &models.ACL{
		Revision:  12,
		Enabled:   true,
		UpdatedAt: "2026-06-04T00:00:00Z",
		Users: []*models.ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{"0123456789abcdef"},
			Rules:          []string{"~app:*", "+@read"},
		}},
	}

	var decoded models.ACL
	if err := json.Unmarshal(acl.Encode(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Revision != acl.Revision || !decoded.Enabled || decoded.UpdatedAt != acl.UpdatedAt {
		t.Fatalf("decoded acl mismatch: %+v", decoded)
	}
	if len(decoded.Users) != 1 || decoded.Users[0].Name != "app_ro" || len(decoded.Users[0].Rules) != 2 {
		t.Fatalf("decoded acl users mismatch: %+v", decoded.Users)
	}
}

func encodeACLUser(t *testing.T, u *models.ACLUser) []byte {
	t.Helper()
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// DB-bound users must round-trip the db pointer, and legacy JSON (no db field)
// must decode to a nil DB so existing records behave as unbound.
func TestACLUserDBBindingEncodeDecode(t *testing.T) {
	db3 := 3
	bound := &models.ACLUser{
		Name:           "app1",
		Enabled:        true,
		DB:             &db3,
		PasswordHashes: []string{"hash1"},
		Rules:          []string{"+@all", "~*"},
	}
	raw := encodeACLUser(t, bound)
	var decodedBound models.ACLUser
	if err := json.Unmarshal(raw, &decodedBound); err != nil {
		t.Fatal(err)
	}
	if decodedBound.DB == nil || *decodedBound.DB != 3 {
		t.Fatalf("bound user db not round-tripped: %+v", decodedBound.DB)
	}

	// Legacy record without a db field — must decode as unbound (DB==nil).
	legacy := []byte(`{"name":"legacy","enabled":true,"password_hashes":["h"],"rules":["+@read"]}`)
	var decodedLegacy models.ACLUser
	if err := json.Unmarshal(legacy, &decodedLegacy); err != nil {
		t.Fatal(err)
	}
	if decodedLegacy.DB != nil {
		t.Fatalf("legacy user must decode with nil db, got %v", *decodedLegacy.DB)
	}

	// Explicit db=0 must survive round-trip distinct from unbound.
	db0 := 0
	zero := &models.ACLUser{Name: "zero", Enabled: true, DB: &db0, Rules: []string{"+@all"}}
	zeroRaw := encodeACLUser(t, zero)
	if string(zeroRaw) == "" {
		t.Fatal("empty encode")
	}
	var decodedZero models.ACLUser
	if err := json.Unmarshal(zeroRaw, &decodedZero); err != nil {
		t.Fatal(err)
	}
	if decodedZero.DB == nil || *decodedZero.DB != 0 {
		t.Fatalf("db=0 must round-trip as bound-to-0, got %+v", decodedZero.DB)
	}
}

func TestStoreACLPathAndUpdate(t *testing.T) {
	client := modelstest.NewState().NewClient()
	defer client.Close()

	store := models.NewStore(client, "codis-demo")
	if got, want := store.ACLPath(), "/codis3/codis-demo/acl"; got != want {
		t.Fatalf("ACLPath = %q, want %q", got, want)
	}

	acl := &models.ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*models.ACLUser{{
			Name:           "default",
			Enabled:        true,
			PasswordHashes: []string{"hash"},
			Rules:          []string{"+@all"},
		}},
	}
	if err := store.UpdateACL(acl); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadACL(true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 1 || len(loaded.Users) != 1 || loaded.Users[0].Name != "default" {
		t.Fatalf("loaded acl mismatch: %+v", loaded)
	}
}
