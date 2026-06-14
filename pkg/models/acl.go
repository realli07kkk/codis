// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

import (
	"crypto/sha256"
	"encoding/hex"
)

type ACL struct {
	Revision  int64      `json:"revision"`
	Enabled   bool       `json:"enabled"`
	Users     []*ACLUser `json:"users"`
	UpdatedAt string     `json:"updated_at"`
}

type ACLUser struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// DB expresses a DB-bound ACL user. nil = unbound (legacy/default), a
	// non-negative value = clients authenticating as this user are forced to
	// this DB. It is a proxy-only routing attribute and is never rendered
	// into a Redis ACL SETUSER.
	DB             *int     `json:"db,omitempty"`
	PasswordHashes []string `json:"password_hashes"`
	Rules          []string `json:"rules"`
}

func (p *ACL) Encode() []byte {
	return jsonEncode(p)
}

func ACLPasswordHash(password []byte) string {
	sum := sha256.Sum256(password)
	return hex.EncodeToString(sum[:])
}
