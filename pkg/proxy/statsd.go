// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

const statsdMaxPacketSize = 1440

// statsdClient implements the small StatsD surface Codis uses: UDP gauges with
// bounded datagrams. A report cycle calls Flush explicitly, so no background
// flush goroutine is needed.
type statsdClient struct {
	mu     sync.Mutex
	conn   net.Conn
	buf    []byte
	closed bool
}

func newStatsdClient(addr string) (*statsdClient, error) {
	conn, err := net.DialTimeout("udp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	// Preserve the old client's early UDP error check.
	for range 2 {
		if _, err := conn.Write(nil); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return &statsdClient{
		conn: conn,
		buf:  make([]byte, 0, statsdMaxPacketSize+200),
	}, nil
}

func (c *statsdClient) Gauge(bucket string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	lastSafeLen := len(c.buf)
	if isNegativeStatsdNumber(value) {
		c.buf = appendStatsdMetric(c.buf, bucket, 0)
	}
	c.buf = appendStatsdMetric(c.buf, bucket, value)
	if len(c.buf) > statsdMaxPacketSize {
		_ = c.flushLocked(lastSafeLen)
	}
}

func appendStatsdMetric(buf []byte, bucket string, value any) []byte {
	buf = append(buf, bucket...)
	buf = append(buf, ':')
	buf = appendStatsdNumber(buf, value)
	return append(buf, '|', 'g', '\n')
}

func appendStatsdNumber(buf []byte, value any) []byte {
	switch n := value.(type) {
	case int:
		return strconv.AppendInt(buf, int64(n), 10)
	case uint:
		return strconv.AppendUint(buf, uint64(n), 10)
	case int64:
		return strconv.AppendInt(buf, n, 10)
	case uint64:
		return strconv.AppendUint(buf, n, 10)
	case int32:
		return strconv.AppendInt(buf, int64(n), 10)
	case uint32:
		return strconv.AppendUint(buf, uint64(n), 10)
	case int16:
		return strconv.AppendInt(buf, int64(n), 10)
	case uint16:
		return strconv.AppendUint(buf, uint64(n), 10)
	case int8:
		return strconv.AppendInt(buf, int64(n), 10)
	case uint8:
		return strconv.AppendUint(buf, uint64(n), 10)
	case float64:
		return strconv.AppendFloat(buf, n, 'f', -1, 64)
	case float32:
		return strconv.AppendFloat(buf, float64(n), 'f', -1, 32)
	default:
		return buf
	}
}

func isNegativeStatsdNumber(value any) bool {
	switch n := value.(type) {
	case int:
		return n < 0
	case int64:
		return n < 0
	case int32:
		return n < 0
	case int16:
		return n < 0
	case int8:
		return n < 0
	case float64:
		return n < 0
	case float32:
		return n < 0
	default:
		return false
	}
}

func (c *statsdClient) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	return c.flushLocked(0)
}

// flushLocked writes the first n bytes, or the whole buffer when n is zero.
// The final newline is omitted as required by the StatsD text protocol.
func (c *statsdClient) flushLocked(n int) error {
	if len(c.buf) == 0 {
		return nil
	}
	if n == 0 {
		n = len(c.buf)
	}
	written, err := c.conn.Write(c.buf[:n-1])
	if err == nil && written != n-1 {
		err = io.ErrShortWrite
	}
	copy(c.buf, c.buf[n:])
	c.buf = c.buf[:len(c.buf)-n]
	return err
}

func (c *statsdClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	flushErr := c.flushLocked(0)
	closeErr := c.conn.Close()
	return errors.Join(flushErr, closeErr)
}
