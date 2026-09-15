package main

import (
	"fmt"
	"sync"
)

// lazySnapshotContext records the exact read-set baseline of one speculative
// Engine. It also owns one long physical-store lease. Speculative reads reuse
// this lease instead of recursively acquiring another RLock, which avoids a
// writer-preference deadlock when persistence is waiting to swap the store.
type lazySnapshotContext struct {
	mu           sync.Mutex
	base         *memorySnapshot
	max          int
	store        *IndexedStore
	releaseStore func()
}

var lazySnapshotContexts sync.Map // map[*Engine]*lazySnapshotContext

func pinnedSpeculativeStore(e *Engine) (*IndexedStore, bool) {
	if e == nil || !e.speculative {
		return nil, false
	}
	v, ok := lazySnapshotContexts.Load(e)
	if !ok {
		return nil, false
	}
	ctx := v.(*lazySnapshotContext)
	if ctx.store == nil {
		return nil, false
	}
	return ctx.store, true
}

func recordSpeculativeBaseline(e *Engine, id string, m *Memory) error {
	if e == nil || !e.speculative || m == nil || id == "" {
		return nil
	}
	v, ok := lazySnapshotContexts.Load(e)
	if !ok {
		return nil
	}
	ctx := v.(*lazySnapshotContext)
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if _, exists := ctx.base.memories[id]; exists {
		return nil
	}
	if ctx.max > 0 && len(ctx.base.memories) >= ctx.max {
		return fmt.Errorf(
			"snapshot working-set limit exceeded: next=%d max=%d",
			len(ctx.base.memories)+1, ctx.max,
		)
	}
	ctx.base.memories[id] = copyMemory(m)
	return nil
}

func cloneTagDelta(src map[string]map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for tag, ids := range src {
		q := map[string]bool{}
		for id, value := range ids {
			q[id] = value
		}
		out[tag] = q
	}
	return out
}

// snapshotForSpeculationLazy creates a private overlay without enumerating the
// persisted Memory body. Only dirty/new local records must be copied up front;
// clean persisted records are page-loaded and baselined on first access. The
// same maxWorkingSet applies to both the initial overlay and subsequent reads.
//
// The physical IndexedStore is pinned here, not in the scheduler, so every
// caller of this primitive gets the same descriptor-lifetime guarantee.
func (e *Engine) snapshotForSpeculationLazy(maxWorkingSet int) (*Engine, *memorySnapshot, error) {
	if e == nil {
		return nil, nil, fmt.Errorf("lazy speculative snapshot requires engine")
	}
	store, releaseStore, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, nil, err
	}
	releaseOnError := true
	defer func() {
		if releaseOnError {
			releaseStore()
		}
	}()

	base := &memorySnapshot{
		memories:   map[string]*Memory{},
		newIDs:     map[string]bool{},
		deletedIDs: map[string]bool{},
		dirtyIDs:   map[string]bool{},
	}
	cloneCache := map[string]*Memory{}
	workingIDs := map[string]bool{}

	e.dataMu.RLock()
	for id := range e.dirtyIDs {
		workingIDs[id] = true
	}
	for id := range e.newIDs {
		workingIDs[id] = true
	}
	if maxWorkingSet > 0 && len(workingIDs) > maxWorkingSet {
		e.dataMu.RUnlock()
		return nil, nil, fmt.Errorf(
			"snapshot working-set limit exceeded: %d > %d", len(workingIDs), maxWorkingSet,
		)
	}
	for id := range workingIDs {
		if e.deletedIDs[id] {
			continue
		}
		if m := e.cache[id]; m != nil {
			cm := copyMemory(m)
			cloneCache[id] = cm
			base.memories[id] = copyMemory(m)
		}
	}
	for id, v := range e.newIDs {
		base.newIDs[id] = v
	}
	for id, v := range e.deletedIDs {
		base.deletedIDs[id] = v
	}
	for id, v := range e.dirtyIDs {
		base.dirtyIDs[id] = v
	}
	tagAdded := cloneTagDelta(e.tagAdded)
	tagRemoved := cloneTagDelta(e.tagRemoved)
	bodyPath := e.bodyPath
	manifest := e.manifest
	genesis := e.genesis
	e.dataMu.RUnlock()

	ce := &Engine{
		bodyPath: bodyPath, manifest: manifest, genesis: genesis, store: store,
		cache: cloneCache, newIDs: map[string]bool{}, dirtyIDs: map[string]bool{}, deletedIDs: map[string]bool{},
		spaces: map[string]*Engine{}, writeSpace: "", dataMu: &sync.RWMutex{}, speculative: true,
		tagAdded: tagAdded, tagRemoved: tagRemoved,
	}
	for id, v := range base.newIDs {
		ce.newIDs[id] = v
	}
	for id, v := range base.deletedIDs {
		ce.deletedIDs[id] = v
	}
	for id, v := range base.dirtyIDs {
		ce.dirtyIDs[id] = v
	}
	lazySnapshotContexts.Store(ce, &lazySnapshotContext{
		base: base, max: maxWorkingSet, store: store, releaseStore: releaseStore,
	})
	releaseOnError = false
	return ce, base, nil
}

func releaseLazySpeculation(e *Engine) {
	if e == nil {
		return
	}
	v, ok := lazySnapshotContexts.LoadAndDelete(e)
	if !ok {
		return
	}
	ctx := v.(*lazySnapshotContext)
	if ctx.releaseStore != nil {
		ctx.releaseStore()
	}
}
