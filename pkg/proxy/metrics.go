// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import (
	"context"
	"strings"
	"time"

	influxdbClient "github.com/influxdata/influxdb1-client/v2"

	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
	"github.com/CodisLabs/codis/pkg/utils/rpc"
)

// startMetricsReporter registers a metrics reporter with the proxy lifecycle.
// The reporter stops on the root context, the ticker is stopped with it, and
// the optional cleanup runs before the task leaves the WaitGroup.
func (p *Proxy) startMetricsReporter(scope string, d time.Duration, do func(context.Context) error, cleanup func() error) {
	registered := p.startBackground("metrics reporter: "+scope, func() {
		if cleanup != nil {
			defer cleanup()
		}
		var ticker = time.NewTicker(d)
		defer ticker.Stop()
		var delay = &DelayExp2{
			Min: 1, Max: 15,
			Unit: time.Second,
		}
		for {
			select {
			case <-p.ctx.Done():
				return
			case <-ticker.C:
			}
			if err := do(p.ctx); err != nil {
				if p.ctx.Err() != nil {
					return
				}
				log.WarnErrorf(err, "report metrics failed")
				delay.SleepWithCancel(func() bool {
					return p.ctx.Err() != nil
				})
			} else {
				delay.Reset()
			}
		}
	})
	if !registered && cleanup != nil {
		// startBackground rejects tasks after Close without running fn.
		_ = cleanup()
	}
}

func (p *Proxy) startMetricsJson() {
	server := p.config.MetricsReportServer
	period := p.config.MetricsReportPeriod.Duration()
	if server == "" {
		return
	}
	period = max(time.Second, period)

	p.startMetricsReporter("json", period, func(ctx context.Context) error {
		return rpc.ApiPostJsonWithContext(ctx, server, p.Overview(StatsRuntime))
	}, nil)
}

func (p *Proxy) startMetricsInfluxdb() {
	server := p.config.MetricsReportInfluxdbServer
	period := p.config.MetricsReportInfluxdbPeriod.Duration()
	if server == "" {
		return
	}
	period = max(time.Second, period)

	c, err := influxdbClient.NewHTTPClient(influxdbClient.HTTPConfig{
		Addr:     server,
		Username: p.config.MetricsReportInfluxdbUsername,
		Password: p.config.MetricsReportInfluxdbPassword,
		Timeout:  time.Second * 5,
	})
	if err != nil {
		log.WarnErrorf(err, "create influxdb client failed")
		return
	}

	database := p.config.MetricsReportInfluxdbDatabase

	p.startMetricsReporter("influxdb", period, func(ctx context.Context) error {
		b, err := influxdbClient.NewBatchPoints(influxdbClient.BatchPointsConfig{
			Database:  database,
			Precision: "ns",
		})
		if err != nil {
			return errors.Trace(err)
		}
		model := p.Model()
		stats := p.Stats(StatsRuntime)

		tags := map[string]string{
			"token":        model.Token,
			"product_name": model.ProductName,
			"admin_addr":   model.AdminAddr,
			"proxy_addr":   model.ProxyAddr,
			"hostname":     model.Hostname,
		}
		fields := map[string]any{
			"ops_total":                stats.Ops.Total,
			"ops_fails":                stats.Ops.Fails,
			"ops_redis_errors":         stats.Ops.Redis.Errors,
			"ops_qps":                  stats.Ops.QPS,
			"sessions_total":           stats.Sessions.Total,
			"sessions_alive":           stats.Sessions.Alive,
			"rusage_mem":               stats.Rusage.Mem,
			"rusage_cpu":               stats.Rusage.CPU,
			"runtime_gc_num":           stats.Runtime.GC.Num,
			"runtime_gc_total_pausems": stats.Runtime.GC.TotalPauseMs,
			"runtime_num_procs":        stats.Runtime.NumProcs,
			"runtime_num_goroutines":   stats.Runtime.NumGoroutines,
			"runtime_num_cgo_call":     stats.Runtime.NumCgoCall,
			"runtime_num_mem_offheap":  stats.Runtime.MemOffheap,
		}
		p, err := influxdbClient.NewPoint("codis_usage", tags, fields, time.Now())
		if err != nil {
			return errors.Trace(err)
		}
		b.AddPoint(p)
		return c.Write(b)
	}, func() error {
		return c.Close()
	})
}

func (p *Proxy) startMetricsStatsd() {
	server := p.config.MetricsReportStatsdServer
	period := p.config.MetricsReportStatsdPeriod.Duration()
	if server == "" {
		return
	}
	period = max(time.Second, period)

	c, err := newStatsdClient(server)
	if err != nil {
		log.WarnErrorf(err, "create statsd client failed")
		return
	}

	var (
		prefix   = p.config.MetricsReportStatsdPrefix
		replacer = strings.NewReplacer(".", "_", ":", "_")
	)

	p.startMetricsReporter("statsd", period, func(ctx context.Context) error {
		model := p.Model()
		stats := p.Stats(StatsRuntime)

		segs := []string{
			prefix, model.ProductName,
			replacer.Replace(model.AdminAddr),
			replacer.Replace(model.ProxyAddr),
		}

		fields := map[string]any{
			"ops_total":                stats.Ops.Total,
			"ops_fails":                stats.Ops.Fails,
			"ops_redis_errors":         stats.Ops.Redis.Errors,
			"ops_qps":                  stats.Ops.QPS,
			"sessions_total":           stats.Sessions.Total,
			"sessions_alive":           stats.Sessions.Alive,
			"rusage_mem":               stats.Rusage.Mem,
			"rusage_cpu":               stats.Rusage.CPU,
			"runtime_gc_num":           stats.Runtime.GC.Num,
			"runtime_gc_total_pausems": stats.Runtime.GC.TotalPauseMs,
			"runtime_num_procs":        stats.Runtime.NumProcs,
			"runtime_num_goroutines":   stats.Runtime.NumGoroutines,
			"runtime_num_cgo_call":     stats.Runtime.NumCgoCall,
			"runtime_num_mem_offheap":  stats.Runtime.MemOffheap,
		}
		for key, value := range fields {
			c.Gauge(strings.Join(append(segs, key), "."), value)
		}
		// Flush once per report cycle. Write errors retain the old reporter's
		// best-effort behavior and do not trigger a retry.
		_ = c.Flush()
		return nil
	}, c.Close)
}
