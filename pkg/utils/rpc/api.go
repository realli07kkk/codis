// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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

var client *http.Client

func init() {
	var dials atomic2.Int64
	tr := &http.Transport{}
	tr.Dial = func(network, addr string) (net.Conn, error) {
		c, err := net.DialTimeout(network, addr, time.Second)
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
	go func() {
		for {
			time.Sleep(time.Minute)
			tr.CloseIdleConnections()
		}
	}()
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
	return apiRequestJsonWithClient(client, method, url, args, reply)
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
	var body []byte
	if args != nil {
		b, err := apiMarshalJson(args)
		if err != nil {
			return errors.Trace(err)
		}
		body = b
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
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
