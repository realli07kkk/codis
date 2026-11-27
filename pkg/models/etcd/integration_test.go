// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// etcdBinary returns the etcd server binary named by CODIS_ETCD_SERVER_BINARY
// or skips the integration test. The binary is only executed, never modified.
func etcdBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("CODIS_ETCD_SERVER_BINARY")
	if bin == "" {
		t.Skip("CODIS_ETCD_SERVER_BINARY is not set; skipping etcd integration test")
	}
	info, err := os.Stat(bin)
	if err != nil || info.IsDir() {
		t.Skipf("CODIS_ETCD_SERVER_BINARY = %q is not an executable file", bin)
	}
	return bin
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)
	if err := ln.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return port
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type testServer struct {
	endpoint string
	cmd      *exec.Cmd
	logs     *lockedBuffer
	once     sync.Once
}

// startEtcd launches a single-member etcd on random free ports in a temporary
// data directory. The cleanup kills the process, reaps it and the temporary
// directory removal is handled by t.TempDir.
func startEtcd(t *testing.T, scheme string, extraArgs ...string) *testServer {
	t.Helper()
	bin := etcdBinary(t)
	clientPort := freePort(t)
	peerPort := freePort(t)
	dir := t.TempDir()
	endpoint := fmt.Sprintf("%s://127.0.0.1:%s", scheme, clientPort)
	peerEndpoint := fmt.Sprintf("http://127.0.0.1:%s", peerPort)
	args := []string{
		"--data-dir", filepath.Join(dir, "data"),
		"--listen-client-urls", endpoint,
		"--advertise-client-urls", endpoint,
		"--listen-peer-urls", peerEndpoint,
		"--initial-advertise-peer-urls", peerEndpoint,
		"--initial-cluster", "default=" + peerEndpoint,
		"--initial-cluster-state", "new",
		"--initial-cluster-token", "codis-etcd-test-" + clientPort,
		"--log-level", "error",
	}
	args = append(args, extraArgs...)

	logs := &lockedBuffer{}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start etcd: %v", err)
	}
	s := &testServer{endpoint: endpoint, cmd: cmd, logs: logs}
	t.Cleanup(s.stop)
	return s
}

func (s *testServer) stop() {
	s.once.Do(func() {
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		_ = s.cmd.Wait()
	})
}

