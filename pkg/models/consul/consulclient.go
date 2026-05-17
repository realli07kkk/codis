// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package consulclient

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	capi "github.com/hashicorp/consul/api"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

var ErrClosedClient = errors.New("use of closed consul client")

var ErrNotSupported = errors.New("not supported")

// Consul requires session TTL to be at least 10 seconds.
const minSessionTTL = time.Second * 10

type Client struct {
	sync.Mutex

	client  *capi.Client
	timeout time.Duration

	cancel   context.CancelFunc
	context  context.Context
	sessions map[string]*sessionRecord
	closed   bool
}

type sessionRecord struct {
	path string
	key  string
	id   string

	stop   chan struct{}
	signal chan struct{}
	once   sync.Once
}

func (r *sessionRecord) close() {
	r.once.Do(func() {
		close(r.stop)
		close(r.signal)
	})
}

func New(addrlist string, auth string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = time.Second * 5
	}

	addr := strings.TrimSpace(addrlist)
	if addr == "" {
		return nil, errors.Errorf("invalid consul address")
	}
	if strings.Contains(addr, ",") {
		return nil, errors.Errorf("consul address must be a single HTTP address")
	}

	config := capi.DefaultConfig()
	config.Address = addr
	if auth != "" {
		config.Token = auth
	}

	client, err := capi.NewClient(config)
	if err != nil {
		return nil, errors.Trace(err)
	}
	c := &Client{
		client:   client,
		timeout:  timeout,
		sessions: make(map[string]*sessionRecord),
	}
	c.context, c.cancel = context.WithCancel(context.Background())
	return c, nil
}

func (c *Client) Close() error {
	c.Lock()
	if c.closed {
		c.Unlock()
		return nil
	}
	c.closed = true
	c.cancel()
	records := make([]*sessionRecord, 0, len(c.sessions))
	for _, record := range c.sessions {
		records = append(records, record)
	}
	c.sessions = make(map[string]*sessionRecord)
	c.Unlock()

	for _, record := range records {
		record.close()
		if err := c.destroySession(record); err != nil {
			log.WarnErrorf(err, "consul destroy session %s failed", record.id)
		}
	}
	return nil
}

func (c *Client) Create(path string, data []byte) error {
	key, err := cleanKey(path)
	if err != nil {
		return err
	}
	options, cancel, err := c.newWriteOptions()
	if err != nil {
		return err
	}
	defer cancel()

	ok, _, err := c.client.KV().CAS(&capi.KVPair{Key: key, Value: data, ModifyIndex: 0}, options)
	if err != nil {
		return errors.Trace(err)
	}
	if !ok {
		return errors.Errorf("consul node already exists: %s", path)
	}
	return nil
}

func (c *Client) Update(path string, data []byte) error {
	key, err := cleanKey(path)
	if err != nil {
		return err
	}
	options, cancel, err := c.newWriteOptions()
	if err != nil {
		return err
	}
	defer cancel()

	if _, err := c.client.KV().Put(&capi.KVPair{Key: key, Value: data}, options); err != nil {
		return errors.Trace(err)
	}
	return nil
}

func (c *Client) Delete(path string) error {
	key, err := cleanKey(path)
	if err != nil {
		return err
	}
	if record := c.takeSession(consulPath(key)); record != nil {
		record.close()
		return c.destroySession(record)
	}

	options, cancel, err := c.newWriteOptions()
	if err != nil {
		return err
	}
	defer cancel()

	if _, err := c.client.KV().Delete(key, options); err != nil {
		return errors.Trace(err)
	}
	return nil
}

func (c *Client) Read(path string, must bool) ([]byte, error) {
	key, err := cleanKey(path)
	if err != nil {
		return nil, err
	}
	options, cancel, err := c.newQueryOptions()
	if err != nil {
		return nil, err
	}
	defer cancel()

	pair, _, err := c.client.KV().Get(key, options)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if pair == nil {
		if must {
			return nil, errors.Errorf("consul node not found: %s", path)
		}
		return nil, nil
	}
	return pair.Value, nil
}

