// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	"os"
	"testing"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/models/fs"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/utils/assert"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

var config = NewDefaultConfig()

func init() {
	log.SetLevel(log.LevelError)
}

func init() {
	config.AdminAddr = "127.0.0.1:0"
	config.ProductName = "topom_test"
	config.ProductAuth = "topom_auth"
}

var testRootDir string

func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "codis-topom-test-")
	if err != nil {
		log.PanicError(err, "create temp root failed")
	}
	testRootDir = d
	code := m.Run()
	os.RemoveAll(d)
	os.Exit(code)
}

func newDiskClient(t *testing.T) *fsclient.Client {
	d, err := os.MkdirTemp(testRootDir, "")
	assert.MustNoError(t, err)
	c, err := fsclient.New(d)
	assert.MustNoError(t, err)
	return c
}

func newForkClient(t *testing.T, client *fsclient.Client) *fsclient.Client {
	c, err := fsclient.New(client.RootDir)
	assert.MustNoError(t, err)
	return c
}

func openTopom(x *testing.T) *Topom {
	t, err := New(newDiskClient(x), config)
	assert.MustNoError(x, err)
	assert.MustNoError(x, t.Start(false))
	return t
}

func openProxy(t *testing.T) (*models.Proxy, *proxy.ApiClient) {
	config := proxy.NewDefaultConfig()
	config.AdminAddr = "127.0.0.1:0"
	config.ProxyAddr = "0.0.0.0:0"
	config.ProductName = "topom_test"
	config.ProductAuth = "topom_auth"
	config.ProxyHeapPlaceholder = 0
	config.ProxyMaxOffheapBytes = 0

	s, err := proxy.New(config)
	assert.MustNoError(t, err)

	c := proxy.NewApiClient(s.Model().AdminAddr)
	c.SetXAuth(config.ProductName, config.ProductAuth, s.Model().Token)

	p, err := c.Model()
	assert.MustNoError(t, err)
	return p, c
}

func TestTopomClose(x *testing.T) {
	t := openTopom(x)
	defer t.Close()

	assert.Must(x, t.IsClosed() == false)
	assert.MustNoError(x, t.Close())
	assert.Must(x, t.IsClosed() == true)
}

func TestTopomExclusive(x *testing.T) {
	client := newDiskClient(x)

	t1, err := New(newForkClient(x, client), config)
	assert.MustNoError(x, err)
	assert.MustNoError(x, t1.Start(false))

	defer t1.Close()

	t2, err := New(newForkClient(x, client), config)
	assert.MustNoError(x, err)
	assert.Must(x, t2.Start(false) != nil)

	t1.Close()

	t3, err := New(newForkClient(x, client), config)
	assert.MustNoError(x, err)
	assert.MustNoError(x, t3.Start(false))
}
