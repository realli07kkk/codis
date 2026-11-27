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

	coordarg "github.com/CodisLabs/codis/cmd/internal/coordinator"
	"github.com/CodisLabs/codis/pkg/models"
	"github.com/CodisLabs/codis/pkg/proxy"
	"github.com/CodisLabs/codis/pkg/proxy/etcdonline"
	"github.com/CodisLabs/codis/pkg/topom"
	"github.com/CodisLabs/codis/pkg/utils"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

// proxyUsage documents the codis-proxy command-line contract. It is kept as
// the single source of truth for --help output and for the flag names pinned
// by the parser tests.
const proxyUsage = `
Usage:
	codis-proxy [--ncpu=N [--max-ncpu=MAX]] [--config=CONF] [--log=FILE] [--log-level=LEVEL] [--host-admin=ADDR] [--host-proxy=ADDR] [--dashboard=ADDR|--etcd=URL[,URL...]|--fillslots=FILE] [--etcd-config=FILE] [--ulimit=NLIMIT] [--pidfile=FILE] [--product_name=NAME] [--product_auth=AUTH] [--session_auth=AUTH]
	codis-proxy  --default-config
	codis-proxy  --version

Options:
	--ncpu=N                    set runtime.GOMAXPROCS to N, default is runtime.NumCPU().
	-c CONF, --config=CONF      run with the specific configuration.
	-l FILE, --log=FILE         set path/name of daliy rotated log file.
	--log-level=LEVEL           set the log-level, should be INFO,WARN,DEBUG or ERROR, default is INFO.
	--dashboard=ADDR            bring the proxy online through the dashboard at ADDR.
	--etcd=URL[,URL...]         bring the proxy online through the dashboard address published in etcd.
	--fillslots=FILE            fill the slots from FILE, then start the proxy.
	--etcd-config=FILE          read the etcd connection options from FILE; the file only supplies
	                            connection options (used by the online registration) and does not
	                            select an online entry.
	--ulimit=NLIMIT             run 'ulimit -n' to check the maximum number of open file descriptors.

The automatic online entries --dashboard, --etcd and --fillslots are mutually
exclusive; when all of them are omitted the proxy waits for the dashboard to
add it manually.

Online registration (TOML etcd_register_online = true) publishes the proxy's
online record and reads the online peers of the product. It needs connection
options, which come from --etcd, --etcd-config or the built-in default; a proxy
that only registers may pass --etcd-config alone. The connection is resolved
in the background when the proxy is onboarded: a missing or broken coordinator
never delays going online, and the registration is retried.
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

	if cfg.Etcd.HasEndpoints {
		log.Warnf("option --etcd is set, the dashboard address will be read from etcd")
	}
	if cfg.Etcd.HasConfigFile {
		log.Warnf("option --etcd-config = %s", cfg.Etcd.ConfigFile)
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
		log.Warnf("option --product_auth=%s", credentialState(config.ProductAuth))
	}
	if cfg.SessionAuth.Provided {
		config.SessionAuth = cfg.SessionAuth.Value
		log.Warnf("option --session_auth=%s", credentialState(config.SessionAuth))
	}

	var opts []proxy.Option
	if f := onlineRegistryFactory(cfg.Etcd, config.EtcdRegisterOnline); f != nil {
		opts = append(opts, proxy.WithOnlineRegistryFactory(f))
	}
	s, err := proxy.New(config, opts...)
	if err != nil {
		log.PanicErrorf(err, "create proxy with config file failed\n%s", config)
	}
	defer s.Close()

	log.Warnf("create proxy with config\n%s", config)

	if config.EtcdRegisterOnline {
		// Nothing is connected here: the registration loop asks cfg.Etcd for a
		// client when it starts (and retries in the background), so a wrong or
		// unreachable coordinator can never keep the proxy from going online
		// (contract R2).
		log.Warnf("online registration enabled; endpoints will be resolved from --etcd/--etcd-config (or default) when the proxy is onboarded")
		// The proxy token identifies the online record. It is known right after
		// construction, so the record is published as soon as the proxy is
		// online (contract §4: publish only after the proxy is online).
		s.SetModel(s.Model())
	}

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

	switch cfg.onlineMode() {
	case onlineDashboard:
		if cfg.Etcd.HasConfigFile {
			log.Warnf("option --etcd-config only supplies etcd connection options; the proxy is brought online by the dashboard and registers itself in the background")
		}
		go AutoOnlineWithDashboard(s, dashboard)
	case onlineEtcd:
		go AutoOnlineWithCoordinator(s, cfg.Etcd)
	case onlineFillSlots:
		// An empty slots file leaves slots nil, which keeps the manual path.
		if slots != nil {
			go AutoOnlineWithFillSlots(s, slots)
		}
	case onlineManual:
		log.Warnf("no automatic online entry was given, waiting for the dashboard to add this proxy")
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

// credentialState reports whether a credential option is in effect without
// rendering its value, so process logs never carry the secret.
func credentialState(v string) string {
	if v == "" {
		return "<unset>"
	}
	return "<set>"
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

// AutoOnlineWithCoordinator brings the proxy online through the dashboard
// address published in the product's topom. The etcd client is created here so
// its lifetime covers the whole retry loop.
func AutoOnlineWithCoordinator(p *proxy.Proxy, etcdArgs coordarg.Args) {
	client, err := etcdArgs.NewClient("")
	if err != nil {
		log.PanicErrorf(err, "create etcd client failed")
	}
	defer client.Close()
	for i := 0; i < 30; i++ {
		if p.IsClosed() || p.IsOnline() {
			return
		}
		t, err := models.LoadTopom(client, p.Config().ProductName, true)
		if err != nil {
			log.WarnErrorf(err, "load & decode topom of product %s failed", p.Config().ProductName)
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

// onlineRegistryFactory builds the online registration implementation when the
// TOML option etcd_register_online is on, and returns nil otherwise. The
// factory itself performs no I/O: the coordinator connection (and its
// preflight) happens later, inside the registration loop, so the entrypoint
// never depends on etcd at startup and can retry in the background.
// Connection options come from the shared --etcd/--etcd-config selection, or
// the built-in default when neither was given.
func onlineRegistryFactory(args coordarg.Args, enabled bool) proxy.OnlineRegistryFactory {
	if !enabled {
		return nil
	}
	return func(product, token, addr string) proxy.OnlineRegistry {
		return etcdonline.New(etcdonline.Options{
			NewClient: func() (etcdonline.LeaseClient, error) {
				return args.NewClient("")
			},
			Product: product,
			Token:   token,
			Addr:    addr,
		})
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
