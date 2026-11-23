// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package migration

import (
	"context"
	"fmt"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/shake/entry"
	"github.com/CodisLabs/codis/pkg/shake/log"
	"github.com/CodisLabs/codis/pkg/shake/reader"
	"github.com/CodisLabs/codis/pkg/shake/writer"
)

// CodisSlot returns the Codis slot (crc32 of the hash tag or key modulo 1024)
// exactly the way pkg/proxy routes commands. It is the routing key of the
// static cluster writer.
func CodisSlot(key string) int {
	return int(proxy.Hash([]byte(key)) % models.MaxSlotNum)
}

// staticClusterReader fans out to one standalone reader per shard address and
// merges their entry channels. It mirrors RedisShake's syncClusterReader /
// scanClusterReader, but the topology is explicit instead of being discovered
// through CLUSTER NODES, which Codis servers and proxies do not provide.
type staticClusterReader struct {
	readers  []reader.Reader
	statusId int
}

func newStaticClusterReader(readers []reader.Reader) reader.Reader {
	return &staticClusterReader{readers: readers}
}

// NewStaticSyncReader builds a sync (PSYNC) reader over the given shard
// addresses. Every shard is replicated independently, exactly like upstream
// does for every master of a Redis Cluster.
func NewStaticSyncReader(ctx context.Context, opts *reader.SyncReaderOptions, addresses []string) (reader.Reader, error) {
	if len(addresses) == 0 {
		return nil, fmt.Errorf("static reader needs at least one shard address")
	}
	readers := make([]reader.Reader, 0, len(addresses))
	for _, address := range addresses {
		if err := validateAddress(address); err != nil {
			return nil, fmt.Errorf("shard %s: %w", address, err)
		}
		theOpts := *opts
		theOpts.Address = address
		readers = append(readers, reader.NewSyncStandaloneReader(ctx, &theOpts))
	}
	log.Infof("create StaticClusterReader with %d shards", len(readers))
	return newStaticClusterReader(readers), nil
}

// NewStaticScanReader builds a SCAN + DUMP/RESTORE reader over the given shard
// addresses.
func NewStaticScanReader(ctx context.Context, opts *reader.ScanReaderOptions, addresses []string) (reader.Reader, error) {
	if len(addresses) == 0 {
		return nil, fmt.Errorf("static reader needs at least one shard address")
	}
	readers := make([]reader.Reader, 0, len(addresses))
	for _, address := range addresses {
		if err := validateAddress(address); err != nil {
			return nil, fmt.Errorf("shard %s: %w", address, err)
		}
		theOpts := *opts
		theOpts.Address = address
		readers = append(readers, reader.NewScanStandaloneReader(ctx, &theOpts))
	}
	log.Infof("create StaticClusterScanReader with %d shards", len(readers))
	return newStaticClusterReader(readers), nil
}

func (rd *staticClusterReader) StartRead(ctx context.Context) []chan *entry.Entry {
	chs := make([]chan *entry.Entry, 0, len(rd.readers))
	for _, r := range rd.readers {
		chs = append(chs, r.StartRead(ctx)...)
	}
	return chs
}

func (rd *staticClusterReader) Status() interface{} {
	stat := make([]interface{}, 0, len(rd.readers))
	for _, r := range rd.readers {
		stat = append(stat, r.Status())
	}
	return stat
}

func (rd *staticClusterReader) StatusString() string {
	if len(rd.readers) == 0 {
		return "static reader without shards"
	}
	rd.statusId = (rd.statusId + 1) % len(rd.readers)
	return fmt.Sprintf("src-%d, %s", rd.statusId, rd.readers[rd.statusId].StatusString())
}

func (rd *staticClusterReader) StatusConsistent() bool {
	for _, r := range rd.readers {
		if !r.StatusConsistent() {
			return false
		}
	}
	return true
}

