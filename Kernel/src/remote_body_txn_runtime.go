package main

import (
	"fmt"
	"path/filepath"
	"sync"
)

// remoteBodyTxnLocks serializes a complete passive-body mutation lifecycle by
// physical path: open -> mutate -> persist -> close. It is deliberately a
// physical storage primitive only; no Memory semantics or routing policy live
// here. Different body paths remain fully parallel.
var remoteBodyTxnLocks sync.Map // map[string]*sync.Mutex

// passiveBodyTxnRelease binds a held physical-body lease to the temporary
// passive Engine returned to one remote-node request. Engine.close releases it
// only after the Store descriptor has been closed.
var passiveBodyTxnRelease sync.Map // map[*Engine]func()

func canonicalPhysicalBodyPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func acquireRemoteBodyTransaction(path string) func() {
	key := canonicalPhysicalBodyPath(path)
	actual, _ := remoteBodyTxnLocks.LoadOrStore(key, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// loadPassiveStorageSerialized wraps the historical passive loader so two
// requests can never load the same old body image concurrently and later race
// to overwrite one another. The lease intentionally spans the caller's entire
// use of the returned Engine and is released by Engine.close via the generated
// Kernel overlay.
func loadPassiveStorageSerialized(path string) (*Engine, error) {
	release := acquireRemoteBodyTransaction(path)
	sp, err := loadPassiveStorage(path)
	if err != nil {
		release()
		return nil, err
	}
	if sp == nil {
		release()
		return nil, fmt.Errorf("passive storage loader returned nil engine: %s", path)
	}
	if _, loaded := passiveBodyTxnRelease.LoadOrStore(sp, release); loaded {
		release()
		sp.close()
		return nil, fmt.Errorf("passive storage transaction already registered: %s", path)
	}
	return sp, nil
}

func releasePassiveBodyTransaction(e *Engine) {
	if e == nil {
		return
	}
	if raw, ok := passiveBodyTxnRelease.LoadAndDelete(e); ok {
		raw.(func())()
	}
}
