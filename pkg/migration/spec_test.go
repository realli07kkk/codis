// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validSpec() *Spec {
	return &Spec{
		ID:   "job-1",
		Name: "order cluster to codis",
		Mode: ModeSync,
		Source: Endpoint{
			Kind:     EndpointCluster,
			Address:  "10.0.0.1:6379",
			Password: "secret",
		},
		Target: Endpoint{
			Kind: EndpointStatic,
			Shards: []Shard{
				{Address: "10.0.1.1:6379", Slots: []SlotRange{{Start: 0, End: 511}}},
				{Address: "10.0.1.2:6379", Slots: []SlotRange{{Start: 512, End: 1023}}},
			},
		},
		Filter:   Filter{BlockCommand: []string{"FLUSHALL", "FLUSHDB"}},
		Advanced: Advanced{RestoreBehavior: RestoreRewrite, TargetRedisMaxQPS: 100000, StatusPort: 18090},
	}
}

func TestSpecValidateAcceptsSupportedShapes(t *testing.T) {
	spec := validSpec()
	if err := spec.Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	if spec.OneShot() {
		t.Fatal("sync mode must not be one-shot")
	}
	if !spec.IsStaticTarget() {
		t.Fatal("static target not detected")
	}

	for _, mode := range []string{ModeScan, ModeRDB} {
		spec.Mode = mode
		if err := spec.Validate(); err != nil {
			t.Fatalf("mode %s rejected: %v", mode, err)
		}
		if !spec.OneShot() {
			t.Fatalf("mode %s must be one-shot", mode)
		}
	}

	standalone := validSpec()
	standalone.Source = Endpoint{Kind: EndpointStandalone, Address: "127.0.0.1:6379"}
	standalone.Target = Endpoint{Kind: EndpointStandalone, Address: "127.0.0.1:6380"}
	if err := standalone.Validate(); err != nil {
		t.Fatalf("standalone to standalone rejected: %v", err)
	}

	staticSourceWithoutSlots := validSpec()
	staticSourceWithoutSlots.Source = Endpoint{
		Kind:   EndpointStatic,
		Shards: []Shard{{Address: "a:1"}, {Address: "b:2"}},
	}
	if err := staticSourceWithoutSlots.Validate(); err != nil {
		t.Fatalf("static source without slot ranges rejected: %v", err)
	}
}

func TestSpecValidateRejectsBrokenInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Spec)
		want   string
	}{
		{"missing id", func(s *Spec) { s.ID = "" }, "missing id"},
		{"missing name", func(s *Spec) { s.Name = " " }, "missing name"},
		{"bad mode", func(s *Spec) { s.Mode = "copy" }, `invalid mode "copy"`},
		{"bad source kind", func(s *Spec) { s.Source.Kind = "codis" }, `source: invalid kind "codis"`},
		{"missing source address", func(s *Spec) { s.Source.Address = "" }, "source address"},
		{"bad source address", func(s *Spec) { s.Source.Address = "10.0.0.1" }, "source address"},
		{"non numeric port", func(s *Spec) { s.Source.Address = "10.0.0.1:abc" }, "source address"},
		{"shards on cluster source", func(s *Spec) { s.Source.Shards = []Shard{{Address: "a:1"}} }, "shards are only valid"},
		{"static without shards", func(s *Spec) { s.Target.Shards = nil }, "at least one shard"},
		{"static with address", func(s *Spec) { s.Target.Address = "a:1" }, "address is not used"},
		{"duplicate shard", func(s *Spec) {
			s.Target.Shards[1].Address = s.Target.Shards[0].Address
		}, "duplicate shard"},
		{"static target uncovered slot", func(s *Spec) {
			s.Target.Shards[1].Slots = []SlotRange{{Start: 600, End: 1023}}
		}, "slot 512 is not covered"},
		{"static target overlapping slot", func(s *Spec) {
			s.Target.Shards[1].Slots = []SlotRange{{Start: 500, End: 1023}}
		}, "slot 500 is owned by both"},
		{"static target bad range", func(s *Spec) {
			s.Target.Shards[1].Slots = []SlotRange{{Start: 512, End: 1024}}
		}, "invalid slot range 512-1024"},
		{"bad restore behavior", func(s *Spec) { s.Advanced.RestoreBehavior = "ignore" }, `invalid restore_behavior "ignore"`},
		{"bad status port", func(s *Spec) { s.Advanced.StatusPort = 70000 }, "invalid status_port"},
		{"bad log level", func(s *Spec) { s.Advanced.LogLevel = "verbose" }, `invalid log_level "verbose"`},
		{"negative qps", func(s *Spec) { s.Advanced.TargetRedisMaxQPS = -1 }, "must not be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			tc.mutate(spec)
			err := spec.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestShakeOptionsApplySpec(t *testing.T) {
	spec := validSpec()
	spec.Advanced.PipelineCountLimit = 512
	spec.Advanced.EmptyDBBeforeSync = true
	spec.Advanced.LogLevel = "debug"
	spec.Advanced.Dir = "/tmp/job"
	opt := spec.ShakeOptions()

	if opt.Advanced.RDBRestoreCommandBehavior != RestoreRewrite {
		t.Fatalf("restore behavior = %q", opt.Advanced.RDBRestoreCommandBehavior)
	}
	if opt.Advanced.TargetRedisMaxQPS != 100000 {
		t.Fatalf("max qps = %d", opt.Advanced.TargetRedisMaxQPS)
	}
	if opt.Advanced.PipelineCountLimit != 512 {
		t.Fatalf("pipeline limit = %d", opt.Advanced.PipelineCountLimit)
	}
	if !opt.Advanced.EmptyDBBeforeSync {
		t.Fatal("empty_db_before_sync not applied")
	}
	if opt.Advanced.StatusPort != 18090 {
		t.Fatalf("status port = %d", opt.Advanced.StatusPort)
	}
	if opt.Advanced.LogLevel != "debug" {
		t.Fatalf("log level = %q", opt.Advanced.LogLevel)
	}
	if opt.Advanced.Dir != "/tmp/job" {
		t.Fatalf("dir = %q", opt.Advanced.Dir)
	}
	if len(opt.Filter.BlockCommand) != 2 || opt.Filter.BlockCommand[0] != "FLUSHALL" {
		t.Fatalf("block command = %v", opt.Filter.BlockCommand)
	}

	empty := (&Spec{}).ShakeOptions()
	if empty.Advanced.Dir != "." {
		t.Fatalf("default dir = %q, want .", empty.Advanced.Dir)
	}
	if empty.Advanced.RDBRestoreCommandBehavior != RestorePanic {
		t.Fatalf("default restore behavior = %q, want upstream default panic", empty.Advanced.RDBRestoreCommandBehavior)
	}
	if empty.Advanced.TargetRedisMaxQPS != 300000 || empty.Advanced.PipelineCountLimit != 1024 {
		t.Fatalf("upstream defaults not preserved: %+v", empty.Advanced)
	}
}

func TestEndpointViewRedactsPassword(t *testing.T) {
	view := Endpoint{Kind: EndpointStandalone, Address: "a:1", Username: "u", Password: "p"}.View()
	if view.HasPassword != true {
		t.Fatal("has_password must be true")
	}
	b, _ := json.Marshal(view)
	if strings.Contains(string(b), `"p"`) {
		t.Fatalf("password leaked in view: %s", b)
	}
	plain := Endpoint{Kind: EndpointStandalone, Address: "a:1"}.View()
	if plain.HasPassword {
		t.Fatal("has_password must be false without password")
	}
}

func TestLoadSpecRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SpecFileName)
	spec := validSpec()
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSpec(path)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if loaded.ID != spec.ID || loaded.Source.Password != "secret" || len(loaded.Target.Shards) != 2 {
		t.Fatalf("loaded spec mismatch: %+v", loaded)
	}

	if _, err := LoadSpec(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("expected error for missing spec")
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSpec(filepath.Join(dir, "broken.json")); err == nil {
		t.Fatal("expected error for broken json")
	}
	if err := os.WriteFile(filepath.Join(dir, "invalid.json"), []byte(`{"id":"x"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSpec(filepath.Join(dir, "invalid.json")); err == nil || !strings.Contains(err.Error(), "invalid spec") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestSlotRouter(t *testing.T) {
	shards := []Shard{
		{Address: "a:1", Slots: []SlotRange{{Start: 0, End: 3}, {Start: 8, End: 10}}},
		{Address: "b:2", Slots: []SlotRange{{Start: 4, End: 7}}},
		{Address: "c:3", Slots: []SlotRange{{Start: 11, End: 1023}}},
	}
	if err := ValidateSlotCoverage(shards); err != nil {
		t.Fatalf("coverage rejected: %v", err)
	}
	router, err := NewSlotRouter(shards)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]int{0: 0, 3: 0, 4: 1, 7: 1, 8: 0, 10: 0, 11: 2, 1023: 2}
	for slot, idx := range want {
		if got := router.Shard(slot); got != idx {
			t.Fatalf("slot %d -> shard %d, want %d", slot, got, idx)
		}
	}
	// Coverage is mandatory: 11 of 1024 slots is not enough.
	if _, err := NewSlotRouter([]Shard{{Address: "a:1", Slots: []SlotRange{{Start: 0, End: 10}}}}); err == nil ||
		!strings.Contains(err.Error(), "not covered") {
		t.Fatalf("expected uncovered slots error, got %v", err)
	}
}
