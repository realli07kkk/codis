// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"bytes"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/modelstest"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/utils/assert"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

var config = NewDefaultConfig()

func init() {
	log.SetLevel(log.LevelError)
}

func init() {
	config.AdminAddr = "127.0.0.1:0"
	config.ProductName = "topom_test"
	config.ProductAuth = "topom_auth"
}

// newDiskClient opens an in-memory coordinator namespace, replacing the
// filesystem driver that unit tests used to run against.
func newDiskClient(t *testing.T) *modelstest.Client {
	t.Helper()
	return modelstest.NewState().NewClient()
}

// newForkClient opens a second independent client over the same namespace,
// which emulates another process sharing the coordinator data.
func newForkClient(t *testing.T, client *modelstest.Client) *modelstest.Client {
	t.Helper()
	return client.State().NewClient()
}

func openTopom(x *testing.T) *Topom {
	t, err := New(newDiskClient(x), config)
	assert.MustNoError(x, err)
	assert.MustNoError(x, t.Start(false))
	return t
}

func openProxy(t *testing.T) (*models.Proxy, *proxy.ApiClient) {
	config := proxy.NewDefaultConfig()
	config.AdminAddr = "127.0.0.1:0"
	config.ProxyAddr = "0.0.0.0:0"
	config.ProductName = "topom_test"
	config.ProductAuth = "topom_auth"
	config.ProxyHeapPlaceholder = 0
	config.ProxyMaxOffheapBytes = 0

	s, err := proxy.New(config)
	assert.MustNoError(t, err)

	c := proxy.NewApiClient(s.Model().AdminAddr)
	c.SetXAuth(config.ProductName, config.ProductAuth, s.Model().Token)

	p, err := c.Model()
	assert.MustNoError(t, err)
	return p, c
}

func TestTopomClose(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	assert.Must(x, t.IsClosed() == false)
	assert.MustNoError(x, t.Close())
	assert.Must(x, t.IsClosed() == true)
}

// topomLogBuffer collects log output written by several goroutines.
type topomLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *topomLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *topomLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestTopomCloseDoesNotWarnAboutAdminShutdown keeps a normal shutdown quiet:
// Close closes the admin listener to unblock accept, so the graceful drain that
// follows finds it already closed and reports net.ErrClosed. That is the normal
// path and must not be logged as "shutdown failed". Which select branch of
// serveAdmin wins is a scheduling detail, so the deterministic reproduction of
// the already-closed listener lives in pkg/utils/httpserver_test.go; this test
// only checks the real Topom.Close path leaves no failure warning behind.
func TestTopomCloseDoesNotWarnAboutAdminShutdown(x *testing.T) {
	buf := &topomLogBuffer{}
	logger := log.New(buf, "")
	std := log.SwapStdLog(logger)
	defer log.SetStdLog(std)

	s := openTopom(x)
	if err := s.Close(); err != nil {
		x.Fatalf("close: %v", err)
	}
	// Give the admin goroutine time to drain in the background.
	time.Sleep(300 * time.Millisecond)
	if out := buf.String(); strings.Contains(out, "shutdown failed") {
		x.Fatalf("the normal shutdown was reported as a failure: %q", out)
	}
}

func TestTopomExclusive(x *testing.T) {
	client := newDiskClient(x)

	t1, err := New(newForkClient(x, client), config)
	assert.MustNoError(x, err)
	assert.MustNoError(x, t1.Start(false))

	defer t1.Close()

	t2, err := New(newForkClient(x, client), config)
	assert.MustNoError(x, err)
	err = t2.Start(false)
	assert.Must(x, err != nil)
	// A lock conflict is a definite rejection, not an unknown outcome.
	assert.Must(x, stderrors.Is(err, models.ErrLockHeld))
	assert.Must(x, !stderrors.Is(err, models.ErrResultUnknown))
	assert.Must(x, t2.IsOnline() == false)

	t1.Close()

	t3, err := New(newForkClient(x, client), config)
	assert.MustNoError(x, err)
	assert.MustNoError(x, t3.Start(false))
}

// assertNoBackgroundTasks fails when a periodic task was registered. Those
// tasks run until the topom closes, so a completed Wait means none exist.
func assertNoBackgroundTasks(t *testing.T, s *Topom) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		s.bg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("background topology tasks were started without a confirmed lock")
	}
}

