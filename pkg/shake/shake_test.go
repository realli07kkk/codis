package shake_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CodisLabs/codis/pkg/shake/commands"
	"github.com/CodisLabs/codis/pkg/shake/config"
	"github.com/CodisLabs/codis/pkg/shake/entry"
	"github.com/CodisLabs/codis/pkg/shake/filter"
	"github.com/CodisLabs/codis/pkg/shake/status"
	"github.com/CodisLabs/codis/pkg/shake/utils"
)

const clusterSlotCount = 16384

// TestConfigDefaults pins the struct defaults applied by config.Default(), which
// replaces the defaults.SetDefaults(&Opt) call upstream ran before reading its
// TOML config.
func TestConfigDefaults(t *testing.T) {
	opt := config.Default()

	strDefaults := []struct {
		name string
		got  string
		want string
	}{
		{"Advanced.Dir", opt.Advanced.Dir, "data"},
		{"Advanced.LogFile", opt.Advanced.LogFile, "shake.log"},
		{"Advanced.LogLevel", opt.Advanced.LogLevel, "info"},
		{"Advanced.RDBRestoreCommandBehavior", opt.Advanced.RDBRestoreCommandBehavior, "panic"},
	}
	for _, tt := range strDefaults {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}

	numDefaults := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"Advanced.LogInterval", uint64(opt.Advanced.LogInterval), 5},
		{"Advanced.PipelineCountLimit", opt.Advanced.PipelineCountLimit, 1024},
		{"Advanced.TargetRedisMaxQPS", uint64(opt.Advanced.TargetRedisMaxQPS), 300000},
		{"Advanced.TargetRedisProtoMaxBulkLen", opt.Advanced.TargetRedisProtoMaxBulkLen, 512000000},
	}
	for _, tt := range numDefaults {
		if tt.got != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}

	if opt.Advanced.EmptyDBBeforeSync {
		t.Errorf("Advanced.EmptyDBBeforeSync = true, want false")
	}

	// The `default:"[]"` tags must leave every filter slice non-nil, so that
	// "no filter configured" is distinguishable from "defaults not applied".
	filterValue := reflect.ValueOf(opt.Filter)
	sliceFields := 0
	for i := 0; i < filterValue.NumField(); i++ {
		field := filterValue.Field(i)
		if field.Kind() != reflect.Slice {
			continue
		}
		sliceFields++
		name := filterValue.Type().Field(i).Name
		if field.IsNil() {
			t.Errorf("Filter.%s is nil, want an empty slice", name)
			continue
		}
		if field.Len() != 0 {
			t.Errorf("Filter.%s = %v, want an empty slice", name, field.Interface())
		}
	}
	if want := 16; sliceFields != want {
		t.Fatalf("FilterOptions has %d slice fields, want %d", sliceFields, want)
	}
}

// TestCalcKeys pins the key-spec table for single- and multi-key string
// commands.
func TestCalcKeys(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		wantCmd   string
		wantGroup string
		wantKeys  []string
	}{
		{"set", []string{"set", "user:1", "x"}, "SET", "STRING", []string{"user:1"}},
		{"mset", []string{"mset", "{tag}.a", "1", "{tag}.b", "2"}, "MSET", "STRING", []string{"{tag}.a", "{tag}.b"}},
		{"get", []string{"get", "k"}, "GET", "STRING", []string{"k"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdName, group, keys, _ := commands.CalcKeys(tt.argv)
			if cmdName != tt.wantCmd {
				t.Errorf("CalcKeys(%v) cmd = %q, want %q", tt.argv, cmdName, tt.wantCmd)
			}
			if group != tt.wantGroup {
				t.Errorf("CalcKeys(%v) group = %q, want %q", tt.argv, group, tt.wantGroup)
			}
			if !slices.Equal(keys, tt.wantKeys) {
				t.Errorf("CalcKeys(%v) keys = %v, want %v", tt.argv, keys, tt.wantKeys)
			}
		})
	}
}

// TestCalcSlots pins that a hash tag collapses multi-key commands onto one
// cluster slot and that plain keys stay inside the slot space.
func TestCalcSlots(t *testing.T) {
	tagged := commands.CalcSlots([]string{"{tag}.a", "{tag}.b"})
	if tagged[0] != tagged[1] {
		t.Fatalf("CalcSlots([{tag}.a {tag}.b]) = %v, want both keys in the same slot", tagged)
	}

	untagged := commands.CalcSlots([]string{"user:1", "user:2"})
	if untagged[0] == untagged[1] {
		t.Fatalf("CalcSlots([user:1 user:2]) = %v, want distinct slots", untagged)
	}

	for _, slots := range [][]int{tagged, untagged} {
		for i, slot := range slots {
			if slot < 0 || slot >= clusterSlotCount {
				t.Fatalf("slot[%d] = %d, want 0..%d", i, slot, clusterSlotCount-1)
			}
		}
	}
}

// TestChecksums pins the CRC16 vendor check value documented in crc16.go
// (XMODEM parameters, output 0x31C3 for "123456789") and the determinism of
// CalcCRC64.
func TestChecksums(t *testing.T) {
	if got, want := utils.Crc16("123456789"), uint16(0x31C3); got != want {
		t.Errorf("Crc16(123456789) = %#04x, want %#04x", got, want)
	}

	input := []byte("0123456789abcdef")
	first := utils.CalcCRC64(input)
	if second := utils.CalcCRC64(input); first != second {
		t.Errorf("CalcCRC64(%q) is not deterministic: %#x != %#x", input, first, second)
	}
	if first == 0 {
		t.Errorf("CalcCRC64(%q) = 0, want a non-zero digest", input)
	}
}

// TestFilterBlockKeyPrefix exercises the pure in-memory key filter: entries
// whose only key carries a blocked prefix are dropped, everything else passes.
func TestFilterBlockKeyPrefix(t *testing.T) {
	saved := config.Opt
	t.Cleanup(func() { config.Opt = saved })

	config.Opt = config.Default()
	config.Opt.Filter.BlockKeyPrefix = []string{"tmp:"}

	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{"blocked prefix", []string{"SET", "tmp:1", "v"}, false},
		{"allowed key", []string{"SET", "user:1", "v"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := entry.NewEntry()
			e.Argv = tt.argv
			e.Parse()
			if got := filter.Filter(e); got != tt.want {
				t.Fatalf("filter.Filter(%v) = %v, want %v", tt.argv, got, tt.want)
			}
		})
	}
}

// TestEntryCountString pins the status line format used by the dashboard.
func TestEntryCountString(t *testing.T) {
	var count status.EntryCount
	if got := count.String(); !strings.Contains(got, "read_count=[") {
		t.Fatalf("EntryCount.String() = %q, want it to contain read_count=[", got)
	}
}
