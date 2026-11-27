// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package coordinator

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/CodisLabs/codis/cmd/internal/cli"
	"github.com/CodisLabs/codis/pkg/models/etcd"
)

// DeprecatedCoordinatorSHA is the last commit that shipped the zookeeper,
// filesystem and consul coordinators. Errors for the removed options point at
// it so an operator can find the previous release.
const DeprecatedCoordinatorSHA = "c635ade0424a1448fb251f32b88d64ca7cf9a555"

// deprecatedOptions are the removed backend options. They stay registered so
// an old command line gets an actionable error instead of "flag provided but
// not defined". Their values are dropped and never reach an error message.
var deprecatedOptions = []string{
	"zookeeper", "zookeeper-auth",
	"filesystem",
	"consul", "consul-auth",
}

// Args is the coordinator connection selection shared by the Codis binaries.
type Args struct {
	EndpointsFlag string // raw --etcd value, comma separated URLs
	ConfigFile    string // --etcd-config value
	HasEndpoints  bool   // whether --etcd was given
	HasConfigFile bool   // whether --etcd-config was given
}

// Value is a string option that accepts an empty argument and records whether
// it appeared. cli.String refuses empty values, which would turn an explicit
// --etcd= into an unrelated flag error instead of the documented empty address
// rejection.
type Value struct {
	Value    string
	Provided bool
}

// String implements flag.Value.
func (v *Value) String() string { return v.Value }

// Set implements flag.Value. Any value, including the empty one, is kept so
// Build can reject it with the right reason.
func (v *Value) Set(s string) error {
	v.Value, v.Provided = s, true
	return nil
}

// Flags holds the connection options shared by the Codis binaries.
type Flags struct {
	Etcd       Value
	EtcdConfig Value

	// Deprecated: these fields are never bound and never populated. They are
	// kept only so the entry packages in cmd/* keep compiling until they are
	// rewired; the options themselves are registered by Bind as rejected ones.
	Zookeeper     cli.String
	ZookeeperAuth cli.String
	Filesystem    cli.String
	Consul        cli.String
	ConsulAuth    cli.String

	deprecated []string
}

// deprecatedOption is the flag.Value behind the removed options. It records
// the option name and drops the value so it can never be echoed back.
type deprecatedOption struct {
	flags *Flags
	name  string
}

func (o *deprecatedOption) String() string { return "" }

func (o *deprecatedOption) Set(string) error {
	o.flags.deprecated = append(o.flags.deprecated, o.name)
	return nil
}

// FlagNames lists the option names registered by Bind.
func FlagNames() []string {
	return []string{"etcd", "etcd-config"}
}

// Bind registers the coordinator options on fs. It is the historical entry
// point and registers the same set as BindAddresses.
func (f *Flags) Bind(fs *flag.FlagSet) {
	f.BindAddresses(fs)
}

// BindAddresses registers the shared connection options. Credentials and TLS
// material come from the --etcd-config file, so every component accepts the
// same set. The removed backend options are registered as well, only to be
// rejected by Parse.
func (f *Flags) BindAddresses(fs *flag.FlagSet) {
	fs.Var(&f.Etcd, "etcd", "etcd endpoints, comma separated http(s) URLs")
	fs.Var(&f.EtcdConfig, "etcd-config", "etcd connection config file")
	for _, name := range deprecatedOptions {
		fs.Var(&deprecatedOption{flags: f, name: name}, name, "deprecated, no longer supported")
	}
}

// Parse returns the requested connection options. ok reports whether at least
// one of them was given. Any removed backend option is rejected by name and by
// the last supporting commit; the supplied values never appear in the error.
func (f *Flags) Parse() (Args, bool, error) {
	if len(f.deprecated) > 0 {
		return Args{}, false, fmt.Errorf("flag --%s is no longer supported; zookeeper/filesystem/consul coordinators were last supported at %s, use --etcd or --etcd-config",
			f.deprecated[0], DeprecatedCoordinatorSHA)
	}
	args := Args{
		EndpointsFlag: f.Etcd.Value,
		ConfigFile:    f.EtcdConfig.Value,
		HasEndpoints:  f.Etcd.Provided,
		HasConfigFile: f.EtcdConfig.Provided,
	}
	return args, args.HasEndpoints || args.HasConfigFile, nil
}

// Build resolves the connection configuration without connecting. The
// connection file, when given, is loaded and validated first; endpoints then
// come from --etcd, the file, fallbackAddr or the built-in default, in that
// order, while every other connection field comes from the file or its
// default. fallbackAddr is the dashboard coordinator_addr and may hold several
// comma separated URLs. All errors are returned before a connection is
// attempted; nothing silently falls back to another address.
func (a Args) Build(fallbackAddr string) (*etcd.Config, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	endpoints, err := a.resolveEndpoints(cfg.Endpoints, fallbackAddr)
	if err != nil {
		return nil, err
	}
	cfg.Endpoints = endpoints
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// NewClient builds the configuration and opens the client. Every configuration
// error is reported before a connection is attempted.
func (a Args) NewClient(fallbackAddr string) (*etcd.Client, error) {
	cfg, err := a.Build(fallbackAddr)
	if err != nil {
		return nil, err
	}
	return etcd.New(cfg)
}

// loadConfig returns the connection file contents or the built-in defaults.
func (a Args) loadConfig() (*etcd.Config, error) {
	if !a.HasConfigFile && a.ConfigFile == "" {
		return &etcd.Config{
			DialTimeout:    etcd.DefaultDialTimeout,
			RequestTimeout: etcd.DefaultRequestTimeout,
		}, nil
	}
	if strings.TrimSpace(a.ConfigFile) == "" {
		return nil, errors.New("invalid --etcd-config: empty file path")
	}
	return etcd.LoadConfigFile(a.ConfigFile)
}

// resolveEndpoints applies the documented address precedence. The connection
// file and the default are resolved by pkg/models/etcd so the endpoint rules
// live in one place; the dashboard fallback is parsed with the same rules.
func (a Args) resolveEndpoints(file []string, fallbackAddr string) ([]string, error) {
	if a.HasEndpoints || strings.TrimSpace(a.EndpointsFlag) != "" {
		return etcd.ResolveEndpointsFlag(a.EndpointsFlag, a.HasEndpoints, nil, "")
	}
	if len(file) > 0 {
		return etcd.ResolveEndpointsFlag("", false, file, "")
	}
	if strings.TrimSpace(fallbackAddr) != "" {
		return etcd.ParseEndpoints(fallbackAddr)
	}
	return etcd.ResolveEndpointsFlag("", false, nil, "")
}
