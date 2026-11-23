// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// Package migration is the Codis side of the data integration console. It
// defines the JSON spec that codis-dashboard hands to a codis-shake worker
// process, the Codis (static shard topology) reader / writer compositions built
// on top of the vendored RedisShake packages, and the worker pipeline itself.
package migration

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/shake/config"
)

// Endpoint kinds accepted by Spec.Source / Spec.Target.
const (
	// EndpointStandalone is a single Redis instance or a master-replica pair.
	EndpointStandalone = "standalone"
	// EndpointCluster is a Redis Cluster; topology is discovered with CLUSTER NODES.
	EndpointCluster = "cluster"
	// EndpointStatic is an explicit shard list (Codis, or any sharded Redis
	// deployment without a cluster bus). Targets additionally carry the Codis
	// slot ranges owned by every shard.
	EndpointStatic = "static"
)

// Migration modes.
const (
	// ModeSync replicates the RDB snapshot and then streams the AOF until the
	// task is stopped. It is the "全量 + 增量" mode used for cutover.
	ModeSync = "sync"
	// ModeScan walks the keyspace with SCAN + DUMP and finishes on its own.
	ModeScan = "scan"
	// ModeRDB transfers only the RDB snapshot and finishes on its own.
	ModeRDB = "rdb"
)

// Restore behaviours for "Target key name is busy" RESTORE replies.
const (
	RestoreRewrite = "rewrite"
	RestoreSkip    = "skip"
	RestorePanic   = "panic"
)

// SlotRange is an inclusive Codis slot range owned by one shard.
type SlotRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Shard is one server of a static endpoint.
type Shard struct {
	Address string      `json:"address"`
	Slots   []SlotRange `json:"slots,omitempty"`
}

// Endpoint describes one side of a migration.
type Endpoint struct {
	Kind     string  `json:"kind"`
	Address  string  `json:"address,omitempty"`
	Username string  `json:"username,omitempty"`
	Password string  `json:"password,omitempty"`
	TLS      bool    `json:"tls,omitempty"`
	Shards   []Shard `json:"shards,omitempty"`

	// PreferReplica reads from a replica instead of the master (sync mode).
	PreferReplica bool `json:"prefer_replica,omitempty"`
}

// Filter is the console-facing subset of RedisShake's filter options.
type Filter struct {
	AllowKeyPrefix []string `json:"allow_key_prefix,omitempty"`
	BlockKeyPrefix []string `json:"block_key_prefix,omitempty"`
	AllowDB        []int    `json:"allow_db,omitempty"`
	BlockDB        []int    `json:"block_db,omitempty"`
	AllowCommand   []string `json:"allow_command,omitempty"`
	BlockCommand   []string `json:"block_command,omitempty"`
}

// Advanced holds the RedisShake advanced options the console exposes.
type Advanced struct {
	RestoreBehavior    string `json:"restore_behavior,omitempty"`
	TargetRedisMaxQPS  int    `json:"target_redis_max_qps,omitempty"`
	PipelineCountLimit int    `json:"pipeline_count_limit,omitempty"`
	EmptyDBBeforeSync  bool   `json:"empty_db_before_sync,omitempty"`
	Ncpu               int    `json:"ncpu,omitempty"`
	StatusPort         int    `json:"status_port,omitempty"`
	LogLevel           string `json:"log_level,omitempty"`
	// Dir is the RedisShake working directory. codis-shake already runs inside
	// the job directory, so the dashboard leaves it empty (meaning "."). Tests
	// point it at a temp directory.
	Dir string `json:"dir,omitempty"`
}

// Spec is the complete description of one migration task. codis-dashboard
// writes it as spec.json into the job directory and codis-shake executes it.
type Spec struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Mode     string   `json:"mode"`
	Source   Endpoint `json:"source"`
	Target   Endpoint `json:"target"`
	Filter   Filter   `json:"filter"`
	Advanced Advanced `json:"advanced"`

	// ParentPID is the codis-dashboard pid. The worker stops itself when the
	// parent disappears so a crashed dashboard never leaves orphan workers.
	ParentPID int `json:"parent_pid,omitempty"`
}