func TestTopomStartUnknownResultDoesNotGoOnline(t *testing.T) {
	t.Run("create failed before commit", func(t *testing.T) {
		state := modelstest.NewState()
		faults := &modelstest.Faults{FailCreateBeforeCommit: true}
		s, err := New(modelstest.Faulty(state.NewClient(), faults), config)
		assert.MustNoError(t, err)

		err = s.Start(true)
		if err == nil {
			t.Fatal("Start succeeded although the lock commit failed")
		}
		if !stderrors.Is(err, models.ErrResultUnknown) {
			t.Fatalf("Start error = %v, want ErrResultUnknown", err)
		}
		if s.IsOnline() {
			t.Fatal("topom is online without a confirmed lock")
		}
		assertNoBackgroundTasks(t, s)

		store := models.NewStore(state.NewClient(), config.ProductName)
		if info, showErr := store.ShowLock(); showErr != nil || info != nil {
			t.Fatalf("lock after a failed start = %+v, %v; want none", info, showErr)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close after a failed start = %v, want nil", err)
		}
	})

	t.Run("committed but response lost", func(t *testing.T) {
		state := modelstest.NewState()
		faults := &modelstest.Faults{LoseCreateResponse: true, FailReadBack: true}
		s, err := New(modelstest.Faulty(state.NewClient(), faults), config)
		assert.MustNoError(t, err)

		err = s.Start(true)
		if err == nil {
			t.Fatal("Start succeeded without a successful read-back")
		}
		if !stderrors.Is(err, models.ErrResultUnknown) {
			t.Fatalf("Start error = %v, want ErrResultUnknown", err)
		}
		if stderrors.Is(err, models.ErrLockHeld) {
			t.Fatalf("unconfirmed start reported as a lock conflict: %v", err)
		}
		if s.IsOnline() {
			t.Fatal("topom is online without a confirmed lock")
		}
		assertNoBackgroundTasks(t, s)

		// The unconfirmed create did commit; the process never owned it, so
		// Close must leave the record for operator recovery.
		faults.Clear()
		store := models.NewStore(state.NewClient(), config.ProductName)
		info, showErr := store.ShowLock()
		if showErr != nil || info == nil {
			t.Fatalf("show lock = %+v, %v; want the unconfirmed record", info, showErr)
		}
		if info.Topom.OwnerID != s.Model().OwnerID {
			t.Fatalf("unconfirmed lock owner = %q, want %q", info.Topom.OwnerID, s.Model().OwnerID)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close after an unknown start = %v, want nil", err)
		}
		after, showErr := store.ShowLock()
		if showErr != nil || after == nil || after.CreateRevision != info.CreateRevision {
			t.Fatalf("lock after Close = %+v, %v; want the untouched revision %d",
				after, showErr, info.CreateRevision)
		}
	})
}

func TestTopomCloseAfterLockRemovedByOperator(t *testing.T) {
	client := newDiskClient(t)
	s, err := New(client, config)
	assert.MustNoError(t, err)
	assert.MustNoError(t, s.Start(false))

	store := models.NewStore(newForkClient(t, client), config.ProductName)
	info, err := store.ShowLock()
	assert.MustNoError(t, err)
	assert.Must(t, info != nil)
	assert.MustNoError(t, store.RemoveLock(info.CreateRevision))

	// The lock is gone, so releasing it on shutdown reports a warning only.
	if err := s.Close(); err != nil {
		t.Fatalf("Close after the lock was removed = %v, want nil", err)
	}
}

func TestTopomOwnerIDChangesPerInstanceAndRestart(t *testing.T) {
	state := modelstest.NewState()

	first, err := New(state.NewClient(), config)
	assert.MustNoError(t, err)
	second, err := New(state.NewClient(), config)
	assert.MustNoError(t, err)
	defer second.Close()

	if first.Model().OwnerID == "" || second.Model().OwnerID == "" {
		t.Fatalf("owner ids = %q, %q; want generated ids", first.Model().OwnerID, second.Model().OwnerID)
	}
	if first.Model().OwnerID == second.Model().OwnerID {
		t.Fatalf("two instances share the owner id %q", first.Model().OwnerID)
	}

	assert.MustNoError(t, first.Start(false))
	firstOwner := first.Model().OwnerID
	store := models.NewStore(state.NewClient(), config.ProductName)
	info, err := store.ShowLock()
	assert.MustNoError(t, err)
	assert.Must(t, info != nil)
	if info.Topom.OwnerID != firstOwner {
		t.Fatalf("lock owner = %q, want %q", info.Topom.OwnerID, firstOwner)
	}
	assert.MustNoError(t, first.Close())

	// A restart under the same product configuration is a different owner.
	restarted, err := New(state.NewClient(), config)
	assert.MustNoError(t, err)
	defer restarted.Close()
	assert.MustNoError(t, restarted.Start(false))
	if restarted.Model().OwnerID == firstOwner {
		t.Fatalf("restart reused the owner id %q", firstOwner)
	}
	info, err = store.ShowLock()
	assert.MustNoError(t, err)
	assert.Must(t, info != nil)
	if info.Topom.OwnerID != restarted.Model().OwnerID {
		t.Fatalf("lock owner after restart = %q, want %q",
			info.Topom.OwnerID, restarted.Model().OwnerID)
	}
}

func TestTopomModelEncodesOwnerID(t *testing.T) {
	client := newDiskClient(t)
	s, err := New(client, config)
	assert.MustNoError(t, err)
	defer s.Close()
	assert.MustNoError(t, s.Start(false))

	encoded := string(s.Model().Encode())
	if !strings.Contains(encoded, `"owner_id"`) {
		t.Fatalf("encoded topom has no owner_id: %s", encoded)
	}
	if !strings.Contains(encoded, s.Model().OwnerID) {
		t.Fatalf("encoded topom has no owner id %q: %s", s.Model().OwnerID, encoded)
	}

	store := models.NewStore(newForkClient(t, client), config.ProductName)
	info, err := store.ShowLock()
	assert.MustNoError(t, err)
	assert.Must(t, info != nil)
	if info.Topom.OwnerID != s.Model().OwnerID {
		t.Fatalf("lock owner = %q, want %q", info.Topom.OwnerID, s.Model().OwnerID)
	}
	if info.CreateRevision <= 0 {
		t.Fatalf("lock create revision = %d, want a positive revision", info.CreateRevision)
	}
}
