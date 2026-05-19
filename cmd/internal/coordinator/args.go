// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package coordinator

import (
	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

type Args struct {
	Name    string
	Addr    string
	Auth    string
	HasAuth bool
}

type option struct {
	name     string
	addrFlag string
	authFlag string
}

var options = []option{
	{name: "zookeeper", addrFlag: "--zookeeper", authFlag: "--zookeeper-auth"},
	{name: "etcd", addrFlag: "--etcd", authFlag: "--etcd-auth"},
	{name: "filesystem", addrFlag: "--filesystem"},
	{name: "consul", addrFlag: "--consul", authFlag: "--consul-auth"},
}

func Parse(d map[string]interface{}) (Args, bool) {
	for _, option := range options {
		if d[option.addrFlag] == nil {
			continue
		}

		args := Args{
			Name: option.name,
			Addr: utils.ArgumentMust(d, option.addrFlag),
		}
		if option.authFlag != "" && d[option.authFlag] != nil {
			args.Auth = utils.ArgumentMust(d, option.authFlag)
			args.HasAuth = true
		}
		return args, true
	}
	return Args{}, false
}

func MustParse(d map[string]interface{}) Args {
	args, ok := Parse(d)
	if !ok {
		log.Panicf("invalid coordinator")
	}
	return args
}
