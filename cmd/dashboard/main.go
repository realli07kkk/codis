// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
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
	codis-dashboard [--ncpu=N] [--config=CONF] [--log=FILE] [--log-level=LEVEL] [--host-admin=ADDR] [--pidfile=FILE] [--zookeeper=ADDR|--filesystem=ROOT|--consul=ADDR] [--product_name=NAME] [--product_auth=AUTH] [--remove-lock]
	codis-dashboard  --default-config
	codis-dashboard  --version

Options:
	--ncpu=N                    set runtime.GOMAXPROCS to N, default is runtime.NumCPU().
	-c CONF, --config=CONF      run with the specific configuration.
	-l FILE, --log=FILE         set path/name of daliy rotated log file.
	--log-level=LEVEL           set the log-level, should be INFO,WARN,DEBUG or ERROR, default is INFO.
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

	if cfg.HasCoordinator {
		config.CoordinatorName = cfg.CoordinatorArg.Name
		config.CoordinatorAddr = cfg.CoordinatorArg.Addr
		if cfg.CoordinatorArg.HasAuth {
			config.CoordinatorAuth = cfg.CoordinatorArg.Auth
		}
		log.Warnf("option --%s = %s", cfg.CoordinatorArg.Name, cfg.CoordinatorArg.Addr)
	}

	if cfg.ProductName.Provided {
		config.ProductName = cfg.ProductName.Value
		log.Warnf("option --product_name = %s", config.ProductName)
	}
	if cfg.ProductAuth.Provided {
		config.ProductAuth = cfg.ProductAuth.Value
		log.Warnf("option --product_auth = %s", config.ProductAuth)
	}

	client, err := models.NewClient(config.CoordinatorName, config.CoordinatorAddr, config.CoordinatorAuth, time.Minute)
	if err != nil {
		log.PanicErrorf(err, "create '%s' client to '%s' failed", config.CoordinatorName, config.CoordinatorAddr)
	}
	defer client.Close()

	if cfg.RemoveLock.Value {
		store := models.NewStore(client, config.ProductName)
		defer store.Close()

		log.Warnf("force remove-lock")
		if err := store.Release(); err != nil {
			log.WarnErrorf(err, "force remove-lock failed")
		} else {
			log.Warnf("force remove-lock OK")
		}
	}

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

	for i := 0; !s.IsClosed() && !s.IsOnline(); i++ {
		if err := s.Start(true); err != nil {
			if i <= 15 {
				log.Warnf("[%p] dashboard online failed [%d]", s, i)
			} else {
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
