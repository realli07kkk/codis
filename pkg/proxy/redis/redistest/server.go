// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package redistest

import (
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/CodisLabs/codis/pkg/proxy/redis"
)

type Resp = redis.Resp

type Handler func([]string) *Resp

type Server struct {
	t       testing.TB
	ln      net.Listener
	handler Handler

	mu       sync.Mutex
	commands [][]string
}

func NewServer(t testing.TB, handler Handler) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{t: t, ln: ln, handler: handler}
	go s.serve()
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func (s *Server) Addr() string {
	return s.ln.Addr().String()
}

func (s *Server) Close() error {
	return s.ln.Close()
}

func (s *Server) Commands() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	commands := make([][]string, len(s.commands))
	for i := range s.commands {
		commands[i] = append([]string(nil), s.commands[i]...)
	}
	return commands
}

func (s *Server) CountCommand(name string) int {
	var n int
	for _, cmd := range s.Commands() {
		if strings.EqualFold(cmd[0], name) {
			n++
		}
	}
	return n
}

func (s *Server) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serveConn(c)
	}
}

func (s *Server) serveConn(c net.Conn) {
	defer c.Close()
	dec := redis.NewDecoder(c)
	enc := redis.NewEncoder(c)
	for {
		r, err := dec.Decode()
		if err != nil {
			return
		}
		if r.Type != redis.TypeArray || len(r.Array) == 0 {
			s.t.Errorf("bad request: %v", r)
			return
		}
		args := make([]string, len(r.Array))
		for i, a := range r.Array {
			if a == nil || !a.IsBulkBytes() {
				s.t.Errorf("bad request argument[%d]: %v", i, a)
				return
			}
			args[i] = string(a.Value)
		}
		s.mu.Lock()
		s.commands = append(s.commands, append([]string(nil), args...))
		s.mu.Unlock()

		var resp *Resp
		if s.handler != nil {
			resp = s.handler(args)
		}
		if resp == nil {
			resp = OK()
		}
		if err := enc.Encode(resp, true); err != nil {
			return
		}
	}
}

func Array(items ...*Resp) *Resp {
	return redis.NewArray(items)
}

func Bulk(s string) *Resp {
	return redis.NewBulkBytes([]byte(s))
}

func Int(s string) *Resp {
	return redis.NewInt([]byte(s))
}

func String(s string) *Resp {
	return redis.NewString([]byte(s))
}

func Error(s string) *Resp {
	return redis.NewError([]byte(s))
}

func OK() *Resp {
	return String("OK")
}
