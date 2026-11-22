// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"bytes"
	"testing"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
)

func TestForwardCommandLiteralsAreShared(t *testing.T) {
	cases := []struct {
		name  string
		value []byte
		want  string
	}{
		{"slotsmgrt", cmdSlotsMgrttagOne, "SLOTSMGRTTAGONE"},
		{"slotsmgrt timeout", cmdSlotsMgrttagTimeout, "3000"},
		{"slotsmgrt exec wrapper", cmdSlotsMgrtExecWrapper, "SLOTSMGRT-EXEC-WRAPPER"},
		{"acl", cmdACL, "ACL"},
		{"acl dryrun", cmdACLDryRun, "DRYRUN"},
	}
	for _, c := range cases {
		if got := string(c.value); got != c.want {
			t.Fatalf("%s literal = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestForwardCommandLiteralsAreReadOnly(t *testing.T) {
	// The wrappers wrap the package-level literal without copying it, so every
	// request built from it shares the same backing array.
	first := redis.NewBulkBytes(cmdACL)
	second := redis.NewBulkBytes(cmdACL)
	if len(first.Value) == 0 || len(second.Value) == 0 {
		t.Fatal("ACL literal is empty")
	}
	if &first.Value[0] != &second.Value[0] {
		t.Fatal("ACL literal was copied for every request")
	}

	// Encoding only reads Resp.Value: two encodes of the shared literal must
	// produce identical bytes and leave the literal untouched.
	want := "*2\r\n$3\r\nACL\r\n$6\r\nDRYRUN\r\n"
	for i := 0; i < 2; i++ {
		var buf bytes.Buffer
		enc := redis.NewEncoder(&buf)
		resp := redis.NewArray([]*redis.Resp{
			redis.NewBulkBytes(cmdACL),
			redis.NewBulkBytes(cmdACLDryRun),
		})
		if err := enc.Encode(resp, true); err != nil {
			t.Fatalf("encode %d: %v", i, err)
		}
		if got := buf.String(); got != want {
			t.Fatalf("encode %d = %q, want %q", i, got, want)
		}
	}
	if got := string(cmdACL); got != "ACL" {
		t.Fatalf("ACL literal changed to %q", got)
	}
	if got := string(cmdACLDryRun); got != "DRYRUN" {
		t.Fatalf("DRYRUN literal changed to %q", got)
	}
}
