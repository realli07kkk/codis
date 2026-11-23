// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package migration

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/shake/entry"
	"github.com/CodisLabs/codis/pkg/shake/reader"
	"github.com/CodisLabs/codis/pkg/shake/writer"
)

// recorderWriter is a writer.Writer that records what it was asked to write.
type recorderWriter struct {
	mu     sync.Mutex
	addr   string
	wrote  []*entry.Entry
	closed bool
}

func (r *recorderWriter) Write(e *entry.Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wrote = append(r.wrote, e)
}

func (r *recorderWriter) StartWrite(ctx context.Context) chan *entry.Entry { return nil }
func (r *recorderWriter) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}
func (r *recorderWriter) Status() interface{}    { return map[string]string{"name": r.addr} }
func (r *recorderWriter) StatusString() string   { return r.addr }
func (r *recorderWriter) StatusConsistent() bool { return true }
func (r *recorderWriter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.wrote)
}

func twoShards() []Shard {
	return []Shard{
		{Address: "a:6379", Slots: []SlotRange{{Start: 0, End: 511}}},
		{Address: "b:6379", Slots: []SlotRange{{Start: 512, End: 1023}}},
	}
}

func keyForShard(t *testing.T, want int) string {
	t.Helper()
	for i := 0; i < 100000; i++ {
		key := fmt.Sprintf("key:%d", i)
		if int(proxy.Hash([]byte(key))%models.MaxSlotNum) < 512 && want == 0 {
			return key
		}
		if int(proxy.Hash([]byte(key))%models.MaxSlotNum) >= 512 && want == 1 {
			return key
		}
	}
	t.Fatal("no key found for shard")
	return ""
}

func TestCodisSlotMatchesProxyHash(t *testing.T) {
	if got, want := CodisSlot("user:1001"), int(proxy.Hash([]byte("user:1001"))%models.MaxSlotNum); got != want {
		t.Fatalf("CodisSlot = %d, proxy = %d", got, want)
	}
	if got, want := CodisSlot("{tag}.a"), int(proxy.Hash([]byte("tag"))%models.MaxSlotNum); got != want {
		t.Fatalf("hash tag slot = %d, want %d", got, want)
	}
	if CodisSlot("{tag}.a") != CodisSlot("{tag}.bbbb") {
		t.Fatal("hash tag keys must share a slot")
	}
}