func (c *Client) List(path string, must bool) ([]string, error) {
	prefix, err := cleanPrefix(path)
	if err != nil {
		return nil, err
	}
	options, cancel, err := c.newQueryOptions()
	if err != nil {
		return nil, err
	}
	defer cancel()

	paths, _, err := c.list(prefix, options)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 && must {
		return nil, errors.Errorf("consul node not found: %s", path)
	}
	return paths, nil
}

func (c *Client) WatchInOrder(path string) (<-chan struct{}, []string, error) {
	prefix, err := cleanPrefix(path)
	if err != nil {
		return nil, nil, err
	}
	options, cancel, err := c.newQueryOptions()
	if err != nil {
		return nil, nil, err
	}
	defer cancel()

	paths, meta, err := c.list(prefix, options)
	if err != nil {
		return nil, nil, err
	}
	var index uint64
	if meta != nil {
		index = meta.LastIndex
	}

	signal := make(chan struct{})
	go c.watchPrefix(prefix, index, signal)
	return signal, paths, nil
}

func (c *Client) CreateEphemeral(path string, data []byte) (<-chan struct{}, error) {
	key, err := cleanKey(path)
	if err != nil {
		return nil, err
	}
	options, cancel, err := c.newWriteOptions()
	if err != nil {
		return nil, err
	}
	defer cancel()

	sessionID, _, err := c.client.Session().CreateNoChecks(&capi.SessionEntry{
		Name:      consulPath(key),
		TTL:       c.sessionTTL(),
		Behavior:  capi.SessionBehaviorDelete,
		LockDelay: 0,
	}, options)
	if err != nil {
		return nil, errors.Trace(err)
	}

	record := &sessionRecord{
		path:   consulPath(key),
		key:    key,
		id:     sessionID,
		stop:   make(chan struct{}),
		signal: make(chan struct{}),
	}

	ok, _, err := c.client.KV().Acquire(&capi.KVPair{Key: key, Value: data, Session: sessionID}, options)
	if err != nil {
		record.close()
		c.destroySession(record)
		return nil, errors.Trace(err)
	}
	if !ok {
		record.close()
		c.destroySession(record)
		return nil, errors.Errorf("consul ephemeral node already exists: %s", path)
	}

	if err := c.putSession(record); err != nil {
		record.close()
		c.destroySession(record)
		return nil, err
	}

	go c.renewSession(record)
	return record.signal, nil
}

func (c *Client) CreateEphemeralInOrder(path string, data []byte) (<-chan struct{}, string, error) {
	key, err := cleanKey(path)
	if err != nil {
		return nil, "", err
	}
	dir := strings.TrimRight(consulPath(key), "/")
	node := fmt.Sprintf("%s/%020d", dir, time.Now().UnixNano())
	signal, err := c.CreateEphemeral(node, data)
	if err != nil {
		return nil, "", err
	}
	return signal, node, nil
}

func (c *Client) newContext() (context.Context, context.CancelFunc, error) {
	c.Lock()
	defer c.Unlock()
	if c.closed {
		return nil, nil, errors.Trace(ErrClosedClient)
	}
	ctx, cancel := context.WithTimeout(c.context, c.timeout)
	return ctx, cancel, nil
}

func (c *Client) newQueryOptions() (*capi.QueryOptions, context.CancelFunc, error) {
	ctx, cancel, err := c.newContext()
	if err != nil {
		return nil, nil, err
	}
	options := &capi.QueryOptions{RequireConsistent: true}
	return options.WithContext(ctx), cancel, nil
}

func (c *Client) newWriteOptions() (*capi.WriteOptions, context.CancelFunc, error) {
	ctx, cancel, err := c.newContext()
	if err != nil {
		return nil, nil, err
	}
	options := &capi.WriteOptions{}
	return options.WithContext(ctx), cancel, nil
}

func (c *Client) newWatchOptions() (*capi.QueryOptions, error) {
	c.Lock()
	defer c.Unlock()
	if c.closed {
		return nil, errors.Trace(ErrClosedClient)
	}
	options := &capi.QueryOptions{RequireConsistent: true, WaitTime: c.timeout}
	return options.WithContext(c.context), nil
}

