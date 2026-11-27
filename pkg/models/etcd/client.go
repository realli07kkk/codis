// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package etcd

import (
	"context"
	"crypto/tls"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	rpctypes "go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

var _ models.Client = (*Client)(nil)

// Options tunes the client. The zero value is usable.
type Options struct {
	// Logger overrides the SDK logger. When nil, SDK log entries are bridged
	// into the Codis logger at warn level instead of being printed to stderr.
	Logger *zap.Logger
}

type Client struct {
	client         *clientv3.Client
	requestTimeout time.Duration
	closed         atomic.Bool
}

// New builds a client from a validated connection config.
func New(cfg *Config) (*Client, error) {
	return NewWithOptions(cfg, Options{})
}

func NewWithOptions(cfg *Config, opts Options) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("nil etcd config")
	}
	c := *cfg
	c.Endpoints = append([]string(nil), cfg.Endpoints...)
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	eps, err := checkEndpoints(c.Endpoints, true)
	if err != nil {
		return nil, errors.Trace(err)
	}
	c.Endpoints = eps

	tlsCfg, err := c.TLSConfig()
	if err != nil {
		return nil, errors.Trace(err)
	}
	if tlsCfg != nil {
		if err := preflightTLS(c.Endpoints, tlsCfg, c.DialTimeout); err != nil {
			return nil, err
		}
	}

	logger := opts.Logger
	if logger == nil {
		logger = codisZapLogger()
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   c.Endpoints,
		DialTimeout: c.DialTimeout,
		TLS:         tlsCfg,
		Username:    c.Username,
		Password:    c.Password.Value(),
		Logger:      logger,
	})
	if err != nil {
		if stderrors.Is(err, rpctypes.ErrAuthFailed) || stderrors.Is(err, rpctypes.ErrPermissionDenied) ||
			stderrors.Is(err, rpctypes.ErrAuthNotEnabled) || stderrors.Is(err, rpctypes.ErrInvalidAuthToken) {
			return nil, errors.Errorf("etcd client: authentication failed for user %q: %w", c.Username, err)
		}
		return nil, errors.Errorf("etcd client: initialize failed: %w", err)
	}
	return &Client{client: client, requestTimeout: c.RequestTimeout}, nil
}

// tlsProbeReadTimeout bounds the post-handshake read used by preflightTLS.
const tlsProbeReadTimeout = 200 * time.Millisecond

