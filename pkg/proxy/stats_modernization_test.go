// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"slices"
	"testing"
)

func TestSortOpStatsOrdersByOpStr(t *testing.T) {
	all := []*OpStats{
		{OpStr: "SET"},
		{OpStr: "GET"},
		{OpStr: "DEL"},
		{OpStr: "GET"},
		{OpStr: "MGET"},
		{OpStr: "AUTH"},
	}

	sortOpStats(all)

	want := make([]string, len(all))
	for i, s := range all {
		want[i] = s.OpStr
	}
	slices.Sort(want)
	for i, s := range all {
		if s.OpStr != want[i] {
			t.Fatalf("sorted[%d].OpStr = %q, want %q", i, s.OpStr, want[i])
		}
	}

	// Sorting must preserve every snapshot, including same-named duplicates.
	counts := make(map[string]int, len(all))
	for _, s := range all {
		counts[s.OpStr]++
	}
	if counts["GET"] != 2 || counts["SET"] != 1 || counts["AUTH"] != 1 {
		t.Fatalf("sort changed the snapshot multiset: %v", counts)
	}
}

func TestSortOpStatsEmptyAndSingle(t *testing.T) {
	sortOpStats(nil)

	var empty = []*OpStats{}
	sortOpStats(empty)
	if len(empty) != 0 {
		t.Fatalf("sortOpStats grew an empty slice to %d entries", len(empty))
	}

	var one = []*OpStats{{OpStr: "PING", Calls: 7}}
	sortOpStats(one)
	if len(one) != 1 || one[0].OpStr != "PING" || one[0].Calls != 7 {
		t.Fatalf("sortOpStats changed a single entry: %+v", one[0])
	}
}
