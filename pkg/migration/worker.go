// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mcuadros/go-defaults"

	"github.com/CodisLabs/codis/pkg/shake/config"
	"github.com/CodisLabs/codis/pkg/shake/entry"
	"github.com/CodisLabs/codis/pkg/shake/filter"
	shakelog "github.com/CodisLabs/codis/pkg/shake/log"
	"github.com/CodisLabs/codis/pkg/shake/reader"
	"github.com/CodisLabs/codis/pkg/shake/status"
	"github.com/CodisLabs/codis/pkg/shake/utils"
	"github.com/CodisLabs/codis/pkg/shake/writer"
)

// LogFileName is the RedisShake log file written inside the job directory.
const LogFileName = "shake.log"

// SpecFileName is the spec file name inside the job directory.
const SpecFileName = "spec.json"

// StatusFileName is the last status document the worker captures right before
// it exits. A one-shot task can drain before codis-dashboard polls the status
// port the first time, so the manager falls back to this file to report the
// final counters instead of an empty stats block.
const StatusFileName = "status.json"

// LoadSpec reads a spec written by codis-dashboard.
func LoadSpec(path string) (*Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec %s: %w", path, err)
	}
	var spec Spec
	if err := json.Unmarshal(b, &spec); err != nil {
		return nil, fmt.Errorf("decode spec %s: %w", path, err)
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid spec %s: %w", path, err)
	}
	return &spec, nil
}