func TestStaticClusterWriterRoutesByCodisSlot(t *testing.T) {
	shards := twoShards()
	recorders := make([]*recorderWriter, 0, 2)
	w, err := newStaticClusterWriter(nil, shards, func(opts *writer.RedisWriterOptions) writer.Writer {
		r := &recorderWriter{addr: opts.Address}
		recorders = append(recorders, r)
		return r
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recorders) != 2 || recorders[0].addr != "a:6379" || recorders[1].addr != "b:6379" {
		t.Fatalf("recorders = %v", recorders)
	}

	keyA := keyForShard(t, 0)
	keyB := keyForShard(t, 1)
	if w.Shard(keyA) != 0 || w.Shard(keyB) != 1 {
		t.Fatalf("shard lookup: %s -> %d, %s -> %d", keyA, w.Shard(keyA), keyB, w.Shard(keyB))
	}

	w.StartWrite(context.Background())
	setA := entry.NewEntry()
	setA.Argv = []string{"SET", keyA, "1"}
	setA.Parse()
	w.Write(setA)

	setB := entry.NewEntry()
	setB.Argv = []string{"SET", keyB, "2"}
	setB.Parse()
	w.Write(setB)

	// Multi-key command with hash tag lands on one shard.
	tagged := entry.NewEntry()
	tagged.Argv = []string{"MSET", "{tag}.a", "1", "{tag}.b", "2"}
	tagged.Parse()
	taggedShard := w.Shard("{tag}.a")
	w.Write(tagged)

	// Entries without keys (PING / FLUSHALL) fan out to every shard.
	ping := entry.NewEntry()
	ping.Argv = []string{"PING"}
	ping.Parse()
	w.Write(ping)

	// Each shard got the PING; shard a got keyA, shard b got keyB, and the
	// tagged MSET landed on exactly one shard (the one owning the tag).
	wantCounts := []int{2, 2}
	wantCounts[taggedShard]++
	if recorders[0].count() != wantCounts[0] || recorders[1].count() != wantCounts[1] {
		t.Fatalf("write counts = %d / %d, want %d / %d", recorders[0].count(), recorders[1].count(), wantCounts[0], wantCounts[1])
	}
	var taggedOn []int
	for i, rec := range recorders {
		for _, e := range rec.wrote {
			if e.Argv[0] == "MSET" {
				taggedOn = append(taggedOn, i)
			}
		}
	}
	if len(taggedOn) != 1 || taggedOn[0] != taggedShard {
		t.Fatalf("tagged MSET landed on %v, want only shard %d", taggedOn, taggedShard)
	}
	if rec := recorders[0].wrote[0]; rec.Argv[1] != keyA {
		t.Fatalf("shard a received %v, want key %s", rec.Argv, keyA)
	}
	if rec := recorders[1].wrote[0]; rec.Argv[1] != keyB {
		t.Fatalf("shard b received %v, want key %s", rec.Argv, keyB)
	}

	stats, ok := w.Status().([]interface{})
	if !ok || len(stats) != 2 {
		t.Fatalf("Status = %#v", w.Status())
	}
	if !w.StatusConsistent() {
		t.Fatal("all recorders are consistent")
	}
	w.Close()
	if !recorders[0].closed || !recorders[1].closed {
		t.Fatal("Close must close every shard writer")
	}
}

func TestStaticClusterWriterRequiresCoverage(t *testing.T) {
	_, err := newStaticClusterWriter(nil, []Shard{{Address: "a:1", Slots: []SlotRange{{Start: 0, End: 10}}}},
		func(opts *writer.RedisWriterOptions) writer.Writer { return &recorderWriter{addr: opts.Address} })
	if err == nil {
		t.Fatal("expected slot coverage error")
	}
}

// TestStaticClusterWriterPropagatesConnectionOptions guards the Codis target
// with requirepass: every shard writer must receive the endpoint credentials
// (and TLS / reply settings), only the address is per shard.
func TestStaticClusterWriterPropagatesConnectionOptions(t *testing.T) {
	base := &writer.RedisWriterOptions{
		Cluster:  true, // must be cleared: shards are plain standalone servers
		Username: "codis",
		Password: "secret",
		Tls:      true,
		OffReply: true,
	}
	var got []writer.RedisWriterOptions
	_, err := newStaticClusterWriter(base, twoShards(), func(opts *writer.RedisWriterOptions) writer.Writer {
		got = append(got, *opts)
		return &recorderWriter{addr: opts.Address}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("writers = %d, want 2", len(got))
	}
	for i, opts := range got {
		want := *base
		want.Address = twoShards()[i].Address
		want.Cluster = false
		if opts != want {
			t.Fatalf("shard %d options = %+v, want %+v", i, opts, want)
		}
	}
	// The caller's options must not be mutated by the per-shard copies.
	if base.Address != "" || base.Cluster != true {
		t.Fatalf("base options were mutated: %+v", base)
	}
}

// fakeReader is a reader.Reader stub used to test channel merging.
type fakeReader struct {
	ch         chan *entry.Entry
	status     string
	consistent bool
}

func (f *fakeReader) StartRead(ctx context.Context) []chan *entry.Entry {
	return []chan *entry.Entry{f.ch}
}
func (f *fakeReader) Status() interface{}    { return map[string]string{"status": f.status} }
func (f *fakeReader) StatusString() string   { return f.status }
func (f *fakeReader) StatusConsistent() bool { return f.consistent }

func TestStaticClusterReaderMergesShards(t *testing.T) {
	r1 := &fakeReader{ch: make(chan *entry.Entry, 1), status: "syncing aof", consistent: true}
	r2 := &fakeReader{ch: make(chan *entry.Entry, 1), status: "syncing rdb", consistent: false}
	rd := newStaticClusterReader([]reader.Reader{r1, r2})
	chs := rd.StartRead(context.Background())
	if len(chs) != 2 {
		t.Fatalf("channels = %d, want 2", len(chs))
	}
	if len(rd.Status().([]interface{})) != 2 {
		t.Fatalf("Status = %#v", rd.Status())
	}
	if rd.StatusConsistent() {
		t.Fatal("one shard is not consistent")
	}
	r2.consistent = true
	if !rd.StatusConsistent() {
		t.Fatal("all shards consistent")
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		s := rd.StatusString()
		seen[s] = true
	}
	if !seen["src-0, syncing aof"] || !seen["src-1, syncing rdb"] {
		t.Fatalf("StatusString rotation = %v", seen)
	}
}