// waitReady polls the endpoint until it accepts connections, at most 10s.
func (s *testServer) waitReady(t *testing.T, tlsCfg *tls.Config) {
	t.Helper()
	u, err := url.Parse(s.endpoint)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ready := false
		if tlsCfg != nil {
			dialer := &tls.Dialer{Config: tlsCfg.Clone(), NetDialer: &net.Dialer{Timeout: time.Second}}
			if conn, err := dialer.DialContext(context.Background(), "tcp", u.Host); err == nil {
				_ = conn.Close()
				ready = true
			}
		} else {
			if conn, err := net.DialTimeout("tcp", u.Host, time.Second); err == nil {
				_ = conn.Close()
				ready = true
			}
		}
		if ready {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("etcd at %s not ready within 10s; log:\n%s", s.endpoint, s.logs.String())
}

func newTestClient(t *testing.T, cfg *Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func clientTLSConfig(t *testing.T, caFile, certFile, keyFile string) *tls.Config {
	t.Helper()
	blob, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("read ca: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(blob) {
		t.Fatalf("append ca: no certificate found")
	}
	cfg := &tls.Config{RootCAs: pool}
	if certFile != "" {
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			t.Fatalf("load client key pair: %v", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return cfg
}

func endpointHost(t *testing.T, endpoint string) string {
	t.Helper()
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	return u.Host
}

func TestIntegrationClient(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})

	if err := c.Create("/codis3/foo/a", []byte("v1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := c.Create("/codis3/foo/a", []byte("v2"))
	if err == nil {
		t.Fatal("duplicate create succeeded")
	}
	if !stderrors.Is(err, ErrExist) {
		t.Fatalf("duplicate create error = %v, want ErrExist", err)
	}
	if got := Classify(err); got != KindExist {
		t.Fatalf("duplicate create classified as %s, want Exist", got)
	}

	if err := c.Update("/codis3/foo/a", []byte("v2")); err != nil {
		t.Fatalf("update: %v", err)
	}
	b, err := c.Read("/codis3/foo/a", true)
	if err != nil || string(b) != "v2" {
		t.Fatalf("read after update = %q, %v", b, err)
	}

	if b, err := c.Read("/codis3/missing", false); err != nil || b != nil {
		t.Fatalf("read missing must=false = %q, %v", b, err)
	}
	if _, err := c.Read("/codis3/missing", true); !stderrors.Is(err, ErrNotExist) {
		t.Fatalf("read missing must=true = %v, want ErrNotExist", err)
	} else if got := Classify(err); got != KindNotFound {
		t.Fatalf("read missing classified as %s, want NotFound", got)
	}
	if err := c.Delete("/codis3/missing"); err != nil {
		t.Fatalf("delete missing key: %v", err)
	}

	if list, err := c.List("/codis3/empty", false); err != nil || list != nil {
		t.Fatalf("list empty must=false = %v, %v", list, err)
	}
	if _, err := c.List("/codis3/empty", true); !stderrors.Is(err, ErrNotExist) {
		t.Fatalf("list empty must=true = %v, want ErrNotExist", err)
	}

	if err := c.Create("/codis3/foo/b", []byte("b")); err != nil {
		t.Fatalf("create foo/b: %v", err)
	}
	if err := c.Create("/codis3/foo-bar/c", []byte("c")); err != nil {
		t.Fatalf("create foo-bar/c: %v", err)
	}
	want := []string{"/codis3/foo", "/codis3/foo-bar"}
	list, err := c.List("/codis3", true)
	if err != nil || !equalStrings(list, want) {
		t.Fatalf("list /codis3 = %v, %v; want %v", list, err, want)
	}
	wantFoo := []string{"/codis3/foo/a", "/codis3/foo/b"}
	list, err = c.List("/codis3/foo", true)
	if err != nil || !equalStrings(list, wantFoo) {
		t.Fatalf("list /codis3/foo = %v, %v; want %v", list, err, wantFoo)
	}
	list, err = c.List("/codis3/foo/", false)
	if err != nil || !equalStrings(list, wantFoo) {
		t.Fatalf("list /codis3/foo/ = %v, %v; want %v", list, err, wantFoo)
	}

	if err := c.Delete("/codis3/foo-bar/c"); err != nil {
		t.Fatalf("delete foo-bar/c: %v", err)
	}
	if _, err := c.List("/codis3/foo-bar", true); !stderrors.Is(err, ErrNotExist) {
		t.Fatalf("list emptied path = %v, want ErrNotExist", err)
	}
	if err := c.Delete("/codis3/foo/a"); err != nil {
		t.Fatalf("delete foo/a: %v", err)
	}
	if _, err := c.Read("/codis3/foo/a", true); !stderrors.Is(err, ErrNotExist) {
		t.Fatalf("read deleted key = %v, want ErrNotExist", err)
	}
}

func TestIntegrationCreateConflictConcurrent(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 5 * time.Second,
	})

	const key = "/codis3/concurrent/lock"
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, conflict := 0, 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := c.Create(key, []byte(fmt.Sprintf("v%d", i)))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				success++
			case stderrors.Is(err, ErrExist):
				conflict++
			default:
				t.Errorf("create %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if success != 1 || conflict != 9 {
		t.Fatalf("concurrent create: success=%d conflict=%d, want 1/9", success, conflict)
	}
}

func TestIntegrationCreateUnknownWhenStopped(t *testing.T) {
	s := startEtcd(t, "http")
	s.waitReady(t, nil)

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
	})

	if err := c.Create("/codis3/stopped/before", []byte("v")); err != nil {
		t.Fatalf("create before stop: %v", err)
	}
	s.stop()

	err := c.Create("/codis3/stopped/after", []byte("v"))
	if err == nil {
		t.Fatal("create succeeded after etcd stopped")
	}
	if stderrors.Is(err, ErrExist) {
		t.Fatalf("stopped create reported as conflict: %v", err)
	}
	if got := Classify(err); got != KindUnknown {
		t.Fatalf("stopped create classified as %s, want Unknown: %v", got, err)
	}
}

