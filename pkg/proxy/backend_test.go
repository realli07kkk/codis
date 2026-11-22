// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"log"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
	"github.com/CodisLabs/codis/pkg/proxy/redis/redistest"
	"github.com/CodisLabs/codis/pkg/utils/assert"
)

func newConnPair(t *testing.T, config *Config) (*redis.Conn, *BackendConn) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.MustNoError(t, err)
	defer l.Close()

	const bufsize = 128 * 1024

	type acceptResult struct {
		conn *redis.Conn
		err  error
	}
	cc := make(chan acceptResult, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			cc <- acceptResult{err: err}
			return
		}
		cc <- acceptResult{conn: redis.NewConn(c, bufsize, bufsize)}
	}()

	bc := NewBackendConn(l.Addr().String(), 0, config)
	result := <-cc
	assert.MustNoError(t, result.err)
	return result.conn, bc
}

func TestBackend(t *testing.T) {
	config := NewDefaultConfig()
	config.BackendMaxPipeline = 0
	config.BackendSendTimeout.Set(time.Second)
	config.BackendRecvTimeout.Set(time.Minute)

	conn, bc := newConnPair(t, config)

	var array = make([]*Request, 16384)
	for i := range array {
		array[i] = &Request{Batch: &sync.WaitGroup{}}
	}

	errCh := make(chan error, 1)
	go func() {
		defer conn.Close()
		time.Sleep(time.Millisecond * 300)
		for i := range array {
			if _, err := conn.Decode(); err != nil {
				errCh <- err
				return
			}
			resp := redis.NewString([]byte(strconv.Itoa(i)))
			if err := conn.Encode(resp, true); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()

	defer bc.Close()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	go func() {
		for i := 0; i < 10; i++ {
			<-ticker.C
		}
		log.Panicf("timeout")
	}()

	for _, r := range array {
		bc.PushBack(r)
	}

	for i, r := range array {
		r.Batch.Wait()
		assert.MustNoError(t, r.Err)
		assert.Must(t, r.Resp != nil)
		assert.Must(t, string(r.Resp.Value) == strconv.Itoa(i))
	}
	assert.MustNoError(t, <-errCh)
}

func TestSharedBackendConnPoolConcurrentGetOrCreateAndClose(t *testing.T) {
	config := NewDefaultConfig()
	config.BackendNumberDatabases = 1
	config.BackendMaxPipeline = 0

	server := redistest.NewServer(t, func(args []string) *redistest.Resp {
		return redistest.OK()
	})

	for iter := 0; iter < 64; iter++ {
		pool := newSharedBackendConnPool(config, 1, RedisAuthIdentity{})
		start := make(chan struct{})
		var wg sync.WaitGroup

		for worker := 0; worker < 8; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				<-start
				for i := 0; i < 32; i++ {
					if bc := pool.GetOrCreate(server.Addr()); bc != nil {
						_ = bc.BackendConn(0, uint(worker+i), false)
					}
				}
			}(worker)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			pool.Close()
		}()

		close(start)
		wg.Wait()
		pool.Close()
	}
}
