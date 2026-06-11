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

func newConnPair(config *Config) (*redis.Conn, *BackendConn) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.MustNoError(err)
	defer l.Close()

	const bufsize = 128 * 1024

	cc := make(chan *redis.Conn, 1)
	go func() {
		defer close(cc)
		c, err := l.Accept()
		assert.MustNoError(err)
		cc <- redis.NewConn(c, bufsize, bufsize)
	}()

	bc := NewBackendConn(l.Addr().String(), 0, config)
	return <-cc, bc
}

func TestBackend(t *testing.T) {
	config := NewDefaultConfig()
	config.BackendMaxPipeline = 0
	config.BackendSendTimeout.Set(time.Second)
	config.BackendRecvTimeout.Set(time.Minute)

	conn, bc := newConnPair(config)

	var array = make([]*Request, 16384)
	for i := range array {
		array[i] = &Request{Batch: &sync.WaitGroup{}}
	}

	go func() {
		defer conn.Close()
		time.Sleep(time.Millisecond * 300)
		for i, _ := range array {
			_, err := conn.Decode()
			assert.MustNoError(err)
			resp := redis.NewString([]byte(strconv.Itoa(i)))
			assert.MustNoError(conn.Encode(resp, true))
		}
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
		assert.MustNoError(r.Err)
		assert.Must(r.Resp != nil)
		assert.Must(string(r.Resp.Value) == strconv.Itoa(i))
	}
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
