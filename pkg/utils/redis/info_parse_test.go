// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import (
	"strings"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
)

func TestClientInfoParsing(t *testing.T) {
	const info = "# Server\r\n" +
		"redis_version:8.0.0\r\n" +
		"no colon in this line\r\n" +
		":empty-key-ignored\r\n" +
		"  padded_key  :  padded value  \r\n" +
		"\r\n" +
		"dup:first\r\n" +
		"dup:second\r\n"
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if strings.ToUpper(args[0]) == "INFO" && len(args) == 1 {
			return redistest.Bulk(info)
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got, err := c.Info()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"redis_version": "8.0.0",
		"padded_key":    "padded value",
		"dup":           "second",
	}
	if len(got) != len(want) {
		t.Fatalf("Info() = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("Info()[%q] = %q, want %q (all = %v)", key, got[key], value, got)
		}
	}
}

func TestClientInfoKeySpaceParsing(t *testing.T) {
	const info = "# Keyspace\r\n" +
		"db0:keys=1,expires=0,avg_ttl=0\r\n" +
		"db3:keys=0\r\n" +
		"notdb:ignored\r\n" +
		"\r\n"
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if len(args) == 2 && strings.ToUpper(args[0]) == "INFO" && args[1] == "keyspace" {
			return redistest.Bulk(info)
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got, err := c.InfoKeySpace()
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{
		0: "keys=1,expires=0,avg_ttl=0",
		3: "keys=0",
	}
	if len(got) != len(want) {
		t.Fatalf("InfoKeySpace() = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("InfoKeySpace()[%d] = %q, want %q (all = %v)", key, got[key], value, got)
		}
	}
}

func TestClientInfoKeySpaceBadDBIndexReturnsError(t *testing.T) {
	s := newRedisClientTestServer(t, func(args []string) *redistest.Resp {
		if len(args) == 2 && strings.ToUpper(args[0]) == "INFO" && args[1] == "keyspace" {
			return redistest.Bulk("dbx:keys=1\r\n")
		}
		t.Fatalf("unexpected command: %v", args)
		return nil
	})

	c, err := NewClientNoAuth(s.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.InfoKeySpace(); err == nil {
		t.Fatal("InfoKeySpace() with a non-numeric db index should fail")
	}
}
