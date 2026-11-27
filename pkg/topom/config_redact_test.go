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
	c.ProductAuth = "product-secret"
	c.BackendAuthPassword = "backend-secret"
	c.RDBAnalysisRemoteFetchAuth = "rdb-fetch-secret"

	out := c.String()
	for _, secret := range []string{
		"product-secret", "backend-secret", "rdb-fetch-secret",
	} {
		if strings.Contains(out, secret) {
			t.Fatalf("config string leaked %q:\n%s", secret, out)
		}
	}
}

// TestOverviewJSONHasNoCoordinatorAuthField pins that the removed
// coordinator_auth option never resurfaces in the dashboard overview JSON.
func TestOverviewJSONHasNoCoordinatorAuthField(t *testing.T) {
	c := NewDefaultConfig()

	b, err := json.Marshal(&Overview{Config: c.Redacted()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "coordinator_auth") {
		t.Fatalf("overview should not contain coordinator_auth field: %s", b)
	}
}
