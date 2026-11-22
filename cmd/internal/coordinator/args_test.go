// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package coordinator

import (
	"flag"
	"testing"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

func testString(v string) cli.String {
	return cli.String{Value: v, Provided: true}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		flags Flags
		want  Args
		ok    bool
	}{
		{
			name: "zookeeper with auth",
			flags: Flags{
				Zookeeper:     testString("127.0.0.1:2181"),
				ZookeeperAuth: testString("user:pass"),
			},
			want: Args{Name: "zookeeper", Addr: "127.0.0.1:2181", Auth: "user:pass", HasAuth: true},
			ok:   true,
		},
		{
			name: "filesystem without auth",
			flags: Flags{
				Filesystem: testString("/tmp/codis"),
			},
			want: Args{Name: "filesystem", Addr: "/tmp/codis"},
			ok:   true,
		},
		{
			name: "consul with auth",
			flags: Flags{
				Consul:     testString("127.0.0.1:8500"),
				ConsulAuth: testString("token"),
			},
			want: Args{Name: "consul", Addr: "127.0.0.1:8500", Auth: "token", HasAuth: true},
			ok:   true,
		},
		{
			name: "consul without auth",
			flags: Flags{
				Consul: testString("127.0.0.1:8500"),
			},
			want: Args{Name: "consul", Addr: "127.0.0.1:8500"},
			ok:   true,
		},
		{
			name: "missing coordinator",
			want: Args{},
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := tt.flags.Parse()
			if err != nil {
				t.Fatalf("Parse() failed: %v", err)
			}
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("args = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestParseAuthWithoutCoordinator pins that an auth flag alone is not a
// coordinator: the address flag still selects the backend and a stray auth
// flag is rejected.
func TestParseAuthWithoutCoordinator(t *testing.T) {
	for _, flags := range []Flags{
		{ZookeeperAuth: testString("user:pass")},
		{ConsulAuth: testString("token")},
		{ZookeeperAuth: testString("user:pass"), ConsulAuth: testString("token")},
	} {
		if got, ok, err := flags.Parse(); err == nil || ok {
			t.Fatalf("Parse() = %+v, %v, %v; want an auth binding error", got, ok, err)
		}
	}
}

// TestParseBindsAuthToOwningCoordinator pins that each auth flag is only read
// together with its own address flag; a stray --consul-auth must not leak into a
// zookeeper result (and vice versa).
func TestParseBindsAuthToOwningCoordinator(t *testing.T) {
	flags := Flags{
		Zookeeper:     testString("zk"),
		ZookeeperAuth: testString("user:pass"),
	}
	args, ok, err := flags.Parse()
	if err != nil || !ok {
		t.Fatalf("Parse() = %+v, %v, %v; want zookeeper", args, ok, err)
	}
	want := Args{Name: "zookeeper", Addr: "zk", Auth: "user:pass", HasAuth: true}
	if args != want {
		t.Fatalf("args = %+v, want %+v", args, want)
	}

	for _, stray := range []Flags{
		{Zookeeper: testString("zk"), ConsulAuth: testString("token")},
		{Consul: testString("consul"), ZookeeperAuth: testString("user:pass")},
	} {
		if got, ok, err := stray.Parse(); err == nil || ok {
			t.Fatalf("Parse() = %+v, %v, %v; want a stray auth error", got, ok, err)
		}
	}
}

// TestParseRejectsMultipleCoordinators pins the three-way mutual exclusion.
func TestParseRejectsMultipleCoordinators(t *testing.T) {
	flags := Flags{
		Zookeeper:  testString("zk"),
		Filesystem: testString("/tmp/codis"),
		Consul:     testString("consul"),
	}
	if got, ok, err := flags.Parse(); err == nil || ok {
		t.Fatalf("Parse() = %+v, %v, %v; want a mutual exclusion error", got, ok, err)
	}
}

func TestBindRejectsEtcdFlags(t *testing.T) {
	for _, addressesOnly := range []bool{false, true} {
		for _, arg := range []string{"--etcd=127.0.0.1:2379", "--etcd-auth=user:pass"} {
			var flags Flags
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			if addressesOnly {
				flags.BindAddresses(fs)
			} else {
				flags.Bind(fs)
			}
			if err := fs.Parse([]string{arg}); err == nil {
				t.Fatalf("Parse(%q) with addressesOnly=%v succeeded, want unknown flag error", arg, addressesOnly)
			}
		}
	}
}
