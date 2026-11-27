// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/CodisLabs/codis/pkg/utils/errors"
)

const (
	// DefaultEndpoint is used when neither the command line, the connection
	// file nor the dashboard configuration names an endpoint.
	DefaultEndpoint = "http://127.0.0.1:2379"

	DefaultDialTimeout    = time.Second * 5
	DefaultRequestTimeout = time.Second * 5

	redactedSecret = "<redacted>"
)

// Secret carries a credential. It is deliberately not printable: every fmt
// verb (including %v, %+v and %#v) renders the placeholder, so a password can
// never leak into an error message, a log line or a formatted config dump.
type Secret string

func (s Secret) String() string   { return s.Redacted() }
func (s Secret) GoString() string { return s.Redacted() }
func (s Secret) Redacted() string {
	if s == "" {
		return ""
	}
	return redactedSecret
}

// Value returns the real secret, for handing over to the etcd client.
func (s Secret) Value() string { return string(s) }

// Config is the coordinator connection configuration, normally loaded from a
// standalone TOML file:
//
//	endpoints = ["http://127.0.0.1:2379"]
//	dial_timeout = "5s"
//	request_timeout = "5s"
//	username = ""
//	password = ""
//	ca_file = ""
//	cert_file = ""
//	key_file = ""
type Config struct {
	Endpoints      []string
	DialTimeout    time.Duration
	RequestTimeout time.Duration

	Username string
	Password Secret

	CAFile   string
	CertFile string
	KeyFile  string

	// baseDir is the directory of the connection file; relative TLS file
	// paths are resolved against it. Empty for programmatic configs.
	baseDir string
}

// fileConfig mirrors Config for TOML decoding. Durations are strings so their
// parse failures report the field name instead of an unrelated value.
type fileConfig struct {
	Endpoints      []string `toml:"endpoints"`
	DialTimeout    string   `toml:"dial_timeout"`
	RequestTimeout string   `toml:"request_timeout"`

	Username string `toml:"username"`
	Password string `toml:"password"`

	CAFile   string `toml:"ca_file"`
	CertFile string `toml:"cert_file"`
	KeyFile  string `toml:"key_file"`
}

// LoadConfigFile decodes the standalone etcd connection file and validates it.
// Unknown top-level fields are rejected. Error texts only mention field names
// and reasons, never field values.
func LoadConfigFile(path string) (*Config, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Errorf("invalid etcd config file: file is not readable")
	}
	var fc fileConfig
	md, err := toml.Decode(string(blob), &fc)
	if err != nil {
		// The raw toml error may echo offending values, so only the position
		// and the field name are reported.
		var perr toml.ParseError
		if stderrors.As(err, &perr) {
			if perr.LastKey != "" {
				return nil, errors.Errorf("invalid etcd config file: malformed toml at line %d for field %q", perr.Position.Line, perr.LastKey)
			}
			return nil, errors.Errorf("invalid etcd config file: malformed toml at line %d", perr.Position.Line)
		}
		return nil, errors.New("invalid etcd config file: malformed toml")
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, errors.Errorf("invalid etcd config file: unknown field %q", undecoded[0].String())
	}

	c := &Config{
		Endpoints:      fc.Endpoints,
		DialTimeout:    DefaultDialTimeout,
		RequestTimeout: DefaultRequestTimeout,
		Username:       fc.Username,
		Password:       Secret(fc.Password),
		CAFile:         fc.CAFile,
		CertFile:       fc.CertFile,
		KeyFile:        fc.KeyFile,
		baseDir:        filepath.Dir(path),
	}
	if fc.DialTimeout != "" {
		d, err := parseDurationField("dial_timeout", fc.DialTimeout)
		if err != nil {
			return nil, err
		}
		c.DialTimeout = d
	}
	if fc.RequestTimeout != "" {
		d, err := parseDurationField("request_timeout", fc.RequestTimeout)
		if err != nil {
			return nil, err
		}
		c.RequestTimeout = d
	}
	if endpoints, err := checkEndpoints(c.Endpoints, false); err != nil {
		return nil, err
	} else {
		c.Endpoints = endpoints
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func parseDurationField(name string, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, errors.Errorf("invalid %s: not a duration", name)
	}
	if d <= 0 {
		return 0, errors.Errorf("invalid %s: must be greater than zero", name)
	}
	return d, nil
}

// ParseEndpoints turns the comma separated --etcd flag value into endpoints.
func ParseEndpoints(s string) ([]string, error) {
	var list []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, errors.New("invalid endpoints: empty entry")
		}
		list = append(list, item)
	}
	return checkEndpoints(list, true)
}

// ResolveEndpoints applies the address precedence: explicit --etcd value,
// then the connection file endpoints, then the dashboard coordinator_addr,
// then http://127.0.0.1:2379. An empty explicit value means "not given"; use
// ResolveEndpointsFlag when a flag may be explicitly set to an empty value.
func ResolveEndpoints(cli string, file []string, fallback string) ([]string, error) {
	return ResolveEndpointsFlag(cli, false, file, fallback)
}

// ResolveEndpointsFlag is ResolveEndpoints for callers that know whether the
// flag was given. cliSet with an empty value is an error.
func ResolveEndpointsFlag(cliValue string, cliSet bool, file []string, fallback string) ([]string, error) {
	if cliSet || strings.TrimSpace(cliValue) != "" {
		if strings.TrimSpace(cliValue) == "" {
			return nil, errors.New("invalid endpoints: empty address")
		}
		return ParseEndpoints(cliValue)
	}
	if len(file) > 0 {
		return checkEndpoints(file, true)
	}
	if strings.TrimSpace(fallback) != "" {
		return checkEndpoints([]string{strings.TrimSpace(fallback)}, true)
	}
	return []string{DefaultEndpoint}, nil
}

