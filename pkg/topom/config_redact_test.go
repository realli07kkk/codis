// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigStringRedactsCredentials(t *testing.T) {
	c := NewDefaultConfig()
	c.CoordinatorAuth = "coordinator-secret"
	c.ProductAuth = "product-secret"
	c.BackendAuthPassword = "backend-secret"
	c.RDBAnalysisRemoteFetchAuth = "rdb-fetch-secret"

	out := c.String()
	for _, secret := range []string{
		"coordinator-secret", "product-secret", "backend-secret", "rdb-fetch-secret",
	} {
		if strings.Contains(out, secret) {
			t.Fatalf("config string leaked %q:\n%s", secret, out)
		}
	}
}

func TestOverviewJSONDoesNotExposeCoordinatorAuth(t *testing.T) {
	c := NewDefaultConfig()
	c.CoordinatorAuth = "coordinator-secret"

	b, err := json.Marshal(&Overview{Config: c.Redacted()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "coordinator-secret") {
		t.Fatalf("overview leaked coordinator auth: %s", b)
	}
	if strings.Contains(string(b), "coordinator_auth") {
		t.Fatalf("overview should not contain coordinator_auth field: %s", b)
	}
}