func TestIntegrationTLSHandshakePreflight(t *testing.T) {
	pki := newTestPKI(t)
	s := startEtcd(t, "https",
		"--cert-file", pki.serverCertFile,
		"--key-file", pki.serverKeyFile,
		"--trusted-ca-file", pki.caFile,
		"--client-cert-auth",
	)
	s.waitReady(t, clientTLSConfig(t, pki.caFile, pki.clientCertFile, pki.clientKeyFile))

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 2 * time.Second,
		CAFile:         pki.caFile,
		CertFile:       pki.clientCertFile,
		KeyFile:        pki.clientKeyFile,
	})
	if err := c.Create("/codis3/tls/a", []byte("v")); err != nil {
		t.Fatalf("create over mtls: %v", err)
	}
	if b, err := c.Read("/codis3/tls/a", true); err != nil || string(b) != "v" {
		t.Fatalf("read over mtls = %q, %v", b, err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	plain := startEtcd(t, "http")
	plain.waitReady(t, nil)

	cases := []struct {
		name     string
		endpoint string
		caFile   string
		certFile string
		keyFile  string
		want     []string
	}{
		{
			name:     "ca mismatch",
			endpoint: s.endpoint,
			caFile:   pki.rogueCAFile,
			certFile: pki.clientCertFile,
			keyFile:  pki.clientKeyFile,
			want:     []string{"unknown authority", "certificate signed by unknown"},
		},
		{
			name:     "missing client certificate",
			endpoint: s.endpoint,
			caFile:   pki.caFile,
			want:     []string{"certificate required", "bad certificate"},
		},
		{
			name:     "tls against plaintext port",
			endpoint: "https://" + endpointHost(t, plain.endpoint),
			caFile:   pki.caFile,
			certFile: pki.clientCertFile,
			keyFile:  pki.clientKeyFile,
			want:     []string{"plaintext listener", "first record does not look like a TLS handshake"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			client, err := New(&Config{
				Endpoints:      []string{tc.endpoint},
				DialTimeout:    time.Second,
				RequestTimeout: time.Second,
				CAFile:         tc.caFile,
				CertFile:       tc.certFile,
				KeyFile:        tc.keyFile,
			})
			elapsed := time.Since(start)
			if client != nil {
				_ = client.Close()
			}
			if err == nil {
				t.Fatal("expected tls handshake failure")
			}
			if elapsed >= 2*time.Second {
				t.Fatalf("preflight took %s, want a fast failure", elapsed)
			}
			for _, want := range tc.want {
				if strings.Contains(err.Error(), want) {
					return
				}
			}
			t.Fatalf("error = %v, want one of %v", err, tc.want)
		})
	}
}

// startTarpit listens on 127.0.0.1 and accepts connections without ever
// answering, so a TLS handshake against it can only fail by deadline. It
// stands in for a blackholed or overloaded endpoint in the preflight tests.
func startTarpit(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tarpit: %v", err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	return "https://" + ln.Addr().String()
}

// TestIntegrationTLSPreflightBadEndpointFirst pins the shared preflight
// deadline (contract v1.2 §3): the unusable endpoint in front must not consume
// the budget of the healthy endpoint behind it.
func TestIntegrationTLSPreflightBadEndpointFirst(t *testing.T) {
	pki := newTestPKI(t)
	s := startEtcd(t, "https",
		"--cert-file", pki.serverCertFile,
		"--key-file", pki.serverKeyFile,
		"--trusted-ca-file", pki.caFile,
		"--client-cert-auth",
	)
	s.waitReady(t, clientTLSConfig(t, pki.caFile, pki.clientCertFile, pki.clientKeyFile))

	tarpit := startTarpit(t)
	start := time.Now()
	c, err := New(&Config{
		Endpoints:      []string{tarpit, s.endpoint},
		DialTimeout:    2 * time.Second,
		RequestTimeout: 2 * time.Second,
		CAFile:         pki.caFile,
		CertFile:       pki.clientCertFile,
		KeyFile:        pki.clientKeyFile,
	})
	elapsed := time.Since(start)
	if c != nil {
		t.Cleanup(func() { _ = c.Close() })
	}
	if err != nil {
		t.Fatalf("new client with a healthy endpoint behind %s: %v", tarpit, err)
	}
	if elapsed >= 3*time.Second {
		t.Fatalf("preflight took %s, want the healthy endpoint to win within one 2s budget", elapsed)
	}
	t.Logf("preflight with a tarpit first, healthy endpoint second: %s", elapsed)
}

// TestIntegrationTLSPreflightAllEndpointsUnavailable pins the other half of
// the deadline rule: N unusable endpoints fail after ONE dial_timeout, not
// N times it, and the failure names the preflight without leaking the
// password.
func TestIntegrationTLSPreflightAllEndpointsUnavailable(t *testing.T) {
	pki := newTestPKI(t)
	const password = "preflight-secret-password"
	endpoints := []string{startTarpit(t), startTarpit(t)}

	start := time.Now()
	c, err := New(&Config{
		Endpoints:      endpoints,
		DialTimeout:    2 * time.Second,
		RequestTimeout: 2 * time.Second,
		Username:       "root",
		Password:       Secret(password),
		CAFile:         pki.caFile,
		CertFile:       pki.clientCertFile,
		KeyFile:        pki.clientKeyFile,
	})
	elapsed := time.Since(start)
	if c != nil {
		_ = c.Close()
		t.Fatal("client created although every endpoint is unusable")
	}
	if err == nil {
		t.Fatal("expected the tls preflight to fail")
	}
	if elapsed < 2*time.Second {
		t.Fatalf("preflight failed after %s, want it to wait for the shared 2s deadline", elapsed)
	}
	if elapsed >= 3200*time.Millisecond {
		t.Fatalf("preflight failed after %s, want one shared 2s budget, not one per endpoint", elapsed)
	}
	if !strings.Contains(err.Error(), "tls preflight") {
		t.Fatalf("error = %v, want it to name the tls preflight", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("error leaks the password: %v", err)
	}
	t.Logf("preflight with two unusable endpoints failed after %s: %v", elapsed, err)
}

func TestIntegrationPasswordAuth(t *testing.T) {
	s := startEtcd(t, "http", "--auth-token", "simple")
	s.waitReady(t, nil)

	boot := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := boot.client.UserAdd(ctx, "root", "rootpw"); err != nil {
		t.Fatalf("user add: %v", err)
	}
	if _, err := boot.client.RoleAdd(ctx, "root"); err != nil {
		t.Fatalf("role add: %v", err)
	}
	if _, err := boot.client.UserGrantRole(ctx, "root", "root"); err != nil {
		t.Fatalf("grant role: %v", err)
	}
	if _, err := boot.client.AuthEnable(ctx); err != nil {
		t.Fatalf("auth enable: %v", err)
	}
	_ = boot.Close()

	const wrongPassword = "wrong-password-value"
	start := time.Now()
	_, err := New(&Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 3 * time.Second,
		Username:       "root",
		Password:       Secret(wrongPassword),
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("client with a wrong password was created")
	}
	if strings.Contains(err.Error(), wrongPassword) {
		t.Fatalf("authentication error leaks the password: %v", err)
	}
	if !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("error = %v, want an authentication failure", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("wrong password reported after %s, want a fast failure", elapsed)
	}
	if Classify(err) != KindRejected {
		t.Fatalf("wrong password classified as %s, want Rejected", Classify(err))
	}

	c := newTestClient(t, &Config{
		Endpoints:      []string{s.endpoint},
		DialTimeout:    time.Second,
		RequestTimeout: 3 * time.Second,
		Username:       "root",
		Password:       Secret("rootpw"),
	})
	if err := c.Create("/codis3/auth/a", []byte("v")); err != nil {
		t.Fatalf("create with valid credentials: %v", err)
	}
	if b, err := c.Read("/codis3/auth/a", true); err != nil || string(b) != "v" {
		t.Fatalf("read with valid credentials = %q, %v", b, err)
	}
}