// checkEndpoints validates and normalizes endpoints. Endpoints must be
// http(s) URLs of one cluster: one scheme only, no embedded credentials, no
// path, no query. When requireNonEmpty is false an empty list is accepted (the
// connection file may leave endpoints to the dashboard fallback).
func checkEndpoints(list []string, requireNonEmpty bool) ([]string, error) {
	if len(list) == 0 {
		if requireNonEmpty {
			return nil, errors.New("invalid endpoints: at least one endpoint is required")
		}
		return nil, nil
	}
	results := make([]string, 0, len(list))
	scheme := ""
	for _, raw := range list {
		ep, epScheme, err := validateEndpoint(raw)
		if err != nil {
			return nil, err
		}
		if scheme == "" {
			scheme = epScheme
		} else if scheme != epScheme {
			return nil, errors.New("invalid endpoints: cannot mix http and https")
		}
		results = append(results, ep)
	}
	return results, nil
}

func validateEndpoint(raw string) (string, string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", errors.New("invalid endpoints: empty entry")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", "", errors.New("invalid endpoints: malformed URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", errors.New("invalid endpoints: scheme must be http or https")
	}
	if u.User != nil {
		return "", "", errors.New("invalid endpoints: credentials are not allowed in the URL")
	}
	if u.Host == "" {
		return "", "", errors.New("invalid endpoints: missing host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", "", errors.New("invalid endpoints: query and fragment are not allowed")
	}
	if p := strings.TrimSuffix(u.Path, "/"); p != "" {
		return "", "", errors.New("invalid endpoints: path is not allowed")
	}
	return u.Scheme + "://" + u.Host, u.Scheme, nil
}

// Validate checks the configuration. Empty endpoints are tolerated (the
// dashboard fallback may fill them in); New requires at least one endpoint.
func (c *Config) Validate() error {
	eps, err := checkEndpoints(c.Endpoints, false)
	if err != nil {
		return err
	}
	if len(eps) > 0 && c.hasTLSFiles() && !isAllHTTPS(eps) {
		return errors.New("invalid endpoints: tls config requires https endpoints")
	}
	if c.DialTimeout < 0 {
		return errors.New("invalid dial_timeout: must be greater than zero")
	}
	if c.RequestTimeout < 0 {
		return errors.New("invalid request_timeout: must be greater than zero")
	}
	if (c.CertFile == "") != (c.KeyFile == "") {
		return errors.New("invalid cert_file/key_file: must be set together")
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("invalid username/password: must be set together")
	}
	if _, err := c.TLSConfig(); err != nil {
		return err
	}
	return nil
}

// applyDefaults fills the timeouts that were not configured.
func (c *Config) applyDefaults() {
	if c.DialTimeout == 0 {
		c.DialTimeout = DefaultDialTimeout
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = DefaultRequestTimeout
	}
}

func (c *Config) hasTLSFiles() bool {
	return c.CAFile != "" || c.CertFile != "" || c.KeyFile != ""
}

func isAllHTTPS(eps []string) bool {
	for _, ep := range eps {
		if !strings.HasPrefix(ep, "https://") {
			return false
		}
	}
	return true
}

// TLSConfig builds the client TLS configuration. It returns nil for plaintext
// endpoints without TLS files. The returned config verifies the server
// hostname (InsecureSkipVerify is never set); https endpoints without an
// explicit ca_file use the system roots.
func (c *Config) TLSConfig() (*tls.Config, error) {
	if !c.hasTLSFiles() && !isAllHTTPS(c.Endpoints) {
		return nil, nil
	}
	tc := &tls.Config{}
	if c.CAFile != "" {
		pool, err := loadCertPool(c.resolve(c.CAFile))
		if err != nil {
			return nil, errors.Errorf("invalid ca_file: %w", err)
		}
		tc.RootCAs = pool
	}
	if c.CertFile != "" {
		pair, err := tls.LoadX509KeyPair(c.resolve(c.CertFile), c.resolve(c.KeyFile))
		if err != nil {
			return nil, errors.Errorf("invalid cert_file/key_file: %w", err)
		}
		tc.Certificates = []tls.Certificate{pair}
	}
	return tc, nil
}

func (c *Config) resolve(path string) string {
	if path == "" || filepath.IsAbs(path) || c.baseDir == "" {
		return path
	}
	return filepath.Join(c.baseDir, path)
}

func loadCertPool(path string) (*x509.CertPool, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("file is not readable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(blob) {
		return nil, errors.New("no PEM certificate found")
	}
	return pool, nil
}

// Redacted returns a copy of the config with the password replaced, intended
// for logs and error messages.
func (c *Config) Redacted() *Config {
	if c == nil {
		return nil
	}
	x := *c
	x.Endpoints = append([]string(nil), c.Endpoints...)
	if x.Password != "" {
		x.Password = redactedSecret
	}
	return &x
}

// String renders the config for logs; the password is always redacted.
func (c *Config) String() string {
	if c == nil {
		return "<nil>"
	}
	x := c.Redacted()
	return fmt.Sprintf("etcd.Config{Endpoints: %v, DialTimeout: %s, RequestTimeout: %s, Username: %q, Password: %s, CAFile: %q, CertFile: %q, KeyFile: %q}",
		x.Endpoints, x.DialTimeout, x.RequestTimeout, x.Username, x.Password, x.CAFile, x.CertFile, x.KeyFile)
}
