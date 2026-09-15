package main

import (
	"fmt"
	"sync"
)

// lazySnapshotContext records the exact read-set baseline of one speculative
// Engine. It also owns one long primary-store lease. IDs first read from passive
// local shards record their real physical owner so commit never collapses shard
// state back into the primary body.
type lazySnapshotContext struct {
	mu           sync.Mutex
	base         *memorySnapshot
	max          int
	store        *IndexedStore
	releaseStore func()
	origin       *Engine
	owners       map[string]*Engine
}

var lazySnapshotContexts sync.Map // map[*Engine]*lazySnapshotContext

func speculativeLazyContext(e *Engine) (*lazySnapshotContext, bool) {
	if e == nil || !e.speculative {
		return nil, false
	}
	v, ok := lazySnapshotContexts.Load(e)
	if !ok {
		return nil, false
	}
	return v.(*lazySnapshotContext), true
}

func pinnedSpeculativeStore(e *Engine) (*IndexedStore, bool) {
	ctx, ok := speculativeLazyContext(e)
	if !ok || ctx.store == nil {
		return nil, false
	}
	return ctx.store, true
}

func speculativeOriginEngine(e *Engine) (*Engine, bool) {
	ctx, ok := speculativeLazyContext(e)
	if !ok || ctx.origin == nil {
		return nil, false
	}
	return ctx.origin, true
}

func speculativePhysicalOwner(e *Engine, id string) (*Engine, bool) {
	ctx, ok := speculativeLazyContext(e)
	if !ok {
		return nil, false
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	owner := ctx.owners[id]
	return owner, owner != nil
}

func recordSpeculativeBaselineOwned(e *Engine, id string, m *Memory, owner *Engine) error {
	if e == nil || !e.speculative || m == nil || id == "" {
		return nil
	}
	ctx, ok := speculativeLazyContext(e)
	if !ok {
		return nil
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if _, exists := ctx.base.memories[id]; exists {
		if ctx.owners[id] == nil && owner != nil {
			ctx.owners[id] = owner
		}
		return nil
	}
	if ctx.max > 0 && len(ctx.base.memories) >= ctx.max {
		return fmt.Errorf(
			"snapshot working-set limit exceeded: next=%d max=%d",
			len(ctx.base.memories)+1, ctx.max,
		)
	}
	ctx.base.memories[id] = copyMemory(m)
	if owner != nil {
		ctx.owners[id] = owner
	}
	return nil
}

// Generated kernel.go invokes this hook for a clean record first loaded through
// the primary store. Shard-aware store fallback records an explicit owner before
// this hook is reached, so the first owner assignment remains authoritative.
func recordSpeculativeBaseline(e *Engine, id string, m *Memory) error {
	origin, _ := speculativeOriginEngine(e)
	return recordSpeculativeBaselineOwned(e, id, m, origin)
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
// persisted Memory Fabric. Only dirty/new records already in the primary body
// are copied up front; other primary or shard records are copied and baselined
// on first access. The same maxWorkingSet bounds both phases.
//
// Lock order is deliberately dataMu.RLock -> shared primary IndexedStore RLock.
// Shard stores are leased only for the duration of an individual first read;
// optimistic digest validation protects them through commit.
func (e *Engine) snapshotForSpeculationLazy(maxWorkingSet int) (*Engine, *memorySnapshot, error) {
	if e == nil {
		return nil, nil, fmt.Errorf("lazy speculative snapshot requires engine")
	}

	base := &memorySnapshot{
		memories:   map[string]*Memory{},
		newIDs:     map[string]bool{},
		deletedIDs: map[string]bool{},
		dirtyIDs:   map[string]bool{},
	}
	cloneCache := map[string]*Memory{}
	workingIDs := map[string]bool{}
	owners := map[string]*Engine{}

	e.dataMu.RLock()
	store := e.store
	if store == nil {
		e.dataMu.RUnlock()
		return nil, nil, fmt.Errorf("lazy speculative snapshot store unavailable")
	}
	storeGuard := indexedStoreGuard(store)
	storeGuard.RLock()
	releaseStore := storeGuard.RUnlock

	for id := range e.dirtyIDs {
		workingIDs[id] = true
	}
	for id := range e.newIDs {
		workingIDs[id] = true
	}
	if maxWorkingSet > 0 && len(workingIDs) > maxWorkingSet {
		e.dataMu.RUnlock()
		releaseStore()
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
			owners[id] = e
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
		origin: e, owners: owners,
	})
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
