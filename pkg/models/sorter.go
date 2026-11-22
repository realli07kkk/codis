// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import (
	"cmp"
	"slices"
)

func SortGroup(group map[int]*Group) []*Group {
	slice := make([]*Group, 0, len(group))
	for _, g := range group {
		slice = append(slice, g)
	}
	slices.SortFunc(slice, func(a, b *Group) int {
		return cmp.Compare(a.Id, b.Id)
	})
	return slice
}

func SortProxy(proxy map[string]*Proxy) []*Proxy {
	slice := make([]*Proxy, 0, len(proxy))
	for _, p := range proxy {
		slice = append(slice, p)
	}
	slices.SortFunc(slice, func(a, b *Proxy) int {
		return cmp.Compare(a.Id, b.Id)
	})
	return slice
}
