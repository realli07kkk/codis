// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package sync2

import "sync"

type Future struct {
	sync.Mutex
	wait sync.WaitGroup
	vmap map[string]any
}

func (f *Future) lazyInit() {
	if f.vmap == nil {
		f.vmap = make(map[string]any)
	}
}

func (f *Future) Add() {
	f.wait.Add(1)
}

func (f *Future) Done(key string, val any) {
	f.Lock()
	defer f.Unlock()
	f.lazyInit()
	f.vmap[key] = val
	f.wait.Done()
}

func (f *Future) Wait() map[string]any {
	f.wait.Wait()
	f.Lock()
	defer f.Unlock()
	f.lazyInit()
	return f.vmap
}