func (c *Client) sessionTTL() string {
	ttl := c.timeout
	if ttl < minSessionTTL {
		ttl = minSessionTTL
	}
	return ttl.String()
}

func (c *Client) putSession(record *sessionRecord) error {
	c.Lock()
	defer c.Unlock()
	if c.closed {
		return errors.Trace(ErrClosedClient)
	}
	if c.sessions[record.path] != nil {
		return errors.Errorf("consul ephemeral node already exists: %s", record.path)
	}
	c.sessions[record.path] = record
	return nil
}

func (c *Client) takeSession(path string) *sessionRecord {
	c.Lock()
	defer c.Unlock()
	record := c.sessions[path]
	if record != nil {
		delete(c.sessions, path)
	}
	return record
}

func (c *Client) finishSession(record *sessionRecord) {
	c.Lock()
	if c.sessions[record.path] == record {
		delete(c.sessions, record.path)
	}
	c.Unlock()
	record.close()
}

func (c *Client) backgroundWriteOptions() (*capi.WriteOptions, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	options := &capi.WriteOptions{}
	return options.WithContext(ctx), cancel
}

func (c *Client) destroySession(record *sessionRecord) error {
	options, cancel := c.backgroundWriteOptions()
	defer cancel()

	var first error
	if _, err := c.client.Session().Destroy(record.id, options); err != nil {
		first = errors.Trace(err)
	}
	if err := c.deleteSessionKey(record, options); err != nil && first == nil {
		first = errors.Trace(err)
	}
	return first
}

func (c *Client) deleteSessionKey(record *sessionRecord, options *capi.WriteOptions) error {
	query := (&capi.QueryOptions{RequireConsistent: true}).WithContext(options.Context())
	pair, _, err := c.client.KV().Get(record.key, query)
	if err != nil {
		return errors.Trace(err)
	}
	if pair == nil || pair.Session != record.id {
		return nil
	}
	if _, _, err := c.client.KV().DeleteCAS(&capi.KVPair{Key: record.key, ModifyIndex: pair.ModifyIndex}, options); err != nil {
		return errors.Trace(err)
	}
	return nil
}

func (c *Client) renewSession(record *sessionRecord) {
	options := (&capi.WriteOptions{}).WithContext(c.context)
	err := c.client.Session().RenewPeriodic(c.sessionTTL(), record.id, options, record.stop)
	if err != nil {
		log.WarnErrorf(err, "consul renew session %s failed", record.id)
		if err := c.destroySession(record); err != nil {
			log.WarnErrorf(err, "consul destroy session %s after renew failure failed", record.id)
		}
	}
	c.finishSession(record)
}

func cleanKey(path string) (string, error) {
	key := strings.TrimPrefix(filepath.Clean(path), "/")
	if key == "" || key == "." {
		return "", errors.Errorf("invalid consul path: %s", path)
	}
	return key, nil
}

func cleanPrefix(path string) (string, error) {
	key, err := cleanKey(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(key, "/") + "/", nil
}

func consulPath(key string) string {
	key = strings.Trim(strings.TrimSuffix(key, "/"), "/")
	if key == "" {
		return "/"
	}
	return "/" + key
}

func (c *Client) list(prefix string, options *capi.QueryOptions) ([]string, *capi.QueryMeta, error) {
	keys, meta, err := c.client.KV().Keys(prefix, "/", options)
	if err != nil {
		return nil, nil, errors.Trace(err)
	}
	var paths []string
	for _, key := range keys {
		key = strings.TrimSuffix(key, "/")
		if key == strings.TrimSuffix(prefix, "/") {
			continue
		}
		paths = append(paths, consulPath(key))
	}
	sort.Strings(paths)
	return paths, meta, nil
}

func (c *Client) watchPrefix(prefix string, index uint64, signal chan struct{}) {
	defer close(signal)
	for {
		options, err := c.newWatchOptions()
		if err != nil {
			return
		}
		options.WaitIndex = index

		_, meta, err := c.list(prefix, options)
		if err != nil {
			return
		}
		if meta == nil {
			return
		}
		if index != 0 && meta.LastIndex > index {
			return
		}
		index = meta.LastIndex
	}
}
