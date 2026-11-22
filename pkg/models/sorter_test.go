// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import "testing"

func TestSortGroup(t *testing.T) {
	group := map[int]*Group{
		3: {Id: 3},
		1: {Id: 1},
		2: {Id: 2},
	}
	got := SortGroup(group)
	if len(got) != len(group) {
		t.Fatalf("SortGroup returned %d groups, want %d", len(got), len(group))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Id >= got[i].Id {
			t.Fatalf("SortGroup ids = %v, want ascending", groupIDs(got))
		}
	}
	if empty := SortGroup(nil); len(empty) != 0 {
		t.Fatalf("SortGroup(nil) = %v, want empty", empty)
	}
}

func TestSortProxy(t *testing.T) {
	proxy := map[string]*Proxy{
		"c": {Id: 3},
		"a": {Id: 1},
		"b": {Id: 2},
	}
	got := SortProxy(proxy)
	if len(got) != len(proxy) {
		t.Fatalf("SortProxy returned %d proxies, want %d", len(got), len(proxy))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Id >= got[i].Id {
			t.Fatalf("SortProxy ids = %v, want ascending", proxyIDs(got))
		}
	}
	if empty := SortProxy(nil); len(empty) != 0 {
		t.Fatalf("SortProxy(nil) = %v, want empty", empty)
	}
}

func groupIDs(group []*Group) []int {
	ids := make([]int, 0, len(group))
	for _, g := range group {
		ids = append(ids, g.Id)
	}
	return ids
}

func proxyIDs(proxy []*Proxy) []int {
	ids := make([]int, 0, len(proxy))
	for _, p := range proxy {
		ids = append(ids, p.Id)
	}
	return ids
}
