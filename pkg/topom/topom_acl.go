// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
	"github.com/CodisLabs/codis/pkg/utils/redis"
)

const aclRedisSyncTimeout = time.Second * 5

type ACLView struct {
	Revision   int64          `json:"revision"`
	Enabled    bool           `json:"enabled"`
	SyncStatus string         `json:"sync_status"`
	Users      []*ACLUserView `json:"users"`
}

type ACLUserView struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// DB echoes the user's DB binding back to operators. nil = unbound.
	DB            *int     `json:"db,omitempty"`
	PasswordCount int      `json:"password_count"`
	Rules         []string `json:"rules"`
	LastError     string   `json:"last_error,omitempty"`
}

type ACLUpdateRequest struct {
	Enabled bool             `json:"enabled"`
	Users   []*ACLUserUpdate `json:"users"`
}

type ACLUserUpdate struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// DB, when non-nil, binds the user to a fixed backend DB. nil = unbound.
	DB             *int     `json:"db,omitempty"`
	NewPassword    string   `json:"new_password,omitempty"`
	PasswordHashes []string `json:"password_hashes,omitempty"`
	Rules          []string `json:"rules"`
}

var (
	aclUserNameRe     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@-]*$`)
	aclPasswordHashRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

func (s *Topom) GetACL() (*ACLView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, err := s.newContext()
	if err != nil {
		return nil, err
	}
	return newACLView(ctx.acl, s.aclSyncStatus(ctx.acl)), nil
}

func (s *Topom) UpdateACL(req *ACLUpdateRequest) (*ACLView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, err := s.newContext()
	if err != nil {
		return nil, err
	}
	acl, err := s.buildACL(ctx.acl, req)
	if err != nil {
		return nil, err
	}
	if err := s.syncACLToRedis(ctx, acl); err != nil {
		return nil, err
	}
	if err := s.storeUpdateACL(acl); err != nil {
		return nil, err
	}
	s.dirtyACLCache()
	failed, err := s.syncACLToProxies(ctx, acl)
	s.setACLSyncResult(acl.Revision, failed)
	if err != nil {
		return nil, err
	}
	return newACLView(acl, s.aclSyncStatus(acl)), nil
}

func (s *Topom) SyncACL() (*ACLView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, err := s.newContext()
	if err != nil {
		return nil, err
	}
	if err := s.syncACLToRedis(ctx, ctx.acl); err != nil {
		return nil, err
	}
	failed, err := s.syncACLToProxies(ctx, ctx.acl)
	s.setACLSyncResult(ctx.acl.Revision, failed)
	if err != nil {
		return nil, err
	}
	return newACLView(ctx.acl, s.aclSyncStatus(ctx.acl)), nil
}

func shouldSyncACLRuntime(acl *models.ACL) bool {
	return acl != nil && (acl.Revision != 0 || acl.Enabled || len(acl.Users) != 0)
}

func (s *Topom) syncACLToRedisAddr(addr string, acl *models.ACL) error {
	if !shouldSyncACLRuntime(acl) {
		return nil
	}
	return s.syncACLToRedisServer(addr, acl, nil)
}

func (s *Topom) buildACL(current *models.ACL, req *ACLUpdateRequest) (*models.ACL, error) {
	if req == nil {
		return nil, errors.New("missing acl request")
	}
	currentUsers := make(map[string]*models.ACLUser)
	if current != nil {
		for _, user := range current.Users {
			if user != nil {
				currentUsers[user.Name] = user
			}
		}
	}
	seen := make(map[string]bool)
	users := make([]*models.ACLUser, 0, len(req.Users))
	for _, input := range req.Users {
		user, err := s.buildACLUser(currentUsers, input)
		if err != nil {
			return nil, err
		}
		if seen[user.Name] {
			return nil, errors.Errorf("duplicate acl user %s", user.Name)
		}
		seen[user.Name] = true
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].Name < users[j].Name
	})
	revision := int64(1)
	if current != nil && current.Revision >= revision {
		revision = current.Revision + 1
	}
	return &models.ACL{
		Revision:  revision,
		Enabled:   req.Enabled,
		Users:     users,
		UpdatedAt: time.Now().Format(time.RFC3339Nano),
	}, nil
}

func (s *Topom) buildACLUser(current map[string]*models.ACLUser, input *ACLUserUpdate) (*models.ACLUser, error) {
	if input == nil {
		return nil, errors.New("nil acl user")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || !aclUserNameRe.MatchString(name) {
		return nil, errors.Errorf("invalid acl user name %q", input.Name)
	}
	if serviceUser := s.config.BackendAuthUsername; serviceUser != "" && name == serviceUser {
		return nil, errors.Errorf("acl user %s is reserved for backend service identity", name)
	}

	hashes := append([]string(nil), input.PasswordHashes...)
	rules, ruleHashes, err := normalizeACLRules(input.Rules)
	if err != nil {
		return nil, errors.Errorf("acl user %s: %s", name, err)
	}
	hashes = append(hashes, ruleHashes...)
	if input.NewPassword != "" {
		hashes = []string{models.ACLPasswordHash([]byte(input.NewPassword))}
	} else if len(hashes) == 0 {
		if old := current[name]; old != nil {
			hashes = append([]string(nil), old.PasswordHashes...)
		}
	}
	for i, hash := range hashes {
		hash = strings.TrimPrefix(strings.TrimSpace(hash), "#")
		if !aclPasswordHashRe.MatchString(hash) {
			return nil, errors.Errorf("acl user %s: invalid password hash", name)
		}
		hashes[i] = strings.ToLower(hash)
	}
	if len(rules) == 0 {
		return nil, errors.Errorf("acl user %s: empty acl rules", name)
	}
	if len(hashes) == 0 && !hasACLRule(rules, "nopass") {
		return nil, errors.Errorf("acl user %s: missing password hash", name)
	}
	// DB binding: topom can only validate db>=0; the proxy enforces the
	// upper bound against backend_number_databases at AUTH time.
	var dbBinding *int
	if input.DB != nil {
		db := *input.DB
		if db < 0 {
			return nil, errors.Errorf("acl user %s: invalid db %d", name, db)
		}
		dbBinding = &db
	}
	return &models.ACLUser{
		Name:           name,
		Enabled:        input.Enabled,
		DB:             dbBinding,
		PasswordHashes: hashes,
		Rules:          rules,
	}, nil
}

func normalizeACLRules(input []string) ([]string, []string, error) {
	var rules []string
	var hashes []string
	for _, raw := range input {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		switch {
		case strings.HasPrefix(token, ">"):
			return nil, nil, errors.New("plain password token is not allowed")
		case strings.HasPrefix(token, "#"):
			hashes = append(hashes, token[1:])
		case lower == "on" || lower == "off":
			continue
		case lower == "reset" || lower == "resetpass":
			return nil, nil, errors.Errorf("rule token %s is managed by codis", token)
		default:
			rules = append(rules, token)
		}
	}
	return rules, hashes, nil
}

func hasACLRule(rules []string, rule string) bool {
	for _, r := range rules {
		if strings.EqualFold(r, rule) {
			return true
		}
	}
	return false
}

func newACLView(acl *models.ACL, status string) *ACLView {
	view := &ACLView{SyncStatus: status}
	if acl == nil {
		view.SyncStatus = "not_configured"
		return view
	}
	view.Revision = acl.Revision
	view.Enabled = acl.Enabled
	if acl.Revision == 0 && !acl.Enabled && len(acl.Users) == 0 {
		view.SyncStatus = "not_configured"
	}
	for _, user := range acl.Users {
		if user == nil {
			continue
		}
		uv := &ACLUserView{
			Name:          user.Name,
			Enabled:       user.Enabled,
			PasswordCount: len(user.PasswordHashes),
			Rules:         append([]string(nil), user.Rules...),
		}
		if user.DB != nil {
			db := *user.DB
			uv.DB = &db
		}
		view.Users = append(view.Users, uv)
	}
	return view
}

func (s *Topom) aclSyncStatus(acl *models.ACL) string {
	if acl == nil {
		return "not_configured"
	}
	if s.aclSync.revision == acl.Revision && len(s.aclSync.failed) != 0 {
		return "proxy_sync_failed:" + strings.Join(s.aclSync.failed, ",")
	}
	return "ready"
}

func (s *Topom) setACLSyncResult(revision int64, failed []string) {
	s.aclSync.revision = revision
	s.aclSync.failed = append(s.aclSync.failed[:0], failed...)
}

func (s *Topom) syncACLToRedis(ctx *context, acl *models.ACL) error {
	if !shouldSyncACLRuntime(acl) {
		return nil
	}
	addrs := aclTargetServerAddrs(ctx)
	for _, addr := range addrs {
		if err := s.syncACLToRedisServer(addr, acl, ctx.acl); err != nil {
			log.ErrorErrorf(err, "redis %s sync acl failed", addr)
			return errors.Errorf("redis %s sync acl failed", addr)
		}
	}
	return nil
}

func aclTargetServerAddrs(ctx *context) []string {
	seen := make(map[string]bool)
	var addrs []string
	for _, group := range ctx.group {
		if group == nil {
			continue
		}
		for _, server := range group.Servers {
			if server == nil || server.Addr == "" || seen[server.Addr] {
				continue
			}
			seen[server.Addr] = true
			addrs = append(addrs, server.Addr)
		}
	}
	sort.Strings(addrs)
	return addrs
}

func (s *Topom) syncACLToRedisServer(addr string, acl *models.ACL, current *models.ACL) error {
	c, err := redis.NewClientWithAuthIdentity(addr, s.config.BackendAuthIdentity(), aclRedisSyncTimeout)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := s.ensureBackendServiceUser(c); err != nil {
		return err
	}
	for _, user := range acl.Users {
		if err := redisACLSetUser(c, user); err != nil {
			return err
		}
	}
	for _, name := range removedACLUsers(current, acl) {
		if serviceUser := s.config.BackendAuthUsername; serviceUser != "" && name == serviceUser {
			continue
		}
		if name == "default" {
			if err := redisACLSetUser(c, &models.ACLUser{Name: name, Enabled: false, Rules: []string{"nopass"}}); err != nil {
				return err
			}
			continue
		}
		if _, err := c.Do("ACL", "DELUSER", name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Topom) ensureBackendServiceUser(c *redis.Client) error {
	if s.config.BackendAuthUsername == "" || s.config.BackendAuthPassword == "" {
		return nil
	}
	hash := models.ACLPasswordHash([]byte(s.config.BackendAuthPassword))
	_, err := c.Do("ACL", "SETUSER", s.config.BackendAuthUsername, "reset", "on", "#"+hash, "+@all", "~*")
	return err
}

func redisACLSetUser(c *redis.Client, user *models.ACLUser) error {
	args := []interface{}{"SETUSER", user.Name, "reset"}
	if user.Enabled {
		args = append(args, "on")
	} else {
		args = append(args, "off")
	}
	for _, hash := range user.PasswordHashes {
		args = append(args, "#"+hash)
	}
	for _, rule := range user.Rules {
		args = append(args, rule)
	}
	_, err := c.Do("ACL", args...)
	return err
}

func removedACLUsers(current, next *models.ACL) []string {
	if current == nil {
		return nil
	}
	nextUsers := make(map[string]bool)
	for _, user := range next.Users {
		if user != nil {
			nextUsers[user.Name] = true
		}
	}
	var removed []string
	for _, user := range current.Users {
		if user != nil && !nextUsers[user.Name] {
			removed = append(removed, user.Name)
		}
	}
	sort.Strings(removed)
	return removed
}

func (s *Topom) syncACLToProxies(ctx *context, acl *models.ACL) ([]string, error) {
	var tokens []string
	for token := range ctx.proxy {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	var failed []string
	for _, token := range tokens {
		p := ctx.proxy[token]
		if p == nil {
			continue
		}
		if err := s.newProxyClient(p).SetACL(acl); err != nil {
			log.ErrorErrorf(err, "proxy-[%s] sync acl failed", p.Token)
			failed = append(failed, p.Token)
		}
	}
	if len(failed) != 0 {
		return failed, errors.Errorf("acl proxy sync failed: %s", strings.Join(failed, ","))
	}
	return nil, nil
}