// preflightTLS probes every endpoint concurrently under ONE shared deadline
// (dial_timeout), not one budget per endpoint: the first successful handshake
// wins and cancels the rest, so a slow or blackholed endpoint in front cannot
// starve the healthy ones, and an unreachable cluster fails after one
// dial_timeout instead of N. A misconfigured CA, a missing client certificate
// or a plaintext port still fail with the underlying reason instead of a bare
// timeout.
func preflightTLS(endpoints []string, tlsCfg *tls.Config, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}
	type probe struct {
		endpoint string
		addr     string
	}
	probes := make([]probe, 0, len(endpoints))
	for _, ep := range endpoints {
		u, err := url.Parse(ep)
		if err != nil || u.Host == "" {
			continue
		}
		probes = append(probes, probe{endpoint: ep, addr: probeAddr(u)})
	}
	if len(probes) == 0 {
		return errors.Errorf("no endpoint passed the tls preflight within %s: no probeable endpoint", timeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type result struct {
		endpoint string
		err      error
	}
	// The channel is buffered for every probe, so a probe that finishes after
	// preflightTLS returned can still deliver its result and exit; the shared
	// ctx cancels the remaining handshakes.
	results := make(chan result, len(probes))
	for _, p := range probes {
		go func(p probe) {
			results <- result{endpoint: p.endpoint, err: probeEndpointTLS(ctx, p.addr, tlsCfg)}
		}(p)
	}

	causes := make([]error, 0, len(probes))
	for range probes {
		r := <-results
		if r.err == nil {
			return nil
		}
		causes = append(causes, fmt.Errorf("%s: %w", r.endpoint, r.err))
	}
	return errors.Errorf("no endpoint passed the tls preflight within %s: %w", timeout, joinProbeCauses(causes))
}

// joinProbeCauses merges the first few probe failures so the reported reason
// stays readable however many endpoints are configured.
func joinProbeCauses(causes []error) error {
	const limit = 3
	switch {
	case len(causes) == 0:
		return errors.New("no reason reported")
	case len(causes) <= limit:
		return stderrors.Join(causes...)
	default:
		rest := fmt.Errorf("%d more endpoint(s) failed", len(causes)-limit)
		return stderrors.Join(append(causes[:limit:limit], rest)...)
	}
}

// probeAddr is the address the TLS preflight dials for one endpoint. An
// endpoint given without an explicit port is probed on the scheme's default
// (https 443, http 80): the gRPC resolver uses the same default for a bare
// host, so such a URL works with the SDK even though tls.Dialer rejects an
// address without a port. The configured endpoints are never rewritten; this
// only supplies a port for the probe.
func probeAddr(u *url.URL) string {
	host := u.Hostname()
	if host == "" {
		return ""
	}
	if port := u.Port(); port != "" {
		return net.JoinHostPort(host, port)
	}
	if u.Scheme == "http" {
		return net.JoinHostPort(host, "80")
	}
	return net.JoinHostPort(host, "443")
}

// probeEndpointTLS returns nil when the endpoint accepts a TLS session with
// the configured credentials, or the underlying handshake failure. Both the
// handshake and the post-handshake read are bounded by ctx, the shared
// preflight deadline.
func probeEndpointTLS(ctx context.Context, addr string, tlsCfg *tls.Config) error {
	dialer := &tls.Dialer{Config: tlsCfg.Clone()}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return describeTLSProbeError(err)
	}
	defer conn.Close()

	// TLS 1.3 may deliver server alerts such as "certificate required" only
	// after the handshake call returned, so a bounded read confirms the
	// session is usable. A healthy etcd keeps the connection silent (it waits
	// for the HTTP/2 preface), so a read timeout means success — unless the
	// shared deadline expired first, in which case the read proves nothing.
	deadline := time.Now().Add(tlsProbeReadTimeout)
	if shared, ok := ctx.Deadline(); ok && shared.Before(deadline) {
		deadline = shared
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return describeTLSProbeError(err)
	}
	var buf [1]byte
	n, err := conn.Read(buf[:])
	if err == nil || n > 0 {
		return nil
	}
	var netErr net.Error
	if stderrors.As(err, &netErr) && netErr.Timeout() {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return describeTLSProbeError(ctxErr)
		}
		return nil
	}
	return describeTLSProbeError(err)
}

func describeTLSProbeError(err error) error {
	if stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("connection closed during tls handshake: endpoint is likely a plaintext listener (" + err.Error() + ")")
	}
	return err
}

// codisZapLogger bridges the SDK log entries into the Codis logger. Only
// warn and above are forwarded, so the SDK stops writing to stderr.
func codisZapLogger() *zap.Logger {
	core := codisCore{
		LevelEnabler: zap.LevelEnablerFunc(func(l zapcore.Level) bool {
			return l >= zapcore.WarnLevel
		}),
		encoder: zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
			MessageKey:     "msg",
			LevelKey:       "level",
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
		}),
	}
	return zap.New(core).Named("etcd-client")
}

type codisCore struct {
	zapcore.LevelEnabler
	encoder zapcore.Encoder
}

func (c codisCore) With(fields []zapcore.Field) zapcore.Core {
	encoder := c.encoder.Clone()
	for i := range fields {
		fields[i].AddTo(encoder)
	}
	return codisCore{LevelEnabler: c.LevelEnabler, encoder: encoder}
}

func (c codisCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

func (c codisCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	buf, err := c.encoder.EncodeEntry(entry, fields)
	if err != nil {
		return err
	}
	line := strings.TrimSpace(buf.String())
	buf.Free()
	if entry.LoggerName != "" {
		line = entry.LoggerName + ": " + line
	}
	switch entry.Level {
	case zapcore.WarnLevel:
		log.Warnf("%s", line)
	case zapcore.ErrorLevel, zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		log.Errorf("%s", line)
	default:
		log.Infof("%s", line)
	}
	return nil
}

func (c codisCore) Sync() error { return nil }

func (c *Client) newContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), c.requestTimeout)
}

func (c *Client) checkOpen() error {
	if c.closed.Load() {
		return errors.Trace(ErrClosedClient)
	}
	return nil
}

