// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	stderrors "errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/topom"
	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// dashboardUsage documents the codis-dashboard command-line contract. It is
// kept as the single source of truth for --help output and for the flag names
// pinned by the parser tests.
const dashboardUsage = `
Usage:
	codis-dashboard [--ncpu=N] [--config=CONF] [--log=FILE] [--log-level=LEVEL] [--host-admin=ADDR] [--pidfile=FILE] [--etcd=URL[,URL...]] [--etcd-config=FILE] [--product_name=NAME] [--product_auth=AUTH]
	codis-dashboard  --default-config
	codis-dashboard  --version

Options:
	--ncpu=N                    set runtime.GOMAXPROCS to N, default is runtime.NumCPU().
	-c CONF, --config=CONF      run with the specific configuration.
	-l FILE, --log=FILE         set path/name of daliy rotated log file.
	--log-level=LEVEL           set the log-level, should be INFO,WARN,DEBUG or ERROR, default is INFO.
	--etcd=URL[,URL...]         etcd endpoints, comma separated. Takes precedence over
	                            --etcd-config and over coordinator_addr from --config.
	--etcd-config=FILE          etcd connection file (endpoints, credentials, TLS).
	                            Its endpoints take precedence over coordinator_addr.

The endpoints are resolved in this order: an explicit --etcd value, then the
endpoints of the --etcd-config file, then coordinator_addr from --config, and
finally http://127.0.0.1:2379. An invalid value is rejected before any
connection is attempted. Coordinator credentials and TLS material belong in
the --etcd-config file; zookeeper, filesystem and consul are no longer
supported.
`

func main() {
	cfg, help, err := parseDashboardArgs(os.Args[1:])
	if err != nil {
		log.PanicError(err, "parse arguments failed")
	}
	if help {
		fmt.Print(dashboardUsage)
		return
	}

	switch {

	case cfg.DefaultConfig.Value:
		fmt.Print(topom.DefaultConfig)
		return

	case cfg.Version.Value:
		fmt.Println("version:", utils.Version)
		fmt.Println("compile:", utils.Compile)
		return

	}

	if cfg.Log.Provided {
		w, err := log.NewRollingFile(cfg.Log.Value, log.DailyRolling)
		if err != nil {
			log.PanicErrorf(err, "open log file %s failed", cfg.Log.Value)
		} else {
			log.SetStdLog(log.New(w, ""))
		}
	}
	log.SetLevel(log.LevelInfo)

	if cfg.LogLevel.Provided {
		if !log.SetLevelString(cfg.LogLevel.Value) {
			log.Panicf("option --log-level = %s", cfg.LogLevel.Value)
		}
	}

	if cfg.NCPU.Provided {
		n, err := strconv.Atoi(cfg.NCPU.Value)
		if err != nil {
			log.PanicErrorf(err, "option --ncpu isn't a valid integer")
		}
		runtime.GOMAXPROCS(n)
	} else {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}
	log.Warnf("set ncpu = %d", runtime.GOMAXPROCS(0))

	config := topom.NewDefaultConfig()
	if cfg.Config.Provided {
		if err := config.LoadFromFile(cfg.Config.Value); err != nil {
			log.PanicErrorf(err, "load config %s failed", cfg.Config.Value)
		}
	}
	if cfg.HostAdmin.Provided {
		config.HostAdmin = cfg.HostAdmin.Value
		log.Warnf("option --host-admin = %s", config.HostAdmin)
	}

	if cfg.ProductName.Provided {
		config.ProductName = cfg.ProductName.Value
		log.Warnf("option --product_name = %s", config.ProductName)
	}
	if cfg.ProductAuth.Provided {
		config.ProductAuth = cfg.ProductAuth.Value
		log.Warnf("option --product_auth=%s", credentialState(config.ProductAuth))
	}

	// Every connection value comes from the flags or their file; the dashboard
	// coordinator_addr is only the last fallback for the endpoints.
	client, err := cfg.CoordinatorArg.NewClient(config.CoordinatorAddr)
	if err != nil {
		log.PanicErrorf(err, "create etcd client failed")
	}
	defer client.Close()

	s, err := topom.New(client, config)
	if err != nil {
		log.PanicErrorf(err, "create topom with config file failed\n%s", config)
	}
	defer s.Close()

	log.Warnf("create topom with config\n%s", config)

	if cfg.Pidfile.Provided {
		if pidfile, err := filepath.Abs(cfg.Pidfile.Value); err != nil {
			log.WarnErrorf(err, "parse pidfile = '%s' failed", cfg.Pidfile.Value)
		} else if err := os.WriteFile(pidfile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			log.WarnErrorf(err, "write pidfile = '%s' failed", pidfile)
		} else {
			defer func() {
				if err := os.Remove(pidfile); err != nil {
					log.WarnErrorf(err, "remove pidfile = '%s' failed", pidfile)
				}
			}()
			log.Warnf("option --pidfile = %s", pidfile)
		}
	}

	go func() {
		defer s.Close()
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		sig := <-c
		log.Warnf("[%p] dashboard receive signal = '%v'", s, sig)
	}()

	// The lock is taken by the first Start: it is the pre-flight check that the
	// role may write, and no admin write path is opened before it succeeds.
	store := models.NewStore(client, config.ProductName)

	for i := 0; !s.IsClosed() && !s.IsOnline(); i++ {
		if err := s.Start(true); err != nil {
			switch {
			case models.IsRejected(err):
				// Permission/authentication failures cannot be fixed by a
				// retry: report the reason and stop before listening.
				log.PanicErrorf(err, "[%p] dashboard startup rejected by the coordinator", s)
			case stderrors.Is(err, models.ErrLockHeld):
				logLockHolder(store, err)
			case stderrors.Is(err, models.ErrResultUnknown):
				// A later Start re-reads the lock and recovers the one this
				// instance owns, so the outcome is retried, never assumed.
				log.WarnErrorf(err, "[%p] dashboard lock state is unknown, will retry [%d]", s, i)
			default:
				log.WarnErrorf(err, "[%p] dashboard online failed [%d]", s, i)
			}
			if i > 15 {
				log.Panicf("dashboard online failed, give up & abort :'(")
			}
			select {
			case <-s.Done():
			case <-time.After(time.Second * 2):
			}
		}
	}

	log.Warnf("[%p] dashboard is working ...", s)

	<-s.Done()

	log.Warnf("[%p] dashboard is exiting ...", s)
}

// credentialState reports whether a credential option is in effect without
// rendering its value, so process logs never carry the secret.
func credentialState(v string) string {
	if v == "" {
		return "<unset>"
	}
	return "<set>"
}

// logLockHolder reports which instance holds the product lock. It is a
// best-effort diagnostic: a failed read must never turn the conflict into
// anything else, and only the lock holder fields are logged.
func logLockHolder(store *models.Store, err error) {
	info, showErr := store.ShowLock()
	switch {
	case showErr != nil:
		log.WarnErrorf(err, "another dashboard holds the lock; reading the holder failed")
	case info == nil || info.Topom == nil:
		log.WarnErrorf(err, "another dashboard holds the lock; the holder is not readable")
	default:
		log.WarnErrorf(err, "another dashboard holds the lock: admin_addr=%s owner_id=%s create_revision=%d",
			info.Topom.AdminAddr, info.Topom.OwnerID, info.CreateRevision)
	}
}
