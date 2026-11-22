// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/topom"
	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// proxyUsage documents the codis-proxy command-line contract. It is kept as
// the single source of truth for --help output and for the flag names pinned
// by the parser tests.
const proxyUsage = `
Usage:
	codis-proxy [--ncpu=N [--max-ncpu=MAX]] [--config=CONF] [--log=FILE] [--log-level=LEVEL] [--host-admin=ADDR] [--host-proxy=ADDR] [--dashboard=ADDR|--zookeeper=ADDR [--zookeeper-auth=USR:PWD]|--etcd=ADDR [--etcd-auth=USR:PWD]|--filesystem=ROOT|--consul=ADDR [--consul-auth=TOKEN]|--fillslots=FILE] [--ulimit=NLIMIT] [--pidfile=FILE] [--product_name=NAME] [--product_auth=AUTH] [--session_auth=AUTH]
	codis-proxy  --default-config
	codis-proxy  --version

Options:
	--ncpu=N                    set runtime.GOMAXPROCS to N, default is runtime.NumCPU().
	-c CONF, --config=CONF      run with the specific configuration.
	-l FILE, --log=FILE         set path/name of daliy rotated log file.
	--log-level=LEVEL           set the log-level, should be INFO,WARN,DEBUG or ERROR, default is INFO.
	--ulimit=NLIMIT             run 'ulimit -n' to check the maximum number of open file descriptors.
`

func main() {
	cfg, help, err := parseProxyArgs(os.Args[1:])
	if err != nil {
		log.PanicError(err, "parse arguments failed")
	}
	if help {
		fmt.Print(proxyUsage)
		return
	}

	switch {

	case cfg.DefaultConfig.Value:
		fmt.Print(proxy.DefaultConfig)
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

	if cfg.Ulimit.Provided {
		n, err := strconv.Atoi(cfg.Ulimit.Value)
		if err != nil {
			log.PanicErrorf(err, "option --ulimit isn't a valid integer")
		}
		b, err := exec.Command("/bin/sh", "-c", "ulimit -n").Output()
		if err != nil {
			log.PanicErrorf(err, "run ulimit -n failed")
		}
		if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err != nil || v < n {
			log.PanicErrorf(err, "ulimit too small: %d, should be at least %d", v, n)
		}
	}

	var ncpu int
	if cfg.NCPU.Provided {
		n, err := strconv.Atoi(cfg.NCPU.Value)
		if err != nil {
			log.PanicErrorf(err, "option --ncpu isn't a valid integer")
		}
		ncpu = n
	} else {
		ncpu = 4
	}
	runtime.GOMAXPROCS(ncpu)

	var maxncpu int
	if cfg.MaxNCPU.Provided {
		n, err := strconv.Atoi(cfg.MaxNCPU.Value)
		if err != nil {
			log.PanicErrorf(err, "option --max-ncpu isn't a valid integer")
		}
		maxncpu = max(ncpu, n)
	} else {
		maxncpu = max(ncpu, runtime.NumCPU())
	}
	log.Warnf("set ncpu = %d, max-ncpu = %d", ncpu, maxncpu)

	if ncpu < maxncpu {
		go AutoGOMAXPROCS(ncpu, maxncpu)
	}

	config := proxy.NewDefaultConfig()
	if cfg.Config.Provided {
		if err := config.LoadFromFile(cfg.Config.Value); err != nil {
			log.PanicErrorf(err, "load config %s failed", cfg.Config.Value)
		}
	}
	if cfg.HostAdmin.Provided {
		config.HostAdmin = cfg.HostAdmin.Value
		log.Warnf("option --host-admin = %s", cfg.HostAdmin.Value)
	}
	if cfg.HostProxy.Provided {
		config.HostProxy = cfg.HostProxy.Value
		log.Warnf("option --host-proxy = %s", cfg.HostProxy.Value)
	}

	var dashboard string
	if cfg.Dashboard.Provided {
		dashboard = cfg.Dashboard.Value
		log.Warnf("option --dashboard = %s", dashboard)
	}

	if cfg.HasCoordinator {
		log.Warnf("option --%s = %s", cfg.CoordinatorArg.Name, cfg.CoordinatorArg.Addr)
	}

	var slots []*models.Slot
	if cfg.FillSlots.Provided {
		b, err := os.ReadFile(cfg.FillSlots.Value)
		if err != nil {
			log.PanicErrorf(err, "load slots from file failed")
		}
		if err := json.Unmarshal(b, &slots); err != nil {
			log.PanicErrorf(err, "decode slots from json failed")
		}
	}

	if cfg.ProductName.Provided {
		config.ProductName = cfg.ProductName.Value
		log.Warnf("option --product_name = %s", config.ProductName)
	}
	if cfg.ProductAuth.Provided {
		config.ProductAuth = cfg.ProductAuth.Value
		log.Warnf("option --product_auth = %s", config.ProductAuth)
	}
	if cfg.SessionAuth.Provided {
		config.SessionAuth = cfg.SessionAuth.Value
		log.Warnf("option --session_auth = %s", config.SessionAuth)
	}

	s, err := proxy.New(config)
	if err != nil {
		log.PanicErrorf(err, "create proxy with config file failed\n%s", config)
	}
	defer s.Close()

	log.Warnf("create proxy with config\n%s", config)

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
		// Restore the default disposition so a second termination signal can
		// still stop the process if graceful Close is waiting on a backend.
		signal.Stop(c)
		log.Warnf("[%p] proxy receive signal = '%v'", s, sig)
	}()

	switch {
	case dashboard != "":
		go AutoOnlineWithDashboard(s, dashboard)
	case cfg.HasCoordinator:
		go AutoOnlineWithCoordinator(s, cfg.CoordinatorArg.Name, cfg.CoordinatorArg.Addr, cfg.CoordinatorArg.Auth)
	case slots != nil:
		go AutoOnlineWithFillSlots(s, slots)
	}

	for !s.IsClosed() && !s.IsOnline() {
		log.Warnf("[%p] proxy waiting online ...", s)
		select {
		case <-s.Done():
		case <-time.After(time.Second):
		}
	}

	log.Warnf("[%p] proxy is working ...", s)

	<-s.Done()

	log.Warnf("[%p] proxy is exiting ...", s)
}

