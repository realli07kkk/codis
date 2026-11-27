// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// Package modelstest provides an in-memory models.Client for tests ONLY.
//
// Unit tests must not depend on a real coordinator backend. This package is
// the test double every pure unit test uses; tests that exercise a real
// backend belong behind an explicit integration entry point and must
// construct the real client themselves.
//
// The semantics follow the new coordinator contract:
//
//   - Create fails when the key already exists (ErrExist).
//   - Update is an upsert.
//   - Delete removes exactly the given key; deleting a missing key succeeds.
//   - Read/List normalize a missing key to (nil, nil) when must is false and
//     report ErrNotExist when must is true.
//   - List returns the deduplicated, sorted direct children under
//     path + "/", matching etcd prefix semantics.
//   - Every write bumps one monotonic counter, which becomes the key's
//     mod_revision; a create also records it as the key's create_revision so
//     the persistent topom lock can be conditioned on it (LockClient).
//
// A State holds the shared key/value data; every Client created from the same
// State sees the same data but has an independent Close, which lets tests
// emulate multiple processes over one coordinator.
package modelstest

import (
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/errors"
)

// pkg/models defines no reusable sentinel errors for coordinator operations,
// so this package defines the ones it needs for tests to compare against.
var (
	// ErrExist is returned by Create for a key that already exists.
	ErrExist = errors.New("key already exists")
	// ErrNotExist is returned by Read/List with must=true for a missing key.
	ErrNotExist = errors.New("key does not exist")
	// ErrClosedClient is returned by every method of a closed Client.
	ErrClosedClient = errors.New("use of closed in-memory client")
)

var (
	_ models.Client     = (*Client)(nil)
	_ models.LockClient = (*Client)(nil)
)

// keyMeta mirrors the etcd revisions of one key.
type keyMeta struct {
	createRevision int64
	modRevision    int64
	lease          int64
}

// State is the shared in-memory data of one coordinator namespace.
type State struct {
	mu       sync.RWMutex
	revision int64
	data     map[string][]byte
	meta     map[string]keyMeta
}

// NewState creates an empty state. Use NewClient on it to create clients.
func NewState() *State {
	return &State{data: make(map[string][]byte), meta: make(map[string]keyMeta)}
}

// nextRevision returns the next store revision. s.mu must be held.
func (s *State) nextRevision() int64 {
	s.revision++
	return s.revision
}

// putLocked creates a new record and returns its create_revision. s.mu must be
// held and the key must be absent.
func (s *State) putLocked(key string, data []byte) int64 {
	rev := s.nextRevision()
	s.data[key] = append([]byte(nil), data...)
	s.meta[key] = keyMeta{createRevision: rev, modRevision: rev}
	return rev
}

// updateLocked upserts a record and returns its mod_revision. s.mu must be
// held.
func (s *State) updateLocked(key string, data []byte) int64 {
	rev := s.nextRevision()
	meta, ok := s.meta[key]
	if !ok {
		meta.createRevision = rev
	}
	meta.modRevision = rev
	s.data[key] = append([]byte(nil), data...)
	s.meta[key] = meta
	return rev
}

// NewClient returns a new independent client sharing this state.
func (s *State) NewClient() *Client {
	return &Client{state: s}
}

// Client is an in-memory models.Client.
type Client struct {
	mu     sync.Mutex
	state  *State
	closed bool
}

// State returns the shared state behind the client, so tests can open a
// second client that observes the same data (see State.NewClient).
func (c *Client) State() *State {
	return c.state
}

// Close marks the client as closed. Already closed clients return nil.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *Client) Create(path string, data []byte) error {
	if err := c.begin(); err != nil {
		return err
	}
	key := cleanPath(path)
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; ok {
		return errors.Trace(ErrExist)
	}
	s.putLocked(key, data)
	return nil
}

func (c *Client) Update(path string, data []byte) error {
	if err := c.begin(); err != nil {
		return err
	}
	key := cleanPath(path)
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateLocked(key, data)
	return nil
}

func (c *Client) Delete(path string) error {
	if err := c.begin(); err != nil {
		return err
	}
	key := cleanPath(path)
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	delete(s.meta, key)
	return nil
}

// CreateWithRevision implements models.LockClient.
func (c *Client) CreateWithRevision(p string, data []byte) (bool, int64, error) {
	if err := c.begin(); err != nil {
		return false, 0, err
	}
	key := cleanPath(p)
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; ok {
		return false, 0, nil
	}
	return true, s.putLocked(key, data), nil
}

// ReadWithRevision implements models.LockClient.
func (c *Client) ReadWithRevision(p string) ([]byte, int64, int64, int64, error) {
	if err := c.begin(); err != nil {
		return nil, 0, 0, 0, err
	}
	key := cleanPath(p)
	s := c.state
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.data[key]
	if !ok {
		return nil, 0, 0, 0, nil
	}
	meta := s.meta[key]
	// Always return an owned slice: an existing key with an empty value must
	// stay distinguishable from a missing key, whose data is nil.
	data := make([]byte, len(b))
	copy(data, b)
	return data, meta.createRevision, meta.modRevision, meta.lease, nil
}

// DeleteByCreateRevision implements models.LockClient.
func (c *Client) DeleteByCreateRevision(p string, createRevision int64) (bool, error) {
	if err := c.begin(); err != nil {
		return false, err
	}
	key := cleanPath(p)
	s := c.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if meta, ok := s.meta[key]; !ok || meta.createRevision != createRevision {
		return false, nil
	}
	delete(s.data, key)
	delete(s.meta, key)
	return true, nil
}

func (c *Client) Read(path string, must bool) ([]byte, error) {
	if err := c.begin(); err != nil {
		return nil, err
	}
	key := cleanPath(path)
	s := c.state
	s.mu.RLock()
	defer s.mu.RUnlock()
	if b, ok := s.data[key]; ok {
		return append([]byte(nil), b...), nil
	}
	if must {
		return nil, errors.Trace(ErrNotExist)
	}
	return nil, nil
}

func (c *Client) List(path string, must bool) ([]string, error) {
	if err := c.begin(); err != nil {
		return nil, err
	}
	key := cleanPath(path)
	prefix := key + "/"
	if key == "/" {
		prefix = "/"
	}
	s := c.state
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	var children []string
	for k := range s.data {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		child := k
		if i := strings.IndexByte(k[len(prefix):], '/'); i >= 0 {
			child = k[:len(prefix)+i]
		}
		if _, ok := seen[child]; ok {
			continue
		}
		seen[child] = struct{}{}
		children = append(children, child)
	}
	if len(children) == 0 {
		if must {
			return nil, errors.Trace(ErrNotExist)
		}
		return nil, nil
	}
	sort.Strings(children)
	return children, nil
}

// begin reports whether the client may still be used.
func (c *Client) begin() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.Trace(ErrClosedClient)
	}
	return nil
}

// cleanPath normalizes keys so that byte-identical paths map to one entry.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}