// StaticClusterWriter routes every entry to the shard that owns the Codis slot
// of its first key. Each shard is backed by an upstream standalone writer, so
// pipelining, rate limiting and reply handling stay identical to RedisShake.
type StaticClusterWriter struct {
	shards  []Shard
	writers []writer.Writer
	router  *SlotRouter
	stat    []interface{}
}

// NewStaticClusterWriter builds the writer. The shard slot ranges must cover
// every Codis slot exactly once (ValidateSlotCoverage).
func NewStaticClusterWriter(ctx context.Context, opts *writer.RedisWriterOptions, shards []Shard) (*StaticClusterWriter, error) {
	return newStaticClusterWriter(opts, shards, func(theOpts *writer.RedisWriterOptions) writer.Writer {
		return writer.NewRedisStandaloneWriter(ctx, theOpts)
	})
}

// shardWriterFactory creates the per-shard writer; tests inject a recorder.
type shardWriterFactory func(opts *writer.RedisWriterOptions) writer.Writer

// newStaticClusterWriter copies baseOpts (AUTH, TLS, sentinel...) for every
// shard and only overrides the address. Codis protects its backends with
// requirepass, so a static target must authenticate exactly like the single
// target writer does.
func newStaticClusterWriter(baseOpts *writer.RedisWriterOptions, shards []Shard, factory shardWriterFactory) (*StaticClusterWriter, error) {
	router, err := NewSlotRouter(shards)
	if err != nil {
		return nil, err
	}
	w := &StaticClusterWriter{shards: append([]Shard(nil), shards...), router: router}
	for _, shard := range shards {
		theOpts := new(writer.RedisWriterOptions)
		if baseOpts != nil {
			*theOpts = *baseOpts
		}
		theOpts.Address = shard.Address
		theOpts.Cluster = false
		w.writers = append(w.writers, factory(theOpts))
	}
	log.Infof("StaticClusterWriter connected to %d shards", len(w.writers))
	return w, nil
}

// StartWrite starts every shard writer. Like the upstream cluster writer it
// returns nil: entries are pushed through Write, not through a shared channel.
func (w *StaticClusterWriter) StartWrite(ctx context.Context) chan *entry.Entry {
	for _, sw := range w.writers {
		sw.StartWrite(ctx)
	}
	return nil
}

// Shard returns the shard index that owns the Codis slot of key.
func (w *StaticClusterWriter) Shard(key string) int {
	return w.router.Shard(CodisSlot(key))
}

// Write routes entry to the owning shard. Entries without keys (PING, FLUSHALL,
// SELECT...) are fanned out to every shard, matching RedisClusterWriter. Keys
// of a multi-key command must belong to the same shard, otherwise the write
// is rejected the same way upstream rejects CROSSSLOT.
func (w *StaticClusterWriter) Write(e *entry.Entry) {
	if len(e.Keys) == 0 {
		for _, sw := range w.writers {
			sw.Write(e)
		}
		return
	}
	last := -1
	for _, key := range e.Keys {
		if idx := w.Shard(key); last == -1 {
			last = idx
		} else if idx != last {
			log.Panicf("CROSSSLOT Keys in request don't hash to the same Codis shard. argv=%v", e.Argv)
		}
	}
	w.writers[last].Write(e)
}

func (w *StaticClusterWriter) Close() {
	for _, sw := range w.writers {
		sw.Close()
	}
}

func (w *StaticClusterWriter) Status() interface{} {
	w.stat = make([]interface{}, 0, len(w.writers))
	for _, sw := range w.writers {
		w.stat = append(w.stat, sw.Status())
	}
	return w.stat
}

func (w *StaticClusterWriter) StatusString() string {
	return fmt.Sprintf("[codis_static_writer] writing to %d shards", len(w.writers))
}

func (w *StaticClusterWriter) StatusConsistent() bool {
	for _, sw := range w.writers {
		if !sw.StatusConsistent() {
			return false
		}
	}
	return true
}
