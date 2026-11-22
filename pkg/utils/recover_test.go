// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package utils

import (
	"strings"
	"testing"
)

func TestRecoverAsError(t *testing.T) {
	run := func(panicValue any) (err error) {
		defer RecoverAsError("test scope", &err)
		if panicValue != nil {
			panic(panicValue)
		}
		return nil
	}

	if err := run(nil); err != nil {
		t.Fatalf("no panic should not produce an error, got %v", err)
	}

	err := run("boom")
	if err == nil {
		t.Fatal("a panic should be converted into an error")
	}
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "test scope") {
		t.Fatalf("error %q should mention the panic and the scope", err)
	}
}
