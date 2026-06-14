// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import "github.com/CodisLabs/codis/pkg/models"

type ACLSnapshot struct {
	Revision  int64
	Enabled   bool
	UpdatedAt string
	Users     map[string]*models.ACLUser
}

func NewACLSnapshot(acl *models.ACL) *ACLSnapshot {
	s := &ACLSnapshot{}
	if acl == nil {
		return s
	}
	s.Revision = acl.Revision
	s.Enabled = acl.Enabled
	s.UpdatedAt = acl.UpdatedAt
	s.Users = make(map[string]*models.ACLUser, len(acl.Users))
	for _, user := range acl.Users {
		if user == nil {
			continue
		}
		dup := &models.ACLUser{
			Name:           user.Name,
			Enabled:        user.Enabled,
			PasswordHashes: append([]string(nil), user.PasswordHashes...),
			Rules:          append([]string(nil), user.Rules...),
		}
		if user.DB != nil {
			db := *user.DB
			dup.DB = &db
		}
		s.Users[dup.Name] = dup
	}
	return s
}

func (s *Proxy) SetACL(acl *models.ACL) error {
	if s.IsClosed() {
		return ErrClosedProxy
	}
	if s.router != nil {
		if err := s.router.SetACL(acl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Proxy) ACLSnapshot() *ACLSnapshot {
	if s.router == nil {
		return nil
	}
	return s.router.ACLSnapshot()
}
