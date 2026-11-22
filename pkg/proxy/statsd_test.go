// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"net"
	"strings"
	"testing"
	"time"
)

func newStatsdTestServer(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readStatsdDatagram(t *testing.T, conn *net.UDPConn) string {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set UDP deadline: %v", err)
	}
	buf := make([]byte, 64*1024)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("read StatsD datagram: %v", err)
		}
		// The client's compatibility dial check may emit empty UDP datagrams.
		if n != 0 {
			return string(buf[:n])
		}
	}
}

func TestStatsdClientGaugeProtocol(t *testing.T) {
	server := newStatsdTestServer(t)
	client, err := newStatsdClient(server.LocalAddr().String())
	if err != nil {
		t.Fatalf("new StatsD client: %v", err)
	}
	defer client.Close()

	client.Gauge("codis.ops", int64(42))
	client.Gauge("codis.qps", 1.25)
	client.Gauge("codis.negative", -2)
	if err := client.Flush(); err != nil {
		t.Fatalf("flush StatsD gauges: %v", err)
	}

	want := strings.Join([]string{
		"codis.ops:42|g",
		"codis.qps:1.25|g",
		"codis.negative:0|g",
		"codis.negative:-2|g",
	}, "\n")
	if got := readStatsdDatagram(t, server); got != want {
		t.Fatalf("StatsD payload mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestStatsdClientCloseFlushesAndIsIdempotent(t *testing.T) {
	server := newStatsdTestServer(t)
	client, err := newStatsdClient(server.LocalAddr().String())
	if err != nil {
		t.Fatalf("new StatsD client: %v", err)
	}
	client.Gauge("codis.alive", uint64(1))
	if err := client.Close(); err != nil {
		t.Fatalf("close StatsD client: %v", err)
	}
	if got, want := readStatsdDatagram(t, server), "codis.alive:1|g"; got != want {
		t.Fatalf("StatsD payload = %q, want %q", got, want)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	client.Gauge("codis.after_close", 1)
	if err := client.Flush(); err != nil {
		t.Fatalf("flush after close: %v", err)
	}
}

func TestStatsdClientSplitsOversizedBufferAtMetricBoundary(t *testing.T) {
	server := newStatsdTestServer(t)
	client, err := newStatsdClient(server.LocalAddr().String())
	if err != nil {
		t.Fatalf("new StatsD client: %v", err)
	}
	defer client.Close()

	for range 12 {
		client.Gauge("codis."+strings.Repeat("x", 150), 1)
	}
	first := readStatsdDatagram(t, server)
	if len(first) > statsdMaxPacketSize-1 {
		t.Fatalf("datagram length = %d, exceeds packet budget %d", len(first), statsdMaxPacketSize-1)
	}
	if strings.HasSuffix(first, "\n") || !strings.Contains(first, ":1|g") {
		t.Fatalf("malformed first datagram: %q", first)
	}
}
