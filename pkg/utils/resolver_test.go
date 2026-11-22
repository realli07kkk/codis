// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package utils

import (
	"net"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func TestLookupIP(t *testing.T) {
	LookupIP("localhost")
}

func TestLookupIPTimeout(t *testing.T) {
	start := time.Now()
	LookupIPTimeout("testtesttest", time.Millisecond)
	since := time.Since(start)
	assert.Must(t, since < time.Millisecond*10)
}

func TestResolveTCPAddr(t *testing.T) {
	tcpAddr := ResolveTCPAddr("127.0.0.1:1000")
	assert.Must(t, tcpAddr != nil)
	assert.Must(t, tcpAddr.IP.Equal(net.ParseIP("127.0.0.1")))
	assert.Must(t, tcpAddr.Port == 1000)
}

func TestResolveTCPAddrTimeout(t *testing.T) {
	start := time.Now()
	ResolveTCPAddrTimeout("testtesttest", time.Millisecond)
	since := time.Since(start)
	assert.Must(t, since < time.Millisecond*10)
}

func TestReplaceUnspecifiedIP(t *testing.T) {
	Hostname = "guest"
	HostIPs, InterfaceIPs = nil, nil

	_, err1 := ReplaceUnspecifiedIP("tcp", "0.0.0.0:1000", "")
	assert.Must(t, err1 != nil)
	_, err2 := ReplaceUnspecifiedIP("tcp", "1.1.1.1:0", "")
	assert.Must(t, err2 != nil)

	addr3, err3 := ReplaceUnspecifiedIP("tcp", "0.0.0.0:1000", "127.0.0.1:2000")
	assert.MustNoError(t, err3)
	assert.Must(t, addr3 == "127.0.0.1:2000")

	InterfaceIPs = []string{"ip0"}
	addr4, err4 := ReplaceUnspecifiedIP("tcp", "0.0.0.0:1000", "")
	assert.MustNoError(t, err4)
	assert.Must(t, addr4 == "ip0:1000")

	HostIPs = []string{"ip1"}
	addr5, err5 := ReplaceUnspecifiedIP("tcp", "0.0.0.0:1000", "")
	assert.MustNoError(t, err5)
	assert.Must(t, addr5 == Hostname+":1000")
}
