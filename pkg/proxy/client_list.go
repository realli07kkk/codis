// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/utils/sync2/atomic2"
)

type clientSessionRegistry struct {
	sync.RWMutex

	next     atomic2.Int64
	sessions map[int64]*Session
}

var clientSessions = &clientSessionRegistry{
	sessions: make(map[int64]*Session),
}

func (r *clientSessionRegistry) nextID() int64 {
	return r.next.Incr()
}

func (r *clientSessionRegistry) register(s *Session, maxClients int) bool {
	r.Lock()
	defer r.Unlock()
	if len(r.sessions) >= maxClients {
		return false
	}
	r.sessions[s.Id] = s
	return true
}

func (r *clientSessionRegistry) unregister(s *Session) {
	r.Lock()
	delete(r.sessions, s.Id)
	r.Unlock()
}

func (r *clientSessionRegistry) count() int64 {
	r.RLock()
	defer r.RUnlock()
	return int64(len(r.sessions))
}

func (r *clientSessionRegistry) markACLStale(config *Config, revision int64) {
	r.RLock()
	sessions := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		if s.config == config {
			sessions = append(sessions, s)
		}
	}
	r.RUnlock()

	for _, s := range sessions {
		s.markACLStale(revision)
	}
}

// Caller must not hold any Session.mu while calling snapshot.
// snapshot releases the registry lock before taking per-session read locks.
func (r *clientSessionRegistry) snapshot(now time.Time) []clientListEntry {
	r.RLock()
	sessions := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.RUnlock()

	entries := make([]clientListEntry, 0, len(sessions))
	for _, s := range sessions {
		entries = append(entries, s.clientListEntry(now))
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})
	return entries
}

// Caller must not hold any Session.mu while calling snapshotByIDs.
// snapshotByIDs releases the registry lock before taking per-session read locks.
func (r *clientSessionRegistry) snapshotByIDs(ids []int64, now time.Time) []clientListEntry {
	r.RLock()
	sessions := make([]*Session, 0, len(ids))
	for _, id := range ids {
		if s := r.sessions[id]; s != nil {
			sessions = append(sessions, s)
		}
	}
	r.RUnlock()

	entries := make([]clientListEntry, 0, len(sessions))
	for _, s := range sessions {
		entries = append(entries, s.clientListEntry(now))
	}
	return entries
}

type clientListEntry struct {
	ID     int64
	Addr   string
	LAddr  string
	Age    int64
	Idle   int64
	Flags  string
	DB     int32
	Events string
	Cmd    string
	User   string
}

func (s *Session) clientListEntry(now time.Time) clientListEntry {
	s.mu.RLock()
	lastOpUnix := s.LastOpUnix
	lastOpStr := s.lastOpStr
	database := s.database
	authorized := s.authorized
	s.mu.RUnlock()

	nowUnix := now.Unix()
	age := nowUnix - s.CreateUnix
	if age < 0 {
		age = 0
	}
	idle := age
	if lastOpUnix != 0 {
		idle = nowUnix - lastOpUnix
		if idle < 0 {
			idle = 0
		}
	}

	cmd := "NULL"
	if lastOpStr != "" {
		cmd = lastOpStr
	}

	user := ""
	if identity := s.getACLIdentity(); identity != nil {
		user = identity.Username
	} else if authorized || s.config == nil || s.config.SessionAuth == "" {
		user = "default"
	}

	entry := clientListEntry{
		ID:     s.Id,
		Age:    age,
		Idle:   idle,
		Flags:  "N",
		DB:     database,
		Events: "r",
		Cmd:    cmd,
		User:   user,
	}
	if s.Conn != nil {
		entry.Addr = s.Conn.RemoteAddr()
		entry.LAddr = s.Conn.LocalAddr()
	}
	return entry
}

func (r *clientSessionRegistry) handleRequestClient(caller *Session, req *Request) error {
	if len(req.Multi) < 2 {
		req.Resp = redis.NewErrorf("ERR wrong number of arguments for 'CLIENT' command")
		return nil
	}
	subcmd := strings.ToUpper(string(req.Multi[1].Value))
	switch subcmd {
	case "LIST":
		return r.handleRequestClientList(caller, req)
	default:
		req.Resp = redis.NewErrorf("ERR unsupported CLIENT subcommand '%s'", string(req.Multi[1].Value))
		return nil
	}
}

func (r *clientSessionRegistry) handleRequestClientList(caller *Session, req *Request) error {
	entries, ok := r.parseClientListEntries(caller, req)
	if !ok {
		return nil
	}
	req.Resp = redis.NewBulkBytes(formatClientList(entries))
	return nil
}

func (r *clientSessionRegistry) parseClientListEntries(_ *Session, req *Request) ([]clientListEntry, bool) {
	now := time.Now()
	switch {
	case len(req.Multi) == 2:
		return r.snapshot(now), true
	case len(req.Multi) == 4 && strings.EqualFold(string(req.Multi[2].Value), "TYPE"):
		switch typ := strings.ToLower(string(req.Multi[3].Value)); typ {
		case "normal":
			return r.snapshot(now), true
		case "master", "replica", "pubsub":
			return nil, true
		default:
			req.Resp = redis.NewErrorf("ERR Unknown client type '%s'", string(req.Multi[3].Value))
			return nil, false
		}
	case len(req.Multi) > 3 && strings.EqualFold(string(req.Multi[2].Value), "ID"):
		ids := make([]int64, 0, len(req.Multi)-3)
		for _, x := range req.Multi[3:] {
			id, err := redis.Btoi64(x.Value)
			if err != nil {
				req.Resp = redis.NewErrorf("ERR Invalid client ID")
				return nil, false
			}
			ids = append(ids, id)
		}
		return r.snapshotByIDs(ids, now), true
	default:
		req.Resp = redis.NewErrorf("ERR syntax error")
		return nil, false
	}
}

func formatClientList(entries []clientListEntry) []byte {
	var b bytes.Buffer
	for _, e := range entries {
		fields := []string{
			fmt.Sprintf("id=%d", e.ID),
			"addr=" + e.Addr,
			"laddr=" + e.LAddr,
			"name=",
			fmt.Sprintf("age=%d", e.Age),
			fmt.Sprintf("idle=%d", e.Idle),
			"flags=" + e.Flags,
			fmt.Sprintf("db=%d", e.DB),
			"sub=0",
			"psub=0",
			"ssub=0",
			"multi=-1",
			"qbuf=0",
			"qbuf-free=0",
			"obl=0",
			"oll=0",
			"omem=0",
			"events=" + e.Events,
			"cmd=" + e.Cmd,
			"user=" + e.User,
			"redir=-1",
			"resp=2",
		}
		b.WriteString(strings.Join(fields, " "))
		b.WriteByte('\n')
	}
	return b.Bytes()
}
