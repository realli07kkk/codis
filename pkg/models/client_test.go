// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import (
	"strings"
	"testing"
	"time"
)

func TestNewClientRejectsEtcd(t *testing.T) {
	client, err := NewClient("etcd", "127.0.0.1:2379", "", time.Second)
	if client != nil {
		client.Close()
		t.Fatal("NewClient(etcd) returned a client, want nil")
	}
	if err == nil || !strings.Contains(err.Error(), "invalid coordinator name = etcd") {
		t.Fatalf("NewClient(etcd) error = %v, want invalid coordinator error", err)
	}
}
