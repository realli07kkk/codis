// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import "testing"

func TestSortClusterNodesForDedupOrder(t *testing.T) {
	nodes := []clusterNode{
		{Token: "t2", Addr: "10.0.0.2:19000"},
		{Token: "t1", Addr: "10.0.0.1:19000", IsSelf: true},
		{Token: "t2", Addr: "10.0.0.1:19000"},
		{Token: "t1", Addr: "10.0.0.2:19000"},
	}
	sortClusterNodesForDedup(nodes)
	want := []clusterNode{
		{Token: "t1", Addr: "10.0.0.1:19000", IsSelf: true},
		{Token: "t1", Addr: "10.0.0.2:19000"},
		{Token: "t2", Addr: "10.0.0.1:19000"},
		{Token: "t2", Addr: "10.0.0.2:19000"},
	}
	for i := range want {
		if nodes[i] != want[i] {
			t.Fatalf("nodes[%d] = %+v, want %+v (all = %+v)", i, nodes[i], want[i], nodes)
		}
	}

	// Self always sorts first, regardless of token/addr.
	nodes = []clusterNode{
		{Token: "a", Addr: "10.0.0.1:19000"},
		{Token: "z", Addr: "10.0.0.9:19000", IsSelf: true},
	}
	sortClusterNodesForDedup(nodes)
	if !nodes[0].IsSelf {
		t.Fatalf("self node did not sort first: %+v", nodes)
	}
}
