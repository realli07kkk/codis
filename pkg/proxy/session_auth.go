// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"strings"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy/redis"
)

const defaultACLUser = "default"

type SessionACLIdentity struct {
	Username       string
	CredentialHash string
	Password       []byte
	Revision       int64
	Stale          bool
}

func (s *Session) handleAuth(r *Request, d *Router) error {
	if d.aclAuthEnabled() {
		return s.handleACLAuth(r, d)
	}
	return s.handleLegacyAuth(r)
}

func (s *Session) handleLegacyAuth(r *Request) error {
	if len(r.Multi) != 2 {
		r.Resp = redis.NewErrorf("ERR wrong number of arguments for 'AUTH' command")
		return nil
	}
	if s.config.SessionAuth == "" {
		r.Resp = redis.NewErrorf("ERR Client sent AUTH, but no password is set")
		return nil
	}

	wasAuthorized := s.isAuthorized()
	now := time.Now()
	remoteAddr := s.remoteAddr()
	if s.authBruteforce.BeforeAuth(remoteAddr, wasAuthorized, now) {
		r.Resp = redis.NewErrorf(authBruteforceLockedMessage)
		return nil
	}

	switch {
	case s.config.SessionAuth != string(r.Multi[1].Value):
		s.setAuthorized(false)
		if !wasAuthorized {
			s.authBruteforce.RecordAuthFailure(remoteAddr, wasAuthorized, now)
		}
		r.Resp = redis.NewErrorf("ERR invalid password")
	default:
		s.setAuthorized(true)
		s.authBruteforce.RecordAuthSuccess(remoteAddr)
		r.Resp = RespOK
	}
	return nil
}

func (s *Session) handleACLAuth(r *Request, d *Router) error {
	if len(r.Multi) != 2 && len(r.Multi) != 3 {
		r.Resp = redis.NewErrorf("ERR wrong number of arguments for 'AUTH' command")
		return nil
	}
	snapshot := d.ACLSnapshot()
	if snapshot == nil || !snapshot.Enabled {
		r.Resp = redis.NewErrorf("ERR Client sent AUTH, but no password is set")
		return nil
	}

	username := defaultACLUser
	password := r.Multi[1].Value
	if len(r.Multi) == 3 {
		username = string(r.Multi[1].Value)
		password = r.Multi[2].Value
	}

	wasAuthorized := s.isAuthorized()
	now := time.Now()
	remoteAddr := s.remoteAddr()
	if s.authBruteforce.BeforeAuth(remoteAddr, wasAuthorized, now) {
		r.Resp = redis.NewErrorf(authBruteforceLockedMessage)
		return nil
	}

	hash := models.ACLPasswordHash(password)
	user := snapshot.Users[username]
	if user == nil || !user.Enabled || !aclPasswordAllowed(user, hash) {
		if !wasAuthorized {
			s.clearACLIdentity()
			s.setAuthorized(false)
			s.authBruteforce.RecordAuthFailure(remoteAddr, wasAuthorized, now)
		}
		r.Resp = redis.NewErrorf("WRONGPASS invalid username-password pair or user is disabled")
		return nil
	}

	s.setACLIdentity(&SessionACLIdentity{
		Username:       username,
		CredentialHash: hash,
		Password:       append([]byte(nil), password...),
		Revision:       snapshot.Revision,
	})
	s.setAuthorized(true)
	s.authBruteforce.RecordAuthSuccess(remoteAddr)
	r.Resp = RespOK
	return nil
}

func aclPasswordAllowed(user *models.ACLUser, hash string) bool {
	for _, rule := range user.Rules {
		if stringEqualFold(rule, "nopass") {
			return true
		}
	}
	for _, allowed := range user.PasswordHashes {
		if allowed == hash {
			return true
		}
	}
	return false
}

func (s *Session) handleRequestACL(r *Request, d *Router) error {
	if d.aclAuthEnabled() && len(r.Multi) == 2 && stringFoldEqual(r.Multi[1].Value, "WHOAMI") {
		identity := s.getACLIdentity()
		if identity == nil || identity.Stale {
			r.Resp = redis.NewErrorf("NOAUTH Authentication required")
			return nil
		}
		r.Resp = redis.NewBulkBytes([]byte(identity.Username))
		return nil
	}
	r.Resp = redis.NewErrorf("ERR unsupported ACL subcommand '%s'", aclSubcommand(r))
	return nil
}

func aclSubcommand(r *Request) string {
	if len(r.Multi) < 2 {
		return ""
	}
	return string(r.Multi[1].Value)
}

func stringFoldEqual(value []byte, expect string) bool {
	return stringEqualFold(string(value), expect)
}

func stringEqualFold(value, expect string) bool {
	return strings.EqualFold(value, expect)
}

func (s *Session) isACLAuthorized(d *Router) bool {
	identity := s.getACLIdentity()
	if identity == nil {
		return false
	}
	snapshot := d.ACLSnapshot()
	if snapshot == nil || !snapshot.Enabled || identity.Stale || identity.Revision != snapshot.Revision {
		s.clearACLIdentity()
		s.setAuthorized(false)
		return false
	}
	return true
}

func (s *Session) setACLIdentity(identity *SessionACLIdentity) {
	s.clearACLIdentity()
	s.mu.Lock()
	s.aclIdentity = identity
	s.mu.Unlock()
}

func (s *Session) getACLIdentity() *SessionACLIdentity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.aclIdentity == nil {
		return nil
	}
	identity := *s.aclIdentity
	identity.Password = append([]byte(nil), s.aclIdentity.Password...)
	return &identity
}

func (s *Session) clearACLIdentity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aclIdentity != nil {
		for i := range s.aclIdentity.Password {
			s.aclIdentity.Password[i] = 0
		}
		s.aclIdentity = nil
	}
}

func (s *Session) markACLStale(revision int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aclIdentity != nil && s.aclIdentity.Revision != revision {
		s.aclIdentity.Stale = true
	}
}

func (s *Session) remoteAddr() string {
	if s.Conn == nil {
		return ""
	}
	return s.Conn.RemoteAddr()
}
