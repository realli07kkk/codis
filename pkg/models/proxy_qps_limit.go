// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package models

type ProxyQPSLimit struct {
	Revision  int64  `json:"revision"`
	Limit     int64  `json:"limit"`
	UpdatedAt string `json:"updated_at"`
}

func (p *ProxyQPSLimit) Encode() []byte {
	return jsonEncode(p)
}
