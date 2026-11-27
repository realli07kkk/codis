// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"fmt"
	"os"

	"github.com/CodisLabs/codis/pkg/utils/log"
)

// adminUsage documents the codis-admin command-line contract. It is kept as
// the single source of truth for --help output; parseAdminArgs maps every
// documented line to explicit flag/required/mutual-exclusion validation.
var adminUsage = `
Usage:
	codis-admin [-v] --proxy=ADDR [--auth=AUTH] [config|model|stats|slots]
	codis-admin [-v] --proxy=ADDR [--auth=AUTH]  --start
	codis-admin [-v] --proxy=ADDR [--auth=AUTH]  --shutdown
	codis-admin [-v] --proxy=ADDR [--auth=AUTH]  --log-level=LEVEL
	codis-admin [-v] --proxy=ADDR [--auth=AUTH]  --fillslots=FILE [--locked]
	codis-admin [-v] --proxy=ADDR [--auth=AUTH]  --reset-stats
	codis-admin [-v] --proxy=ADDR [--auth=AUTH]  --forcegc
	codis-admin [-v] --dashboard=ADDR           [config|model|stats|slots|group|proxy]
	codis-admin [-v] --dashboard=ADDR            --shutdown
	codis-admin [-v] --dashboard=ADDR            --reload
	codis-admin [-v] --dashboard=ADDR            --log-level=LEVEL
	codis-admin [-v] --dashboard=ADDR            --slots-assign   --beg=ID --end=ID (--gid=ID|--offline) [--confirm]
	codis-admin [-v] --dashboard=ADDR            --slots-status
	codis-admin [-v] --dashboard=ADDR            --list-proxy
	codis-admin [-v] --dashboard=ADDR            --create-proxy   --addr=ADDR
	codis-admin [-v] --dashboard=ADDR            --online-proxy   --addr=ADDR
	codis-admin [-v] --dashboard=ADDR            --remove-proxy  (--addr=ADDR|--token=TOKEN|--pid=ID)       [--force]
	codis-admin [-v] --dashboard=ADDR            --reinit-proxy  (--addr=ADDR|--token=TOKEN|--pid=ID|--all) [--force]
	codis-admin [-v] --dashboard=ADDR            --proxy-status
	codis-admin [-v] --dashboard=ADDR            --list-group
	codis-admin [-v] --dashboard=ADDR            --create-group   --gid=ID
	codis-admin [-v] --dashboard=ADDR            --remove-group   --gid=ID
	codis-admin [-v] --dashboard=ADDR            --resync-group  [--gid=ID | --all]
	codis-admin [-v] --dashboard=ADDR            --group-add      --gid=ID --addr=ADDR [--datacenter=DATACENTER]
	codis-admin [-v] --dashboard=ADDR            --group-del      --gid=ID --addr=ADDR
	codis-admin [-v] --dashboard=ADDR            --group-status
	codis-admin [-v] --dashboard=ADDR            --replica-groups --gid=ID --addr=ADDR (--enable|--disable)
	codis-admin [-v] --dashboard=ADDR            --promote-server --gid=ID --addr=ADDR
	codis-admin [-v] --dashboard=ADDR            --sync-action    --create --addr=ADDR
	codis-admin [-v] --dashboard=ADDR            --sync-action    --remove --addr=ADDR
	codis-admin [-v] --dashboard=ADDR            --slot-action    --create --sid=ID --gid=ID
	codis-admin [-v] --dashboard=ADDR            --slot-action    --remove --sid=ID
	codis-admin [-v] --dashboard=ADDR            --slot-action    --create-some  --gid-from=ID --gid-to=ID --num-slots=N
	codis-admin [-v] --dashboard=ADDR            --slot-action    --create-range --beg=ID --end=ID --gid=ID
	codis-admin [-v] --dashboard=ADDR            --slot-action    --interval=VALUE
	codis-admin [-v] --dashboard=ADDR            --slot-action    --disabled=VALUE
	codis-admin [-v] --dashboard=ADDR            --rebalance     [--confirm]
	codis-admin [-v] --dashboard=ADDR            --acl
	codis-admin [-v] --dashboard=ADDR            --acl-set=FILE  [--confirm]
	codis-admin [-v] --dashboard=ADDR            --acl-sync
	codis-admin [-v] --dashboard=ADDR            --rdb-analysis-remote-fetch --server=ADDR [--topn=N] [--prefix-sep=SEP] [--max-depth=N] [--regex=REGEX] [--include-expired]
	codis-admin [-v] --dashboard=ADDR            --pitr-create   --server=ADDR --truncate-ts=TS
	codis-admin [-v] --dashboard=ADDR            --pitr-list
	codis-admin [-v] --dashboard=ADDR            --pitr-get      --job=ID
	codis-admin [-v] --dashboard=ADDR            --pitr-cancel   --job=ID
	codis-admin [-v] --dashboard=ADDR            --pitr-remove   --job=ID
	codis-admin [-v] --dashboard=ADDR            --sentinel-add   --addr=ADDR
	codis-admin [-v] --dashboard=ADDR            --sentinel-del   --addr=ADDR [--force]
	codis-admin [-v] --dashboard=ADDR            --sentinel-resync
	codis-admin [-v] --show-lock                --product=NAME (--etcd=URL[,URL...]|--etcd-config=FILE)
	codis-admin [-v] --remove-lock              --product=NAME --expected-revision=REV --confirm (--etcd=URL[,URL...]|--etcd-config=FILE)
	codis-admin [-v] --config-dump              --product=NAME (--etcd=URL[,URL...]|--etcd-config=FILE)
	codis-admin [-v] --config-convert=FILE
	codis-admin [-v] --config-restore=FILE      --product=NAME (--etcd=URL[,URL...]|--etcd-config=FILE) [--confirm]
	codis-admin [-v] --dashboard-list                           (--etcd=URL[,URL...]|--etcd-config=FILE)

Examples:
	codis-admin --etcd-config=etcd.toml --product=demo --show-lock
	codis-admin --etcd-config=etcd.toml --product=demo --remove-lock --expected-revision=417 --confirm

Options:
	-a AUTH, --auth=AUTH
	-x ADDR, --addr=ADDR
	-t TOKEN, --token=TOKEN
	-g ID, --gid=ID
	--etcd=URL[,URL...]         etcd endpoints; takes precedence over --etcd-config
	--etcd-config=FILE          etcd connection file (endpoints, credentials, TLS)
	--product=NAME              product whose metadata is accessed directly
	--expected-revision=REV     create_revision of the lock --remove-lock may delete
	--confirm                   confirm a destructive command
	--server=ADDR
	--topn=N
	--prefix-sep=SEP
	--max-depth=N
	--regex=REGEX
	--acl-set=FILE
	--truncate-ts=TS
	--job=ID

The commands that read the metadata directly (--show-lock, --remove-lock,
--config-dump, --config-restore, --dashboard-list) need at least one of
--etcd and --etcd-config. zookeeper, filesystem and consul are no longer
supported.
`

func main() {
	cfg, help, err := parseAdminArgs(os.Args[1:])
	if err != nil {
		log.PanicError(err, "parse arguments failed")
	}
	if help {
		fmt.Print(adminUsage)
		return
	}
	log.SetLevel(log.LevelInfo)

	if cfg.Verbose.Value {
		log.SetLevel(log.LevelDebug)
	}

	switch {
	case cfg.Proxy.Provided:
		new(cmdProxy).Main(cfg)
	case cfg.Dashboard.Provided:
		new(cmdDashboard).Main(cfg)
	default:
		new(cmdAdmin).Main(cfg)
	}
}
