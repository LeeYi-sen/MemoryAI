package main

import (
	"fmt"
	"path/filepath"
	"sync"
)

type remoteBodyTxnLockEntry struct {
	mu   sync.Mutex
	refs int
}

// remoteBodyTxnLocks serializes a complete passive-body mutation lifecycle by
// physical path: open -> mutate -> persist -> close. Registry entries are
// reference-counted across both holders and waiters, so old shard paths do not
// accumulate forever after their last transaction completes.
var (
	remoteBodyTxnLocksMu sync.Mutex
	remoteBodyTxnLocks   = map[string]*remoteBodyTxnLockEntry{}
)

// passiveBodyTxnRelease binds a held physical-body lease to the temporary
// passive Engine returned to one remote-node request. Engine.close releases it
// only after the Store descriptor has been closed.
var passiveBodyTxnRelease sync.Map // map[*Engine]func()

// activePhysicalBodies records bodies mounted into a live local Fabric. A body
// may be either mounted or opened as a temporary remote-node passive Engine,
// never both at the same time in one process.
var activePhysicalBodies sync.Map // map[canonical body path]*Engine

func canonicalPhysicalBodyPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	clean := filepath.Clean(abs)
	// Existing bodies must collapse symlink aliases to one physical key.
	if resolved, er := filepath.EvalSymlinks(clean); er == nil {
		return filepath.Clean(resolved)
	}
	// A body may not exist yet (for example a failed/opening path). Resolve the
	// parent when possible so aliases through a symlinked directory still share
	// the same transaction key.
	if resolvedDir, er := filepath.EvalSymlinks(filepath.Dir(clean)); er == nil {
		return filepath.Join(filepath.Clean(resolvedDir), filepath.Base(clean))
	}
	return clean
}

func acquireRemoteBodyTransaction(path string) func() {
	key := canonicalPhysicalBodyPath(path)
	remoteBodyTxnLocksMu.Lock()
	entry := remoteBodyTxnLocks[key]
	if entry == nil {
		entry = &remoteBodyTxnLockEntry{}
		remoteBodyTxnLocks[key] = entry
	}
	entry.refs++
	remoteBodyTxnLocksMu.Unlock()

	entry.mu.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			entry.mu.Unlock()
			remoteBodyTxnLocksMu.Lock()
			entry.refs--
			if entry.refs == 0 && remoteBodyTxnLocks[key] == entry {
				delete(remoteBodyTxnLocks, key)
			}
			remoteBodyTxnLocksMu.Unlock()
		})
	}
}

func remoteBodyTransactionRegistrySize() int {
	remoteBodyTxnLocksMu.Lock()
	defer remoteBodyTxnLocksMu.Unlock()
	return len(remoteBodyTxnLocks)
}

func registerActivePhysicalBody(e *Engine) {
	if e == nil || e.bodyPath == "" {
		return
	}
	activePhysicalBodies.Store(canonicalPhysicalBodyPath(e.bodyPath), e)
}

func unregisterActivePhysicalBody(e *Engine) {
	if e == nil || e.bodyPath == "" {
		return
	}
	key := canonicalPhysicalBodyPath(e.bodyPath)
	if current, ok := activePhysicalBodies.Load(key); ok && current == e {
		activePhysicalBodies.Delete(key)
	}
}

func activePhysicalBody(path string) (*Engine, bool) {
	key := canonicalPhysicalBodyPath(path)
	raw, ok := activePhysicalBodies.Load(key)
	if !ok {
		return nil, false
	}
	e, ok := raw.(*Engine)
	if !ok || e == nil || !engineStoreOpen(e) {
		activePhysicalBodies.Delete(key)
		return nil, false
	}
	return e, true
}

// loadPassiveStorageSerialized wraps the historical passive loader so two
// requests can never load the same old body image concurrently and later race
// to overwrite one another. The lease intentionally spans the caller's entire
// use of the returned Engine and is released by Engine.close via the generated
// Kernel overlay.
func loadPassiveStorageSerialized(path string) (*Engine, error) {
	release := acquireRemoteBodyTransaction(path)
	if live, ok := activePhysicalBody(path); ok {
		release()
		return nil, fmt.Errorf("physical Memory body already mounted by live Fabric: %s (%s)", path, live.manifest.BodyID)
	}
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