func AutoGOMAXPROCS(min, max int) {
	for {
		var ncpu = runtime.GOMAXPROCS(0)
		var less, more int
		var usage [10]float64
		for i := 0; i < len(usage) && more == 0; i++ {
			u, _, err := utils.CPUUsage(time.Second)
			if err != nil {
				log.WarnErrorf(err, "get cpu usage failed")
				time.Sleep(time.Second * 30)
				continue
			}
			u /= float64(ncpu)
			switch {
			case u < 0.30 && ncpu > min:
				less++
			case u > 0.70 && ncpu < max:
				more++
			}
			usage[i] = u
		}
		var nn = ncpu
		switch {
		case more != 0:
			nn = ncpu + ((max - ncpu + 3) / 4)
		case less == len(usage):
			nn = ncpu - 1
		}
		if nn != ncpu {
			runtime.GOMAXPROCS(nn)
			var b bytes.Buffer
			for i, u := range usage {
				if i != 0 {
					fmt.Fprintf(&b, ", ")
				}
				fmt.Fprintf(&b, "%.3f", u)
			}
			log.Warnf("ncpu = %d -> %d, usage = [%s]", ncpu, nn, b.Bytes())
		}
	}
}

func AutoOnlineWithDashboard(p *proxy.Proxy, dashboard string) {
	for i := 0; i < 10; i++ {
		if p.IsClosed() || p.IsOnline() {
			return
		}
		if OnlineProxy(p, dashboard) {
			return
		}
		time.Sleep(time.Second * 3)
	}
	log.Panicf("online proxy failed")
}

func AutoOnlineWithCoordinator(p *proxy.Proxy, name, addr, auth string) {
	client, err := models.NewClient(name, addr, auth, time.Minute)
	if err != nil {
		log.PanicErrorf(err, "create '%s' client to '%s' failed", name, addr)
	}
	defer client.Close()
	for i := 0; i < 30; i++ {
		if p.IsClosed() || p.IsOnline() {
			return
		}
		t, err := models.LoadTopom(client, p.Config().ProductName, false)
		if err != nil {
			log.WarnErrorf(err, "load & decode topom failed")
		} else if t != nil && OnlineProxy(p, t.AdminAddr) {
			return
		}
		time.Sleep(time.Second * 3)
	}
	log.Panicf("online proxy failed")
}

func AutoOnlineWithFillSlots(p *proxy.Proxy, slots []*models.Slot) {
	if err := p.FillSlots(slots); err != nil {
		log.PanicErrorf(err, "fill slots failed")
	}
	if err := p.Start(); err != nil {
		log.PanicErrorf(err, "start proxy failed")
	}
}

func OnlineProxy(p *proxy.Proxy, dashboard string) bool {
	client := topom.NewApiClient(dashboard)
	t, err := client.Model()
	if err != nil {
		log.WarnErrorf(err, "rpc fetch model failed")
		return false
	}
	if t.ProductName != p.Config().ProductName {
		log.Panicf("unexcepted product name, got model =\n%s", t.Encode())
	}
	client.SetXAuth(p.Config().ProductName)

	if err := client.OnlineProxy(p.Model().AdminAddr); err != nil {
		log.WarnErrorf(err, "rpc online proxy failed")
		return false
	} else {
		p.SetTopomAdminAddr(t.AdminAddr)
		log.Warnf("rpc online proxy seems OK")
		return true
	}
}
