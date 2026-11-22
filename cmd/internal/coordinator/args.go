// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package coordinator

import (
	"flag"
	"fmt"

	"github.com/CodisLabs/codis/cmd/internal/cli"
)

// Args identifies the selected coordinator backend.
type Args struct {
	Name    string
	Addr    string
	Auth    string
	HasAuth bool
}

// Flags holds the typed coordinator options shared by the Codis binaries.
type Flags struct {
	Zookeeper     cli.String
	ZookeeperAuth cli.String
	Filesystem    cli.String
	Consul        cli.String
	ConsulAuth    cli.String
}

type option struct {
	name     string
	addr     *cli.String
	addrFlag string
	auth     *cli.String
	authFlag string
}

func (f *Flags) options() []option {
	return []option{
		{name: "zookeeper", addr: &f.Zookeeper, addrFlag: "zookeeper", auth: &f.ZookeeperAuth, authFlag: "zookeeper-auth"},
		{name: "filesystem", addr: &f.Filesystem, addrFlag: "filesystem"},
		{name: "consul", addr: &f.Consul, addrFlag: "consul", auth: &f.ConsulAuth, authFlag: "consul-auth"},
	}
}

// FlagNames lists the option names registered by Bind.
func FlagNames() []string {
	return []string{
		"zookeeper", "zookeeper-auth",
		"filesystem",
		"consul", "consul-auth",
	}
}

// Bind registers the coordinator options on fs.
func (f *Flags) Bind(fs *flag.FlagSet) {
	f.BindAddresses(fs)
	fs.Var(&f.ZookeeperAuth, "zookeeper-auth", "zookeeper auth")
	fs.Var(&f.ConsulAuth, "consul-auth", "consul auth")
}

// BindAddresses registers only the coordinator address options. The dashboard
// deliberately does not accept the --*-auth flags on the command line.
func (f *Flags) BindAddresses(fs *flag.FlagSet) {
	fs.Var(&f.Zookeeper, "zookeeper", "zookeeper address")
	fs.Var(&f.Filesystem, "filesystem", "filesystem root")
	fs.Var(&f.Consul, "consul", "consul address")
}

// Parse validates the mutual exclusion and auth binding rules and returns
// the selected coordinator. ok is false when no coordinator was requested.
func (f *Flags) Parse() (Args, bool, error) {
	var selected *option
	for _, o := range f.options() {
		if o.auth != nil && o.auth.Provided && !o.addr.Provided {
			return Args{}, false, fmt.Errorf("option --%s requires --%s", o.authFlag, o.addrFlag)
		}
		if !o.addr.Provided {
			continue
		}
		if selected != nil {
			return Args{}, false, fmt.Errorf("options --%s and --%s are mutually exclusive", selected.addrFlag, o.addrFlag)
		}
		selected = &o
	}
	if selected == nil {
		return Args{}, false, nil
	}

	args := Args{Name: selected.name, Addr: selected.addr.Value}
	if selected.auth != nil && selected.auth.Provided {
		args.Auth = selected.auth.Value
		args.HasAuth = true
	}
	return args, true, nil
}
