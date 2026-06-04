// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CodisLabs/codis/pkg/topom"
)

func TestRedactACLUpdateRequestHidesNewPassword(t *testing.T) {
	req := &topom.ACLUpdateRequest{
		Enabled: true,
		Users: []*topom.ACLUserUpdate{{
			Name:           "app_ro",
			Enabled:        true,
			NewPassword:    "secret",
			PasswordHashes: []string{"hash"},
			Rules:          []string{"~app:*", "+@read"},
		}},
	}

	redacted := redactACLUpdateRequest(req)
	b, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "secret") {
		t.Fatalf("redacted output contains password: %s", out)
	}
	if redacted.Users[0].NewPassword != "<redacted>" {
		t.Fatalf("redacted marker missing: %+v", redacted.Users[0])
	}
	if req.Users[0].NewPassword != "secret" {
		t.Fatalf("redaction mutated original request: %+v", req.Users[0])
	}
}
