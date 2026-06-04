// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CodisLabs/codis/pkg/models/fs"
)

func TestACLModelEncodeDecode(t *testing.T) {
	acl := &ACL{
		Revision:  12,
		Enabled:   true,
		UpdatedAt: "2026-06-04T00:00:00Z",
		Users: []*ACLUser{{
			Name:           "app_ro",
			Enabled:        true,
			PasswordHashes: []string{"0123456789abcdef"},
			Rules:          []string{"~app:*", "+@read"},
		}},
	}

	var decoded ACL
	if err := jsonDecode(&decoded, acl.Encode()); err != nil {
		t.Fatal(err)
	}
	if decoded.Revision != acl.Revision || !decoded.Enabled || decoded.UpdatedAt != acl.UpdatedAt {
		t.Fatalf("decoded acl mismatch: %+v", decoded)
	}
	if len(decoded.Users) != 1 || decoded.Users[0].Name != "app_ro" || len(decoded.Users[0].Rules) != 2 {
		t.Fatalf("decoded acl users mismatch: %+v", decoded.Users)
	}
}

func TestStoreACLPathAndUpdate(t *testing.T) {
	root := filepath.Join(os.TempDir(), "codis-acl-store-test")
	_ = os.RemoveAll(root)
	defer os.RemoveAll(root)

	client, err := fsclient.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	store := NewStore(client, "codis-demo")
	if got, want := store.ACLPath(), "/codis3/codis-demo/acl"; got != want {
		t.Fatalf("ACLPath = %q, want %q", got, want)
	}

	acl := &ACL{
		Revision: 1,
		Enabled:  true,
		Users: []*ACLUser{{
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