// ShakeOptions converts the spec into the RedisShake global option set used by
// the vendored packages.
func (s *Spec) ShakeOptions() config.ShakeOptions {
	opt := config.Default()
	f := &opt.Filter
	f.AllowKeyPrefix = append([]string(nil), s.Filter.AllowKeyPrefix...)
	f.BlockKeyPrefix = append([]string(nil), s.Filter.BlockKeyPrefix...)
	f.AllowDB = append([]int(nil), s.Filter.AllowDB...)
	f.BlockDB = append([]int(nil), s.Filter.BlockDB...)
	f.AllowCommand = append([]string(nil), s.Filter.AllowCommand...)
	f.BlockCommand = append([]string(nil), s.Filter.BlockCommand...)

	a := &opt.Advanced
	if s.Advanced.RestoreBehavior != "" {
		a.RDBRestoreCommandBehavior = s.Advanced.RestoreBehavior
	}
	if s.Advanced.TargetRedisMaxQPS > 0 {
		a.TargetRedisMaxQPS = s.Advanced.TargetRedisMaxQPS
	}
	if s.Advanced.PipelineCountLimit > 0 {
		a.PipelineCountLimit = uint64(s.Advanced.PipelineCountLimit)
	}
	a.EmptyDBBeforeSync = s.Advanced.EmptyDBBeforeSync
	a.Ncpu = s.Advanced.Ncpu
	a.StatusPort = s.Advanced.StatusPort
	if s.Advanced.LogLevel != "" {
		a.LogLevel = s.Advanced.LogLevel
	}
	// The worker always runs inside its own job directory unless the spec
	// names another working directory.
	a.Dir = s.Advanced.Dir
	if a.Dir == "" {
		a.Dir = "."
	}
	return opt
}

// IsStaticTarget reports whether the target uses the Codis slot router.
func (s *Spec) IsStaticTarget() bool {
	return s.Target.Kind == EndpointStatic
}

// OneShot reports whether the task finishes on its own (scan / rdb modes).
func (s *Spec) OneShot() bool {
	return s.Mode == ModeScan || s.Mode == ModeRDB
}

// Validate checks the spec the same way in the dashboard (before spawning) and
// in the worker (before touching any Redis).
func (s *Spec) Validate() error {
	if s == nil {
		return fmt.Errorf("nil spec")
	}
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("missing id")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("missing name")
	}
	switch s.Mode {
	case ModeSync, ModeScan, ModeRDB:
	default:
		return fmt.Errorf("invalid mode %q", s.Mode)
	}
	if err := validateEndpoint("source", s.Source, false); err != nil {
		return err
	}
	if err := validateEndpoint("target", s.Target, true); err != nil {
		return err
	}
	switch s.Advanced.RestoreBehavior {
	case "", RestoreRewrite, RestoreSkip, RestorePanic:
	default:
		return fmt.Errorf("invalid restore_behavior %q", s.Advanced.RestoreBehavior)
	}
	if s.Advanced.TargetRedisMaxQPS < 0 || s.Advanced.PipelineCountLimit < 0 || s.Advanced.Ncpu < 0 {
		return fmt.Errorf("advanced limits must not be negative")
	}
	if s.Advanced.StatusPort < 0 || s.Advanced.StatusPort > 65535 {
		return fmt.Errorf("invalid status_port %d", s.Advanced.StatusPort)
	}
	switch s.Advanced.LogLevel {
	case "", "debug", "info", "warn":
	default:
		return fmt.Errorf("invalid log_level %q", s.Advanced.LogLevel)
	}
	return nil
}

