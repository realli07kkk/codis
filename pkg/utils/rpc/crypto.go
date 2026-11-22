// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package rpc

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net"
	"slices"
)

// ConstantTimeCompareString compares two strings without leaking their
// contents through timing. Both values are hashed first so the comparison
// time does not depend on the input length either.
func ConstantTimeCompareString(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}

func NewToken(segs ...string) string {
	var list []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		addr := i.HardwareAddr.String()
		if addr != "" {
			list = append(list, addr)
		}
	}
	slices.Sort(list)

	t := &bytes.Buffer{}
	fmt.Fprintf(t, "Codis-Token@%v", list)
	for _, s := range segs {
		fmt.Fprintf(t, "-{%s}", s)
	}
	b := md5.Sum(t.Bytes())
	return fmt.Sprintf("%x", b)
}

func NewXAuth(segs ...string) string {
	t := &bytes.Buffer{}
	fmt.Fprintf(t, "Codis-XAuth")
	for _, s := range segs {
		fmt.Fprintf(t, "-[%s]", s)
	}
	b := sha256.Sum256(t.Bytes())
	return fmt.Sprintf("%x", b[:16])
}
