package main

import (
	"fmt"
	"sync"
)

// lazySnapshotContext records the exact read-set baseline of one speculative
// Engine. The primary Store is pinned only while speculative code can still page
// in records. After execution/diff formation the lease is released before the
// commit phase, while this context remains alive to carry frozen owner/read-set
// metadata into optimistic validation.
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
	if !ok {
		return nil, false
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.store == nil {
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

func recordSpeculativeOwnerHints(e *Engine, hints map[string]*Engine) error {
	ctx, ok := speculativeLazyContext(e)
	if !ok || len(hints) == 0 {
		return nil
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	for id, owner := range hints {
		if id == "" || owner == nil {
			continue
		}
		if existing := ctx.owners[id]; existing != nil && existing != owner {
			if _, baselined := ctx.base.memories[id]; baselined {
				return duplicateFabricIdentityError(id)
			}
			ctx.owners[id] = owner
			continue
		}
		ctx.owners[id] = owner
	}
	return nil
}

func clearSpeculativeOwnerHint(e *Engine, id string, expected *Engine) {
	ctx, ok := speculativeLazyContext(e)
	if !ok || id == "" {
		return
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if _, baselined := ctx.base.memories[id]; baselined {
		return
	}
	if expected == nil || ctx.owners[id] == expected {
		delete(ctx.owners, id)
	}
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
		if existing := ctx.owners[id]; existing != nil && owner != nil && existing != owner {
			return duplicateFabricIdentityError(id)
		}
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
	if existing := ctx.owners[id]; existing != nil && owner != nil && existing != owner {
		return duplicateFabricIdentityError(id)
	}
	ctx.base.memories[id] = copyMemory(m)
	if owner != nil {
		ctx.owners[id] = owner
	}
	return nil
}

// Generated kernel.go invokes this hook after storeGetID. A shard-aware
// storeGetID may already have baselined the record with its real physical owner,
// so preserve that owner instead of blindly rewriting it to the primary origin.
func recordSpeculativeBaseline(e *Engine, id string, m *Memory) error {
	if owner, ok := speculativePhysicalOwner(e, id); ok && owner != nil {
		return recordSpeculativeBaselineOwned(e, id, m, owner)
	}
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

func releaseSpeculativeStoreLease(e *Engine) {
	ctx, ok := speculativeLazyContext(e)
	if !ok {
		return
	}
	ctx.mu.Lock()
	release := ctx.releaseStore
	ctx.releaseStore = nil
	ctx.store = nil
	ctx.mu.Unlock()
	if release != nil {
		release()
	}
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
	ctx.mu.Lock()
	release := ctx.releaseStore
	ctx.releaseStore = nil
	ctx.store = nil
	ctx.mu.Unlock()
	if release != nil {
		release()
	}
}
