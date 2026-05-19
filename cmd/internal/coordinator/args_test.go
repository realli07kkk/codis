// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package coordinator

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		want Args
		ok   bool
	}{
		{
			name: "zookeeper with auth",
			args: map[string]interface{}{
				"--zookeeper":      "127.0.0.1:2181",
				"--zookeeper-auth": "user:pass",
			},
			want: Args{Name: "zookeeper", Addr: "127.0.0.1:2181", Auth: "user:pass", HasAuth: true},
			ok:   true,
		},
		{
			name: "etcd with auth",
			args: map[string]interface{}{
				"--etcd":      "127.0.0.1:2379",
				"--etcd-auth": "user:pass",
			},
			want: Args{Name: "etcd", Addr: "127.0.0.1:2379", Auth: "user:pass", HasAuth: true},
			ok:   true,
		},
		{
			name: "filesystem without auth",
			args: map[string]interface{}{
				"--filesystem": "/tmp/codis",
			},
			want: Args{Name: "filesystem", Addr: "/tmp/codis"},
			ok:   true,
		},
		{
			name: "consul with auth",
			args: map[string]interface{}{
				"--consul":      "127.0.0.1:8500",
				"--consul-auth": "token",
			},
			want: Args{Name: "consul", Addr: "127.0.0.1:8500", Auth: "token", HasAuth: true},
			ok:   true,
		},
		{
			name: "consul without auth",
			args: map[string]interface{}{
				"--consul": "127.0.0.1:8500",
			},
			want: Args{Name: "consul", Addr: "127.0.0.1:8500"},
			ok:   true,
		},
		{
			name: "missing coordinator",
			args: map[string]interface{}{},
			want: Args{},
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Parse(tt.args)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("args = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseUsesExistingCoordinatorPrecedence(t *testing.T) {
	args, ok := Parse(map[string]interface{}{
		"--zookeeper": "zk",
		"--etcd":      "etcd",
		"--consul":    "consul",
	})
	if !ok {
		t.Fatal("expected coordinator")
	}
	if args.Name != "zookeeper" || args.Addr != "zk" {
		t.Fatalf("args = %+v, want zookeeper first", args)
	}
}
