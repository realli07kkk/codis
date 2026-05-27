// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"bytes"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
)

type streamRoute struct {
	hashKey []byte
	keys    [][]byte
}

type streamRouteKind uint8

const (
	streamRouteSingleKey streamRouteKind = iota
	streamRouteXGroup
	streamRouteXInfo
	streamRouteXRead
	streamRouteXReadGroup
)

type streamCommandSpec struct {
	flag OpFlag
	kind streamRouteKind
}

var streamCommandSpecs = map[string]streamCommandSpec{
	"XACK":        {flag: FlagWrite, kind: streamRouteSingleKey},
	"XACKDEL":     {flag: FlagWrite, kind: streamRouteSingleKey},
	"XADD":        {flag: FlagWrite, kind: streamRouteSingleKey},
	"XAUTOCLAIM":  {flag: FlagWrite, kind: streamRouteSingleKey},
	"XCFGSET":     {flag: FlagWrite, kind: streamRouteSingleKey},
	"XCLAIM":      {flag: FlagWrite, kind: streamRouteSingleKey},
	"XDEL":        {flag: FlagWrite, kind: streamRouteSingleKey},
	"XDELEX":      {flag: FlagWrite, kind: streamRouteSingleKey},
	"XGROUP":      {flag: FlagWrite, kind: streamRouteXGroup},
	"XIDMPRECORD": {flag: FlagWrite, kind: streamRouteSingleKey},
	"XINFO":       {flag: 0, kind: streamRouteXInfo},
	"XLEN":        {flag: 0, kind: streamRouteSingleKey},
	"XPENDING":    {flag: 0, kind: streamRouteSingleKey},
	"XRANGE":      {flag: 0, kind: streamRouteSingleKey},
	"XREAD":       {flag: 0, kind: streamRouteXRead},
	"XREADGROUP":  {flag: FlagWrite, kind: streamRouteXReadGroup},
	"XREVRANGE":   {flag: 0, kind: streamRouteSingleKey},
	"XSETID":      {flag: FlagWrite, kind: streamRouteSingleKey},
	"XTRIM":       {flag: FlagWrite, kind: streamRouteSingleKey},
}

var streamXGroupSubcommands = []string{
	"CREATE",
	"SETID",
	"DESTROY",
	"CREATECONSUMER",
	"DELCONSUMER",
}

var streamXInfoSubcommands = []string{
	"STREAM",
	"GROUPS",
	"CONSUMERS",
}

func isStreamCommand(opstr string) bool {
	_, ok := streamCommandSpecs[opstr]
	return ok
}

func registerStreamCommandOps(table map[string]OpInfo) {
	for name, spec := range streamCommandSpecs {
		table[name] = OpInfo{Name: name, Flag: spec.flag}
	}
}

func resolveStreamRoute(multi []*redis.Resp, opstr string) (streamRoute, *redis.Resp) {
	spec, ok := streamCommandSpecs[opstr]
	if !ok {
		return streamRoute{}, redis.NewErrorf("ERR unsupported Stream command '%s'", opstr)
	}

	switch spec.kind {
	case streamRouteXGroup:
		return resolveStreamContainerRoute(multi, opstr, streamXGroupSubcommands)
	case streamRouteXInfo:
		return resolveStreamContainerRoute(multi, opstr, streamXInfoSubcommands)
	case streamRouteXRead:
		return resolveStreamReadRoute(multi, opstr, 1)
	case streamRouteXReadGroup:
		if len(multi) < 4 || !streamTokenEqual(multi[1].Value, "GROUP") {
			return streamRoute{}, redis.NewErrorf("ERR wrong number of arguments for '%s' command", opstr)
		}
		return resolveStreamReadRoute(multi, opstr, 4)
	default:
		return resolveStreamSingleKeyRoute(multi, opstr)
	}
}

func resolveStreamSingleKeyRoute(multi []*redis.Resp, opstr string) (streamRoute, *redis.Resp) {
	if len(multi) < 2 {
		return streamRoute{}, redis.NewErrorf("ERR wrong number of arguments for '%s' command", opstr)
	}
	key := multi[1].Value
	return streamRoute{
		hashKey: key,
		keys:    [][]byte{key},
	}, nil
}

func resolveStreamReadRoute(multi []*redis.Resp, opstr string, startFrom int) (streamRoute, *redis.Resp) {
	streams := -1
	for i := startFrom; i < len(multi); i++ {
		switch {
		case streamTokenEqual(multi[i].Value, "STREAMS"):
			streams = i
			i = len(multi)
		case streamTokenEqual(multi[i].Value, "BLOCK"):
			return streamRoute{}, redis.NewErrorf("ERR unsupported blocking Stream command '%s'", opstr)
		}
	}
	if streams < 0 {
		return streamRoute{}, redis.NewErrorf("ERR missing STREAMS for '%s' command", opstr)
	}

	nblks := len(multi) - streams - 1
	if nblks < 2 || nblks%2 != 0 {
		return streamRoute{}, redis.NewErrorf("ERR Stream keys and IDs must be paired for '%s' command", opstr)
	}

	nkeys := nblks / 2
	keys := make([][]byte, 0, nkeys)
	keyStart := streams + 1
	firstKey := multi[keyStart].Value
	firstHashKey, firstHasTag := hashTagOrKeyWithTag(firstKey)
	for i := 0; i < nkeys; i++ {
		key := multi[keyStart+i].Value
		hashKey, hasTag := hashTagOrKeyWithTag(key)
		if !bytes.Equal(hashKey, firstHashKey) || hasTag != firstHasTag {
			return streamRoute{}, redis.NewErrorf("CROSSSLOT Stream keys in request don't share the same hash tag")
		}
		keys = append(keys, key)
	}

	return streamRoute{
		hashKey: firstKey,
		keys:    keys,
	}, nil
}

func streamTokenEqual(value []byte, token string) bool {
	if len(value) != len(token) {
		return false
	}
	for i := 0; i < len(token); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c != token[i] {
			return false
		}
	}
	return true
}

func resolveStreamContainerRoute(multi []*redis.Resp, opstr string, supported []string) (streamRoute, *redis.Resp) {
	if len(multi) < 2 {
		return streamRoute{}, redis.NewErrorf("ERR wrong number of arguments for '%s' command", opstr)
	}
	if !streamSubcommandSupported(multi[1].Value, supported) {
		return streamRoute{}, redis.NewErrorf("ERR unsupported Stream subcommand for '%s'", opstr)
	}
	if len(multi) < 3 {
		return streamRoute{}, redis.NewErrorf("ERR wrong number of arguments for '%s' command", opstr)
	}
	key := multi[2].Value
	return streamRoute{
		hashKey: key,
		keys:    [][]byte{key},
	}, nil
}

func streamSubcommandSupported(value []byte, supported []string) bool {
	for _, token := range supported {
		if streamTokenEqual(value, token) {
			return true
		}
	}
	return false
}

func (s *Router) handleRequestStream(r *Request) error {
	route, resp := resolveStreamRoute(r.Multi, r.OpStr)
	if resp != nil {
		r.Resp = resp
		return nil
	}
	return s.handleRequestWithHotKeyCacheInvalidationKeys(r, route.keys, func() error {
		return s.dispatchByHashKey(r, route.hashKey)
	})
}
