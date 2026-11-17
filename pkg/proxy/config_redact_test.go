// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigStringRedactsCredentials(t *testing.T) {
	c := NewDefaultConfig()
	c.JodisAuth = "jodis-secret"
	c.ProductAuth = "product-secret"
	c.SessionAuth = "session-secret"
	c.BackendAuthPassword = "backend-secret"
	c.MetricsReportInfluxdbPassword = "influx-secret"

	out := c.String()
	for _, secret := range []string{
		"jodis-secret", "product-secret", "session-secret",
		"backend-secret", "influx-secret",
	} {
		if strings.Contains(out, secret) {
			t.Fatalf("config string leaked %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, redactedSecret) {
		t.Fatalf("config string should contain %q:\n%s", redactedSecret, out)
	}
}

func TestConfigJSONDoesNotExposeCredentials(t *testing.T) {
	c := NewDefaultConfig()
	c.JodisAuth = "jodis-secret"
	c.ProductAuth = "product-secret"
	c.SessionAuth = "session-secret"
	c.BackendAuthPassword = "backend-secret"
	c.MetricsReportInfluxdbPassword = "influx-secret"

	b, err := json.Marshal(c.Redacted())
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, secret := range []string{
		"jodis-secret", "product-secret", "session-secret",
		"backend-secret", "influx-secret",
	} {
		if strings.Contains(out, secret) {
			t.Fatalf("config JSON leaked %q: %s", secret, out)
		}
	}
}
