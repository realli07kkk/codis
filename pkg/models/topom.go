// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

type Topom struct {
	Token     string `json:"token"`
	StartTime string `json:"start_time"`
	AdminAddr string `json:"admin_addr"`

	// OwnerID identifies the process that created the persistent topom lock.
	// It is generated once per process start, so retries of the same process
	// can recover their own lock while a restart is a different owner.
	OwnerID string `json:"owner_id"`

	ProductName string `json:"product_name"`

	Pid int    `json:"pid"`
	Pwd string `json:"pwd"`
	Sys string `json:"sys"`
}

func (t *Topom) Encode() []byte {
	return jsonEncode(t)
}
