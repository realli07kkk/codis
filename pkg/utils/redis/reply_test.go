// Copyright 2026 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import "testing"

func TestReplyConversionHelpers(t *testing.T) {
	if s, err := replyString([]byte("codis")); err != nil || s != "codis" {
		t.Fatalf("replyString = %q, %v", s, err)
	}
	if n, err := replyInt("42"); err != nil || n != 42 {
		t.Fatalf("replyInt = %d, %v", n, err)
	}
	ints, err := replyInts([]interface{}{int64(3), []byte("11")})
	if err != nil {
		t.Fatal(err)
	}
	if len(ints) != 2 || ints[0] != 3 || ints[1] != 11 {
		t.Fatalf("replyInts = %v", ints)
	}
	m, err := replyStringMap([]interface{}{
		[]byte("name"), "codis-1",
		"config-epoch", []byte("9"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m["name"] != "codis-1" || m["config-epoch"] != "9" {
		t.Fatalf("replyStringMap = %v", m)
	}
}

func TestReplyConversionHelpersRejectMalformedShape(t *testing.T) {
	if _, err := replyString(42); err == nil {
		t.Fatal("expected replyString to reject integer")
	}
	if _, err := replyInts("not-array"); err == nil {
		t.Fatal("expected replyInts to reject non-array")
	}
	if _, err := replyStringMap([]interface{}{"name"}); err == nil {
		t.Fatal("expected replyStringMap to reject odd values")
	}
}
