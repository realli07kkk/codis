// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package zkclient_test

import (
	"testing"

	gozk "github.com/go-zookeeper/zk"

	zkclient "github.com/CodisLabs/codis/pkg/models/zk"
)

func TestClientDoSignatureUsesGoZookeeperConn(_ *testing.T) {
	var client *zkclient.Client
	var do func(func(*gozk.Conn) error) error = client.Do
	_ = do
}