func validateEndpoint(side string, e Endpoint, requireSlots bool) error {
	switch e.Kind {
	case EndpointStandalone, EndpointCluster:
		if err := validateAddress(e.Address); err != nil {
			return fmt.Errorf("%s address: %w", side, err)
		}
		if len(e.Shards) != 0 {
			return fmt.Errorf("%s: shards are only valid for kind %q", side, EndpointStatic)
		}
	case EndpointStatic:
		if e.Address != "" {
			return fmt.Errorf("%s: address is not used for kind %q, use shards", side, EndpointStatic)
		}
		if len(e.Shards) == 0 {
			return fmt.Errorf("%s: at least one shard is required", side)
		}
		seen := make(map[string]bool, len(e.Shards))
		for i, shard := range e.Shards {
			if err := validateAddress(shard.Address); err != nil {
				return fmt.Errorf("%s shard %d: %w", side, i, err)
			}
			if seen[shard.Address] {
				return fmt.Errorf("%s: duplicate shard address %s", side, shard.Address)
			}
			seen[shard.Address] = true
		}
		if requireSlots {
			if err := ValidateSlotCoverage(e.Shards); err != nil {
				return fmt.Errorf("%s: %w", side, err)
			}
		}
	default:
		return fmt.Errorf("%s: invalid kind %q", side, e.Kind)
	}
	return nil
}

// ValidateSlotCoverage verifies that the shard slot ranges are well formed,
// do not overlap and cover every Codis slot exactly once.
func ValidateSlotCoverage(shards []Shard) error {
	owner := make([]int, models.MaxSlotNum)
	for i := range owner {
		owner[i] = -1
	}
	for i, shard := range shards {
		if len(shard.Slots) == 0 {
			return fmt.Errorf("shard %s has no slot range", shard.Address)
		}
		for _, r := range shard.Slots {
			if r.Start < 0 || r.End >= models.MaxSlotNum || r.Start > r.End {
				return fmt.Errorf("shard %s: invalid slot range %d-%d (slots are 0-%d)",
					shard.Address, r.Start, r.End, models.MaxSlotNum-1)
			}
			for sid := r.Start; sid <= r.End; sid++ {
				if owner[sid] != -1 {
					return fmt.Errorf("slot %d is owned by both %s and %s",
						sid, shards[owner[sid]].Address, shard.Address)
				}
				owner[sid] = i
			}
		}
	}
	for sid, o := range owner {
		if o == -1 {
			return fmt.Errorf("slot %d is not covered by any shard", sid)
		}
	}
	return nil
}

func validateAddress(addr string) error {
	if strings.TrimSpace(addr) == "" {
		return fmt.Errorf("missing address")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("%q is not host:port", addr)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return fmt.Errorf("%q has a non-numeric port", addr)
	}
	return nil
}

// Redacted returns a copy of the endpoint with the password replaced by a
// HasPassword marker; it is what the API and the persisted history expose.
type EndpointView struct {
	Kind        string  `json:"kind"`
	Address     string  `json:"address,omitempty"`
	Username    string  `json:"username,omitempty"`
	HasPassword bool    `json:"has_password"`
	TLS         bool    `json:"tls,omitempty"`
	Shards      []Shard `json:"shards,omitempty"`
}

func (e Endpoint) View() EndpointView {
	return EndpointView{
		Kind:        e.Kind,
		Address:     e.Address,
		Username:    e.Username,
		HasPassword: e.Password != "",
		TLS:         e.TLS,
		Shards:      append([]Shard(nil), e.Shards...),
	}
}

// SlotRouter maps a Codis slot to the shard index owning it.
type SlotRouter struct {
	owner []int
}

// NewSlotRouter builds the router from shard slot ranges. It returns an error
// when the ranges are not a complete, non-overlapping cover of 0..MaxSlotNum-1.
func NewSlotRouter(shards []Shard) (*SlotRouter, error) {
	if err := ValidateSlotCoverage(shards); err != nil {
		return nil, err
	}
	r := &SlotRouter{owner: make([]int, models.MaxSlotNum)}
	for i, shard := range shards {
		for _, rng := range shard.Slots {
			for sid := rng.Start; sid <= rng.End; sid++ {
				r.owner[sid] = i
			}
		}
	}
	return r, nil
}

// Shard returns the index of the shard owning slot.
func (r *SlotRouter) Shard(slot int) int {
	return r.owner[slot]
}