// Run executes one migration inside a dedicated codis-shake process. The
// pipeline is the upstream cmd/redis-shake/main.go flow (reader -> filter ->
// lua function -> writer, with status counting, periodic logging and the PING
// keepalive); only reader / writer construction differs to support the static
// Codis topology. It returns nil when ctx is cancelled (graceful stop) or a
// one-shot task drained, and an error when the spec is invalid or a reader /
// writer cannot be built. Runtime failures inside RedisShake still go through
// its log.Panicf -> os.Exit(1) path; the dashboard reports the exit code.
func Run(ctx context.Context, spec *Spec) error {
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("invalid spec: %w", err)
	}
	config.Opt = spec.ShakeOptions()
	advanced := config.Opt.Advanced

	shakelog.Init(advanced.LogLevel, LogFileName, advanced.Dir,
		false, 0, 0, 0, false)
	utils.ChdirAndAcquireFileLock()
	defer utils.ReleaseFileLock()
	utils.SetNcpu()
	luaRuntime := filter.NewFunctionFilter(config.Opt.Filter.Function)
	shakelog.Infof("codis-shake start. id=[%s] name=[%s] mode=[%s] source=[%s/%s] target=[%s/%s]",
		spec.ID, spec.Name, spec.Mode, spec.Source.Kind, endpointSummary(spec.Source),
		spec.Target.Kind, endpointSummary(spec.Target))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if spec.ParentPID > 0 {
		go watchParent(ctx, cancel, spec.ParentPID)
	}

	theReader, err := buildReader(ctx, spec)
	if err != nil {
		return fmt.Errorf("build reader failed: %w", err)
	}
	theWriter, err := buildWriter(ctx, spec)
	if err != nil {
		return fmt.Errorf("build writer failed: %w", err)
	}
	if advanced.StatusPort != 0 {
		status.Init(theReader, theWriter)
	}
	if advanced.EmptyDBBeforeSync {
		e := entry.NewEntry()
		e.Argv = []string{"FLUSHALL"}
		theWriter.Write(e)
		shakelog.Warnf("empty_db_before_sync enabled: FLUSHALL sent to the target before syncing")
	}

	shakelog.Infof("start syncing...")

	chrs := theReader.StartRead(ctx)
	theWriter.StartWrite(ctx)

	readerDone := make(chan bool)
	var logEntryCount status.EntryCount

	for _, chr := range chrs {
		go func(ch chan *entry.Entry) {
			for e := range ch {
				e.Parse()
				if advanced.StatusPort != 0 {
					status.AddReadCount(e.CmdName)
				}
				atomic.AddUint64(&logEntryCount.ReadCount, 1)

				if !filter.Filter(e) {
					shakelog.Debugf("skip command: %v", e)
					continue
				}
				entries := luaRuntime.RunFunction(e)
				for _, theEntry := range entries {
					theEntry.Parse()
					theWriter.Write(theEntry)
					if advanced.StatusPort != 0 {
						status.AddWriteCount(theEntry.CmdName)
					}
					atomic.AddUint64(&logEntryCount.WriteCount, 1)
				}
			}
			readerDone <- true
		}(chr)
	}

	go func() {
		if advanced.LogInterval <= 0 {
			shakelog.Infof("log interval is 0, will not log to screen")
			return
		}
		ticker := time.NewTicker(time.Duration(advanced.LogInterval) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			logEntryCount.UpdateOPS()
			shakelog.Infof("%s, %s", logEntryCount.String(), theReader.StatusString())
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	readerCnt := len(chrs)
Loop:
	for {
		select {
		case done := <-readerDone:
			if done {
				readerCnt--
			}
			if readerCnt == 0 {
				break Loop
			}
		case <-ticker.C:
			pingEntry := entry.NewEntry()
			pingEntry.DbId = 0
			pingEntry.CmdName = "PING"
			pingEntry.Argv = []string{"PING"}
			pingEntry.Group = "connection"
			theWriter.Write(pingEntry)
		}
	}

	theWriter.Close()
	captureFinalStatus(advanced.Dir, advanced.StatusPort, theReader, theWriter)
	shakelog.Infof("all done")
	return nil
}

// captureFinalStatus asks the loopback status server for one last snapshot and
// writes it next to the spec. The server lives in this process, so it is still
// reachable after the pipeline drained; the dashboard reads the file when a
// one-shot task finished before its first poll. The status ticker refreshes the
// reader / writer sections only once per second, so a short task would persist
// nulls there: fill them from the live objects.
func captureFinalStatus(dir string, port int, r, w status.Statusable) {
	if port == 0 {
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		shakelog.Warnf("capture final status from port %d failed: %v", port, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		shakelog.Warnf("capture final status from port %d: %s", port, resp.Status)
		return
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		shakelog.Warnf("capture final status from port %d failed: %v", port, err)
		return
	}
	doc := make(map[string]json.RawMessage)
	if err := json.Unmarshal(b, &doc); err != nil {
		shakelog.Warnf("decode final status from port %d failed: %v", port, err)
		return
	}
	if r != nil {
		if raw, err := json.Marshal(r.Status()); err == nil {
			doc["reader"] = raw
		}
	}
	if w != nil {
		if raw, err := json.Marshal(w.Status()); err == nil {
			doc["writer"] = raw
		}
	}
	b, err = json.Marshal(doc)
	if err != nil {
		shakelog.Warnf("encode final status failed: %v", err)
		return
	}
	path := filepath.Join(dir, StatusFileName)
	if err := os.WriteFile(path, b, 0644); err != nil {
		shakelog.Warnf("write final status %s failed: %v", path, err)
	}
}

func endpointSummary(e Endpoint) string {
	switch e.Kind {
	case EndpointStatic:
		return fmt.Sprintf("%d shards", len(e.Shards))
	default:
		return e.Address
	}
}

func buildReader(ctx context.Context, spec *Spec) (reader.Reader, error) {
	src := spec.Source
	switch spec.Mode {
	case ModeScan:
		opts := new(reader.ScanReaderOptions)
		defaults.SetDefaults(opts)
		opts.Cluster = src.Kind == EndpointCluster
		opts.Address = src.Address
		opts.Username = src.Username
		opts.Password = src.Password
		opts.Tls = src.TLS
		opts.PreferReplica = src.PreferReplica
		switch src.Kind {
		case EndpointStandalone:
			return reader.NewScanStandaloneReader(ctx, opts), nil
		case EndpointCluster:
			return reader.NewScanClusterReader(ctx, opts), nil
		case EndpointStatic:
			return NewStaticScanReader(ctx, opts, shardAddresses(src))
		}
	case ModeSync, ModeRDB:
		opts := new(reader.SyncReaderOptions)
		defaults.SetDefaults(opts)
		opts.Cluster = src.Kind == EndpointCluster
		opts.Address = src.Address
		opts.Username = src.Username
		opts.Password = src.Password
		opts.Tls = src.TLS
		opts.SyncRdb = true
		opts.SyncAof = spec.Mode == ModeSync
		opts.PreferReplica = src.PreferReplica
		switch src.Kind {
		case EndpointStandalone:
			return reader.NewSyncStandaloneReader(ctx, opts), nil
		case EndpointCluster:
			return reader.NewSyncClusterReader(ctx, opts), nil
		case EndpointStatic:
			return NewStaticSyncReader(ctx, opts, shardAddresses(src))
		}
	}
	return nil, fmt.Errorf("unsupported mode %q with source kind %q", spec.Mode, src.Kind)
}

func buildWriter(ctx context.Context, spec *Spec) (writer.Writer, error) {
	dst := spec.Target
	if dst.Kind == EndpointStatic {
		opts := new(writer.RedisWriterOptions)
		defaults.SetDefaults(opts)
		opts.Username = dst.Username
		opts.Password = dst.Password
		opts.Tls = dst.TLS
		return NewStaticClusterWriter(ctx, opts, dst.Shards)
	}
	opts := new(writer.RedisWriterOptions)
	defaults.SetDefaults(opts)
	opts.Cluster = dst.Kind == EndpointCluster
	opts.Address = dst.Address
	opts.Username = dst.Username
	opts.Password = dst.Password
	opts.Tls = dst.TLS
	if opts.Cluster {
		return writer.NewRedisClusterWriter(ctx, opts), nil
	}
	return writer.NewRedisStandaloneWriter(ctx, opts), nil
}

func shardAddresses(e Endpoint) []string {
	addresses := make([]string, 0, len(e.Shards))
	for _, shard := range e.Shards {
		addresses = append(addresses, shard.Address)
	}
	return addresses
}

// watchParent stops the worker when the dashboard that started it is gone.
func watchParent(ctx context.Context, cancel context.CancelFunc, pid int) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !processAlive(pid) {
				shakelog.Warnf("parent process %d is gone, stopping the migration", pid)
				cancel()
				return
			}
		}
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return true
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
