// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
	"github.com/CodisLabs/codis/pkg/utils/sync2/atomic2"
	"github.com/CodisLabs/codis/pkg/utils/trace"
)

const (
	MethodGet  = "GET"
	MethodPut  = "PUT"
	MethodPost = "POST"
)

var (
	client *http.Client

	defaultIdleCleaner *idleConnCleaner
)

func init() {
	var dials atomic2.Int64
	tr := &http.Transport{}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, addr)
		if err == nil {
			log.Debugf("rpc: dial new connection to [%d] %s - %s",
				dials.Incr()-1, network, addr)
		}
		return c, err
	}
	client = &http.Client{
		Transport: tr,
		Timeout:   time.Minute,
	}
	defaultIdleCleaner = newIdleConnCleaner(tr.CloseIdleConnections, time.Minute)
}

// idleConnCleaner periodically drops idle connections of a transport. It has an
// explicit Start/Close lifecycle so a caller (a test, or a process shutdown
// path) can stop it and wait for the loop instead of leaking a goroutine that
// sleeps forever.
type idleConnCleaner struct {
	closeIdle func()
	interval  time.Duration

	mu   sync.Mutex
	stop chan struct{}
	wg   sync.WaitGroup
}

func newIdleConnCleaner(closeIdle func(), interval time.Duration) *idleConnCleaner {
	return &idleConnCleaner{closeIdle: closeIdle, interval: interval}
}

// Start runs the cleanup loop. It is a no-op while the loop is running.
func (c *idleConnCleaner) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stop != nil {
		return
	}
	stop := make(chan struct{})
	c.stop = stop

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer utils.Recover("rpc idle connection cleaner")
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				c.closeIdle()
			}
		}
	}()
}

// Close stops the cleanup loop, waits for it and drops the idle connections
// once more. It is safe to call without Start and safe to call twice. The
// mutex is held across stop and wait so a concurrent Start cannot attach a new
// loop to the WaitGroup Close is waiting on.
func (c *idleConnCleaner) Close() error {
	c.mu.Lock()
	if c.stop != nil {
		close(c.stop)
		c.stop = nil
		c.wg.Wait()
	}
	c.mu.Unlock()

	c.closeIdle()
	return nil
}

func (c *idleConnCleaner) running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stop != nil
}

// CloseDefaultClient stops the shared client's idle connection cleanup loop and
// drops idle connections. Requests already in flight are unaffected, and a
// later request starts the loop again. Shutdown paths and tests should call it
// so no background goroutine outlives the process work.
func CloseDefaultClient() error {
	return defaultIdleCleaner.Close()
}

// ApiPostJsonWithContext posts args to url and aborts the request when ctx is
// canceled, which keeps metrics reporters from blocking proxy shutdown on a
// slow remote endpoint.
func ApiPostJsonWithContext(ctx context.Context, url string, args any) error {
	return apiRequestJsonContext(ctx, client, MethodPost, url, args, nil)
}

type RemoteError struct {
	Cause string
	Stack trace.Stack
}

func (e *RemoteError) Error() string {
	return e.Cause
}

func (e *RemoteError) TracedError() error {
	return &errors.TracedError{
		Cause: errors.New("[Remote Error] " + e.Cause),
		Stack: e.Stack,
	}
}

func NewRemoteError(err error) *RemoteError {
	if err == nil {
		return nil
	}
	if v, ok := err.(*RemoteError); ok {
		return v
	}
	return &RemoteError{
		Cause: err.Error(),
		Stack: errors.Stack(err),
	}
}

func responseBodyAsBytes(rsp *http.Response) ([]byte, error) {
	b, err := io.ReadAll(rsp.Body)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return b, nil
}

func responseBodyAsError(rsp *http.Response) (error, error) {
	b, err := responseBodyAsBytes(rsp)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.Errorf("remote error is empty")
	}
	e := &RemoteError{}
	if err := json.Unmarshal(b, e); err != nil {
		return nil, errors.Trace(err)
	}
	return e.TracedError(), nil
}

func apiMarshalJson(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "    ")
}

func apiRequestJson(method string, url string, args, reply any) error {
	defaultIdleCleaner.Start()
	return apiRequestJsonContext(context.Background(), client, method, url, args, reply)
}

func redactRPCURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parts := strings.Split(u.Path, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "api" && (parts[i+1] == "proxy" || parts[i+1] == "topom") {
			parts[i+2] = ":xauth"
			break
		}
	}
	u.Path = strings.Join(parts, "/")
	return u.String()
}

func apiRequestJsonWithClient(c *http.Client, method string, url string, args, reply any) error {
	return apiRequestJsonContext(context.Background(), c, method, url, args, reply)
}

func apiRequestJsonContext(ctx context.Context, c *http.Client, method string, url string, args, reply any) error {
	var body []byte
	if args != nil {
		b, err := apiMarshalJson(args)
		if err != nil {
			return errors.Trace(err)
		}
		body = b
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return errors.Trace(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	}

	var start = time.Now()

	rsp, err := c.Do(req)
	if err != nil {
		return errors.Trace(err)
	}
	defer func() {
		io.Copy(io.Discard, rsp.Body)
		rsp.Body.Close()
		log.Debugf("call rpc [%s] %s in %v", method, redactRPCURL(url), time.Since(start))
	}()

	switch rsp.StatusCode {
	case 200:
		b, err := responseBodyAsBytes(rsp)
		if err != nil {
			return err
		}
		if reply == nil {
			return nil
		}
		if err := json.Unmarshal(b, reply); err != nil {
			return errors.Trace(err)
		} else {
			return nil
		}
	case 800, 1500:
		e, err := responseBodyAsError(rsp)
		if err != nil {
			return err
		} else {
			return e
		}
	default:
		return errors.Errorf("[%d] %s - %s", rsp.StatusCode, http.StatusText(rsp.StatusCode), url)
	}
}

func ApiGetJson(url string, reply any) error {
	return apiRequestJson(MethodGet, url, nil, reply)
}

func ApiPutJson(url string, args, reply any) error {
	return apiRequestJson(MethodPut, url, args, reply)
}

func ApiPutJsonWithTimeout(url string, args, reply any, timeout time.Duration) error {
	if timeout <= 0 {
		return ApiPutJson(url, args, reply)
	}
	c := &http.Client{
		Transport: client.Transport,
		Timeout:   timeout,
	}
	return apiRequestJsonWithClient(c, MethodPut, url, args, reply)
}

func ApiPostJson(url string, args any) error {
	return apiRequestJson(MethodPost, url, args, nil)
}

func ApiResponseError(err error) (int, string) {
	if err == nil {
		return 800, ""
	}
	b, err := apiMarshalJson(NewRemoteError(err))
	if err != nil {
		return 800, ""
	} else {
		return 800, string(b)
	}
}

func ApiResponseJson(v any) (int, string) {
	b, err := apiMarshalJson(v)
	if err != nil {
		return ApiResponseError(errors.Trace(err))
	} else {
		return 200, string(b)
	}
}

func EncodeURL(host string, format string, args ...any) string {
	var u url.URL
	u.Scheme = "http"
	u.Host = host
	u.Path = fmt.Sprintf(format, args...)
	return u.String()
}
