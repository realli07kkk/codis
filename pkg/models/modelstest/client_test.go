// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package modelstest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
)

func TestCreateRejectsExistingKey(t *testing.T) {
	c := NewState().NewClient()
	if err := c.Create("/a/b", []byte("v1")); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := c.Create("/a/b", []byte("v2")); !errors.Is(err, ErrExist) {
		t.Fatalf("second create error = %v, want ErrExist", err)
	}
	b, err := c.Read("/a/b", true)
	if err != nil || string(b) != "v1" {
		t.Fatalf("read after failed create = %q, %v", b, err)
	}
}

func TestUpdateIsUpsert(t *testing.T) {
	c := NewState().NewClient()
	if err := c.Update("/a", []byte("v1")); err != nil {
		t.Fatalf("update missing: %v", err)
	}
	if err := c.Update("/a", []byte("v2")); err != nil {
		t.Fatalf("update existing: %v", err)
	}
	b, err := c.Read("/a", true)
	if err != nil || string(b) != "v2" {
		t.Fatalf("read = %q, %v", b, err)
	}
}

func TestDeleteMissingKeySucceeds(t *testing.T) {
	c := NewState().NewClient()
	if err := c.Update("/a", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete("/a"); err != nil {
		t.Fatalf("delete existing: %v", err)
	}
	if err := c.Delete("/a"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestReadMustFlag(t *testing.T) {
	c := NewState().NewClient()
	if b, err := c.Read("/missing", false); b != nil || err != nil {
		t.Fatalf("read missing with must=false = %q, %v", b, err)
	}
	if b, err := c.Read("/missing", true); b != nil || !errors.Is(err, ErrNotExist) {
		t.Fatalf("read missing with must=true = %q, %v", b, err)
	}
}

func TestListReturnsSortedDirectChildren(t *testing.T) {
	c := NewState().NewClient()
	for _, p := range []string{
		"/root/group/group-0002",
		"/root/group/group-0001",
		"/root/group/group-0001/child",
		"/root/proxy/proxy-x",
	} {
		if err := c.Create(p, []byte("v")); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := c.List("/root/group", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/root/group/group-0001", "/root/group/group-0002"}
	if len(paths) != len(want) {
		t.Fatalf("list = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("list = %v, want %v", paths, want)
		}
	}
}

func TestListMustFlag(t *testing.T) {
	c := NewState().NewClient()
	if p, err := c.List("/empty", false); p != nil || err != nil {
		t.Fatalf("list empty with must=false = %v, %v", p, err)
	}
	if p, err := c.List("/empty", true); p != nil || !errors.Is(err, ErrNotExist) {
		t.Fatalf("list empty with must=true = %v, %v", p, err)
	}
}

func TestCloseIsIndependentPerClient(t *testing.T) {
	s := NewState()
	c1 := s.NewClient()
	c2 := s.NewClient()

	if err := c1.Create("/k", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := c1.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c1.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := c1.Read("/k", true); !errors.Is(err, ErrClosedClient) {
		t.Fatalf("read on closed client error = %v, want ErrClosedClient", err)
	}
	if err := c1.Create("/k2", []byte("v")); !errors.Is(err, ErrClosedClient) {
		t.Fatalf("create on closed client error = %v, want ErrClosedClient", err)
	}
	b, err := c2.Read("/k", true)
	if err != nil || string(b) != "v" {
		t.Fatalf("sibling client read = %q, %v", b, err)
	}
}

func TestLockClientRevisions(t *testing.T) {
	s := NewState()
	c1 := s.NewClient()
	c2 := s.NewClient()

	ok, create, err := c1.CreateWithRevision("/lock", []byte("v1"))
	if err != nil || !ok || create <= 0 {
		t.Fatalf("create with revision = %t, %d, %v; want true and a positive revision", ok, create, err)
	}

	// A conditional create on an existing key is rejected, not an error.
	ok, revision, err := c2.CreateWithRevision("/lock", []byte("v2"))
	if err != nil || ok || revision != 0 {
		t.Fatalf("second create with revision = %t, %d, %v; want false, 0, nil", ok, revision, err)
	}

	b, gotCreate, gotMod, lease, err := c2.ReadWithRevision("/lock")
	if err != nil || string(b) != "v1" || gotCreate != create || gotMod != create || lease != 0 {
		t.Fatalf("read = %q, %d, %d, %d, %v; want v1, %d, %d, 0", b, gotCreate, gotMod, lease, err, create, create)
	}

	// An update keeps the create revision and bumps the mod revision.
	if err := c2.Update("/lock", []byte("v3")); err != nil {
		t.Fatal(err)
	}
	if _, gotCreate, gotMod, _, err = c2.ReadWithRevision("/lock"); err != nil {
		t.Fatal(err)
	}
	if gotCreate != create || gotMod <= create {
		t.Fatalf("after update create/mod = %d/%d, want %d and > %d", gotCreate, gotMod, create, create)
	}

	// A delete conditioned on another create revision is rejected and keeps
	// the record.
	if ok, err := c1.DeleteByCreateRevision("/lock", create+1); err != nil || ok {
		t.Fatalf("delete with a wrong revision = %t, %v; want false, nil", ok, err)
	}
	if b, _, _, _, err := c1.ReadWithRevision("/lock"); err != nil || string(b) != "v3" {
		t.Fatalf("read after the rejected delete = %q, %v", b, err)
	}
	if ok, err := c1.DeleteByCreateRevision("/lock", create); err != nil || !ok {
		t.Fatalf("delete with the observed revision = %t, %v; want true, nil", ok, err)
	}
	if b, create, mod, lease, err := c1.ReadWithRevision("/lock"); err != nil ||
		b != nil || create != 0 || mod != 0 || lease != 0 {
		t.Fatalf("read after delete = %q, %d, %d, %d, %v; want zero values", b, create, mod, lease, err)
	}
}

func TestFaultsInjectUnknownOutcomes(t *testing.T) {
	s := NewState()
	inner := s.NewClient()
	faults := &Faults{}
	client := Faulty(inner, faults)

	faults.Set(true, false, false, false)
	if _, _, err := client.CreateWithRevision("/before", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("create before commit error = %v, want an unknown outcome", err)
	}
	if b, _, _, _, err := inner.ReadWithRevision("/before"); err != nil || b != nil {
		t.Fatalf("key after a failed commit = %q, %v; want none", b, err)
	}

	faults.Set(false, true, false, false)
	if _, _, err := client.CreateWithRevision("/lost", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost create response error = %v, want an unknown outcome", err)
	}
	if b, _, _, _, err := client.ReadWithRevision("/lost"); err != nil || b == nil {
		t.Fatalf("key after a lost response = %q, %v; want the committed record", b, err)
	}

	faults.Set(false, false, true, false)
	if _, _, _, _, err := client.ReadWithRevision("/lost"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read-back error = %v, want an unknown outcome", err)
	}

	faults.Set(false, false, false, true)
	_, create, _, _, err := client.ReadWithRevision("/lost")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := client.DeleteByCreateRevision("/lost", create+1); err != nil || ok {
		t.Fatalf("delete with a wrong revision = %t, %v; want false, nil", ok, err)
	}
	if ok, err := client.DeleteByCreateRevision("/lost", create); err == nil || ok {
		t.Fatalf("delete with a lost response = %t, %v; want false and an injected error", ok, err)
	}
	if b, _, _, _, err := inner.ReadWithRevision("/lost"); err != nil || b != nil {
		t.Fatalf("key after the lost delete response = %q, %v; want it deleted", b, err)
	}
}

func TestFaultsInjectRejections(t *testing.T) {
	s := NewState()
	inner := s.NewClient()
	faults := &Faults{}
	client := Faulty(inner, faults)

	faults.SetRejections(true, false)
	_, _, err := client.CreateWithRevision("/refused", nil)
	if err == nil {
		t.Fatal("create was not refused")
	}
	if !models.IsRejected(err) {
		t.Fatalf("create refusal = %v, want models.IsRejected", err)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("create refusal = %v, want the reason", err)
	}
	if b, _, _, _, err := inner.ReadWithRevision("/refused"); err != nil || b != nil {
		t.Fatalf("key after a refused create = %q, %v; want none", b, err)
	}

	faults.Clear()
	if _, _, err := client.CreateWithRevision("/kept", nil); err != nil {
		t.Fatalf("create after clearing the refusal: %v", err)
	}
	_, create, _, _, err := client.ReadWithRevision("/kept")
	if err != nil {
		t.Fatal(err)
	}

	faults.SetRejections(false, true)
	ok, err := client.DeleteByCreateRevision("/kept", create)
	if err == nil || ok {
		t.Fatalf("refused delete = %t, %v; want false and an error", ok, err)
	}
	if !models.IsRejected(err) {
		t.Fatalf("delete refusal = %v, want models.IsRejected", err)
	}
	if b, _, _, _, err := inner.ReadWithRevision("/kept"); err != nil || b == nil {
		t.Fatalf("key after a refused delete = %q, %v; want it kept", b, err)
	}

	faults.Clear()
	if ok, err := client.DeleteByCreateRevision("/kept", create); err != nil || !ok {
		t.Fatalf("delete after clearing the refusal = %t, %v; want true, nil", ok, err)
	}
}
