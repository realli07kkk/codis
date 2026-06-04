// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redis

import "github.com/CodisLabs/codis/pkg/utils/errors"

type RedisAuthIdentity struct {
	Username string `json:"username"`
	Password string `json:"-"`
}

func PasswordAuthIdentity(password string) RedisAuthIdentity {
	return RedisAuthIdentity{Password: password}
}

func (a RedisAuthIdentity) Validate() error {
	if a.Username != "" && a.Password == "" {
		return errors.New("invalid redis auth identity")
	}
	return nil
}

func (a RedisAuthIdentity) HasAuth() bool {
	return a.Password != ""
}

func (a RedisAuthIdentity) AuthArgs() []interface{} {
	if a.Username != "" {
		return []interface{}{a.Username, a.Password}
	}
	if a.Password != "" {
		return []interface{}{a.Password}
	}
	return nil
}