// txnCreate atomically creates key only if it does not exist. The returned
// header carries CreateRevision/ModRevision so a later lock API can reuse the
// same compare-then-put shape. succeeded is false when the key already exists.
func (c *Client) txnCreate(key string, data []byte) (succeeded bool, header *pb.ResponseHeader, err error) {
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Version(key), "=", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()
	if err != nil {
		return false, nil, wrapOp("create", key, err)
	}
	hdr := resp.Header
	return resp.Succeeded, hdr, nil
}

func (c *Client) Create(path string, data []byte) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	succeeded, _, err := c.txnCreate(path, data)
	if err != nil {
		return err
	}
	if !succeeded {
		return &OpError{Op: "create", Key: path, Err: ErrExist, Kind: KindExist}
	}
	return nil
}

func (c *Client) Update(path string, data []byte) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	if _, err := c.client.Put(ctx, path, string(data)); err != nil {
		return wrapOp("update", path, err)
	}
	return nil
}

func (c *Client) Delete(path string) error {
	if err := c.checkOpen(); err != nil {
		return err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	if _, err := c.client.Delete(ctx, path); err != nil {
		return wrapOp("delete", path, err)
	}
	return nil
}

func (c *Client) Read(path string, must bool) ([]byte, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.Get(ctx, path)
	if err != nil {
		return nil, wrapOp("read", path, err)
	}
	for _, kv := range resp.Kvs {
		if string(kv.Key) == path {
			return kv.Value, nil
		}
	}
	if !must {
		return nil, nil
	}
	return nil, &OpError{Op: "read", Key: path, Err: ErrNotExist, Kind: KindNotFound}
}

func (c *Client) List(path string, must bool) ([]string, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	dir := strings.TrimSuffix(path, "/")
	ctx, cancel := c.newContext()
	defer cancel()
	resp, err := c.client.Get(ctx, dir+"/", clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		return nil, wrapOp("list", path, err)
	}
	keys := make([]string, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		keys = append(keys, string(kv.Key))
	}
	results := directChildren(dir, keys)
	if len(results) == 0 {
		if !must {
			return nil, nil
		}
		return nil, &OpError{Op: "list", Key: path, Err: ErrNotExist, Kind: KindNotFound}
	}
	return results, nil
}

// directChildren maps full keys under path to its direct child paths, dedupes
// them and sorts them. /codis3/foo and /codis3/foo-bar stay isolated.
func directChildren(path string, keys []string) []string {
	prefix := path + "/"
	seen := make(map[string]struct{}, len(keys))
	results := make([]string, 0, len(keys))
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			rest = rest[:i]
		}
		if rest == "" {
			continue
		}
		full := prefix + rest
		if _, ok := seen[full]; ok {
			continue
		}
		seen[full] = struct{}{}
		results = append(results, full)
	}
	slices.Sort(results)
	return results
}

func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	if err := c.client.Close(); err != nil {
		return errors.Trace(err)
	}
	return nil
}

// WatchInOrder is part of the pre-3.7 models.Client; the etcd coordinator line
// has no ordered-watch support.
func (c *Client) WatchInOrder(path string) (<-chan struct{}, []string, error) {
	if err := c.checkOpen(); err != nil {
		return nil, nil, err
	}
	return nil, nil, &OpError{Op: "watch", Key: path, Err: ErrNotSupported, Kind: KindRejected}
}

// CreateEphemeral is part of the pre-3.7 models.Client; ephemeral keys are
// bound to a lease through the lease API instead.
func (c *Client) CreateEphemeral(path string, data []byte) (<-chan struct{}, error) {
	if err := c.checkOpen(); err != nil {
		return nil, err
	}
	return nil, &OpError{Op: "create-ephemeral", Key: path, Err: ErrNotSupported, Kind: KindRejected}
}

// CreateEphemeralInOrder is part of the pre-3.7 models.Client.
func (c *Client) CreateEphemeralInOrder(path string, data []byte) (<-chan struct{}, string, error) {
	if err := c.checkOpen(); err != nil {
		return nil, "", err
	}
	return nil, "", &OpError{Op: "create-ephemeral", Key: path, Err: ErrNotSupported, Kind: KindRejected}
}
