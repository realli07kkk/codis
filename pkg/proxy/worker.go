// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package proxy

import "sync"

// sharedWorker gives a package-level worker an explicit, reference-counted
// lifecycle. The worker is started by the first owner and stopped after the
// last owner releases it, so two owners (two proxies in one process, or a proxy
// and a test) can never run duplicate copies of the same background loop.
//
// The worker's start and stop functions are called while the worker mutex is
// held, and stop must wait until the worker loops have exited.
type sharedWorker[T any] struct {
	mu    sync.Mutex
	refs  int
	value T
	start func(T)
	stop  func(T)
}

func newSharedWorker[T any](value T, start, stop func(T)) *sharedWorker[T] {
	return &sharedWorker[T]{value: value, start: start, stop: stop}
}

// acquire registers one owner. The returned release function is idempotent and
// stops the worker once the last owner has released it.
func (w *sharedWorker[T]) acquire() func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.refs == 0 {
		w.start(w.value)
	}
	w.refs++
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.refs--
			if w.refs == 0 {
				w.stop(w.value)
			}
		})
	}
}

// running reports whether the worker currently has an owner.
func (w *sharedWorker[T]) running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.refs > 0
}

// refCount returns the number of live owners.
func (w *sharedWorker[T]) refCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.refs
}
