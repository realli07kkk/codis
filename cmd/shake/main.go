// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// Command codis-shake runs one Codis data migration task. It embeds the
// RedisShake v4.8.0 kernel (pkg/shake) and is started by codis-dashboard as
//
//	codis-shake --spec <job-dir>/spec.json
//
// with the job directory as working directory. Progress is published on the
// loopback status port named in the spec; SIGTERM / SIGINT stop the task
// gracefully (a second signal forces the exit), like upstream redis-shake.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/CodisLabs/codis/pkg/migration"
	"github.com/CodisLabs/codis/pkg/shake/log"
	"github.com/CodisLabs/codis/pkg/utils"
)

const shakeUsage = `
Usage:
	codis-shake --spec=FILE
	codis-shake --version

Options:
	--spec=FILE          run the migration described by the spec.json written by codis-dashboard.
`

var (
	// These variables will be set during build time
	Version   = "unknown"
	GitCommit = "unknown"
)

func main() {
	cfg, help, err := parseShakeArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		fmt.Fprint(os.Stderr, shakeUsage)
		os.Exit(2)
	}
	if help {
		fmt.Print(shakeUsage)
		return
	}
	if cfg.Version.Provided {
		fmt.Printf("codis-shake version %s (redis-shake v4.8.0, Git SHA: %s)\n", utils.Version, GitCommit)
		return
	}

	spec, err := migration.LoadSpec(cfg.Spec.Value)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(2)
	}

	// RedisShake's log package is initialised by migration.Run; until then its
	// global logger is a no-op, so argument / spec errors are reported on stderr
	// by the caller. Signal handling mirrors upstream: the first SIGINT/SIGTERM
	// cancels the context and waits for the writer to drain, a second SIGINT
	// forces the exit.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go waitShutdown(cancel)

	if err := migration.Run(ctx, spec); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func waitShutdown(cancel context.CancelFunc) {
	quitCh := make(chan os.Signal, 1)
	signal.Notify(quitCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	sigTimes := 0
	for {
		sig := <-quitCh
		if sig == syscall.SIGINT {
			sigTimes++
			log.Infof("Got signal: %s to exit. Press Ctrl+C again to force exit.", sig)
			if sigTimes >= 2 {
				os.Exit(0)
			}
			cancel()
			continue
		}
		log.Infof("Got signal: %s to exit.", sig)
		cancel()
	}
}
