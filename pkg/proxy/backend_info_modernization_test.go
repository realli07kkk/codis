// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"strings"
	"testing"
)

// legacyParseInfo is the map-based INFO parse the keepalive used before the
// byte-wise parser. It is the reference for the fidelity tests below.
func legacyParseInfo(value []byte) map[string]string {
	var info = make(map[string]string)
	for _, line := range strings.Split(string(value), "\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) != 2 {
			continue
		}
		if key := strings.TrimSpace(kv[0]); key != "" {
			info[key] = strings.TrimSpace(kv[1])
		}
	}
	return info
}

func TestParseKeepAliveInfoMatchesLegacyParse(t *testing.T) {
	payloads := map[string]string{
		"first line":           "master_link_status:down\r\nloading:0\r\n",
		"later lines":          "redis_version:7.4.0\r\nrole:slave\r\nmaster_link_status:up\r\nloading:1\r\n",
		"empty payload":        "",
		"missing keys":         "redis_version:7.4.0\r\nrole:slave\r\n",
		"missing last line":    "role:slave\r\nmaster_link_status:down",
		"empty values":         "master_link_status:\r\nloading:\r\n",
		"colon in values":      "master_link_status:down:extra\r\nloading:1:2\r\n",
		"padded key and value": "  master_link_status  :  down  \r\n loading : 0 \r\n",
		"empty key":            ":ignored\r\nmaster_link_status:up\r\n",
		"line without colon":   "garbage-line\r\nmaster_link_status:down\r\n",
		"blank lines":          "\r\n\n\nloading:1\r\n",
		"last value wins":      "loading:1\r\nloading:0\r\n",
		"last value wins rev":  "loading:0\r\nloading:1\r\n",
		"only separator":       ":\r\n:\r\n",
		"unix newlines":        "role:master\nloading:1\nmaster_link_status:down\n",
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			legacy := legacyParseInfo([]byte(payload))
			linkStatus, loading := parseKeepAliveInfo([]byte(payload))
			if got, want := linkStatus, legacy["master_link_status"]; got != want {
				t.Fatalf("master_link_status = %q, want %q", got, want)
			}
			if got, want := loading, legacy["loading"]; got != want {
				t.Fatalf("loading = %q, want %q", got, want)
			}
		})
	}
}

func TestParseKeepAliveInfoCompleteInfoPayload(t *testing.T) {
	payload := []byte("# Replication\r\n" +
		"role:slave\r\n" +
		"master_host:10.0.0.1\r\n" +
		"master_link_status:down\r\n" +
		"master_link_down_since_seconds:12\r\n" +
		"\r\n" +
		"# Persistence\r\n" +
		"loading:1\r\n" +
		"async_loading:0\r\n")

	linkStatus, loading := parseKeepAliveInfo(payload)
	if linkStatus != "down" {
		t.Fatalf("master_link_status = %q, want down", linkStatus)
	}
	if loading != "1" {
		t.Fatalf("loading = %q, want 1", loading)
	}
}

func TestParseKeepAliveInfoMissingKeysStayEmpty(t *testing.T) {
	linkStatus, loading := parseKeepAliveInfo([]byte("role:slave\r\nconnected_slaves:0\r\n"))
	if linkStatus != "" {
		t.Fatalf("master_link_status = %q, want empty", linkStatus)
	}
	if loading != "" {
		t.Fatalf("loading = %q, want empty", loading)
	}
	if linkStatus, loading := parseKeepAliveInfo(nil); linkStatus != "" || loading != "" {
		t.Fatalf("parseKeepAliveInfo(nil) = (%q, %q), want empty values", linkStatus, loading)
	}
}

// TestParseKeepAliveInfoAllocationFree is the reason the parser walks bytes: a
// stale-connection probe must not build a map of strings. Only the two watched
// keys may allocate.
func TestParseKeepAliveInfoAllocationFree(t *testing.T) {
	payload := []byte("redis_version:7.4.0\r\nrole:slave\r\nconnected_slaves:2\r\n" +
		"master_repl_offset:1234\r\nused_memory_human:1.5M\r\n")

	allocs := testing.AllocsPerRun(1000, func() {
		linkStatus, loading := parseKeepAliveInfo(payload)
		if linkStatus != "" || loading != "" {
			panic("unexpected keepalive fields")
		}
	})
	if allocs != 0 {
		t.Fatalf("parseKeepAliveInfo allocated %v objects for an unrelated INFO payload, want 0", allocs)
	}
}

var keepAliveInfoSink string

var keepAliveInfoBenchPayload = []byte("redis_version:7.4.0\r\n" +
	"role:slave\r\n" +
	"master_host:10.0.0.1\r\n" +
	"master_link_status:up\r\n" +
	"master_link_down_since_seconds:0\r\n" +
	"connected_slaves:2\r\n" +
	"loading:0\r\n" +
	"async_loading:0\r\n")

// BenchmarkParseKeepAliveInfo compares the byte-wise parser with the previous
// map-based parse over a typical INFO payload.
func BenchmarkParseKeepAliveInfo(b *testing.B) {
	payload := keepAliveInfoBenchPayload
	b.ReportAllocs()
	for b.Loop() {
		linkStatus, loading := parseKeepAliveInfo(payload)
		keepAliveInfoSink = linkStatus + loading
	}
}

func BenchmarkParseKeepAliveInfoLegacyMap(b *testing.B) {
	payload := keepAliveInfoBenchPayload
	b.ReportAllocs()
	for b.Loop() {
		info := legacyParseInfo(payload)
		keepAliveInfoSink = info["master_link_status"] + info["loading"]
	}
}
