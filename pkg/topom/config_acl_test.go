// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import "testing"

func TestConfigBackendAuthIdentity(t *testing.T) {
	config := NewDefaultConfig()
	config.ProductAuth = "product-secret"
	auth := config.BackendAuthIdentity()
	if auth.Username != "" || auth.Password != "product-secret" {
		t.Fatalf("default backend auth = %+v", auth)
	}

	config.BackendAuthUsername = "svc"
	config.BackendAuthPassword = "backend-secret"
	auth = config.BackendAuthIdentity()
	if auth.Username != "svc" || auth.Password != "backend-secret" {
		t.Fatalf("user backend auth = %+v", auth)
	}
}

func TestConfigRejectsBackendAuthUsernameWithoutPassword(t *testing.T) {
	config := NewDefaultConfig()
	config.BackendAuthUsername = "svc"
	config.BackendAuthPassword = ""
	if err := config.Validate(); err == nil {
		t.Fatal("expected invalid backend_auth_password")
	}
}
