// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

//go:build !linux

package utils

import (
	"syscall"
	"time"

	"github.com/CodisLabs/codis/pkg/utils/errors"
)

type Usage struct {
	Utime time.Duration `json:"utime"`
	Stime time.Duration `json:"stime"`

	MaxRss int64 `json:"max_rss"`
	Ixrss  int64 `json:"ix_rss"`
	Idrss  int64 `json:"id_rss"`
	Isrss  int64 `json:"is_rss"`
}

func (u *Usage) MemTotal() int64 {
	return u.Ixrss + u.Idrss + u.Isrss
}

func (u *Usage) CPUTotal() time.Duration {
	return u.Utime + u.Stime
}

func GetUsage() (*Usage, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return nil, errors.Trace(err)
	}
	u := &Usage{}
	u.Utime = time.Duration(usage.Utime.Nano())
	u.Stime = time.Duration(usage.Stime.Nano())

	u.MaxRss = usage.Maxrss
	u.Ixrss = rssUnit * usage.Ixrss
	u.Idrss = rssUnit * usage.Idrss
	u.Isrss = rssUnit * usage.Isrss
	return u, nil
}
