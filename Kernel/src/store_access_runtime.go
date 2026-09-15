package main

import (
	"errors"
	"io"
	"sync"
)

// IndexedStore file descriptors are shared by the primary Engine and lazy
// speculative Engines. A lock embedded only in Engine would therefore not
// protect a shared store from being closed by another Engine. Guards are keyed
// by the physical IndexedStore instance so every user of the same descriptor
// coordinates through one lifetime boundary.
var indexedStoreLifetime sync.Map // map[*IndexedStore]*sync.RWMutex

func indexedStoreGuard(st *IndexedStore) *sync.RWMutex {
	if st == nil {
		return nil
	}
	if g, ok := indexedStoreLifetime.Load(st); ok {
		return g.(*sync.RWMutex)
	}
	g := &sync.RWMutex{}
	actual, _ := indexedStoreLifetime.LoadOrStore(st, g)
	return actual.(*sync.RWMutex)
}

// acquireStoreLifetimeLease pins the Engine's current physical store. Lazy
// speculative Engines already own one long primary-store lease in their
// context, so nested reads reuse that lease instead of taking another RLock.
func (e *Engine) acquireStoreLifetimeLease() (*IndexedStore, func(), error) {
	if e == nil {
		return nil, nil, io.EOF
	}
	if st, ok := pinnedSpeculativeStore(e); ok {
		return st, func() {}, nil
	}
	e.dataMu.RLock()
	st := e.store
	if st == nil {
		e.dataMu.RUnlock()
		return nil, nil, io.EOF
	}
	g := indexedStoreGuard(st)
	g.RLock()
	e.dataMu.RUnlock()
	return st, g.RUnlock, nil
}

func (e *Engine) storeGetID(id string) (*Memory, error) {
	// A lazy speculative Engine has no mounted shard map of its own. First try
	// its pinned primary Store. On a miss, reuse an owner hint already discovered
	// by tag/activation index traversal; only an unhinted exact lookup performs a
	// mounted-shard fallback scan. The primary is never re-probed here.
	if st, ok := pinnedSpeculativeStore(e); ok {
		m, err := st.GetID(id)
		if err == nil {
			return m, nil
		}
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
		origin, exists := speculativeOriginEngine(e)
		if !exists {
			return nil, io.EOF
		}
		if hinted, exists := speculativePhysicalOwner(e, id); exists && hinted != nil && hinted != origin {
			snapshot, er := resolveSpecificOwnerMemoryCopy(hinted, id)
			if er != nil {
				return nil, er
			}
			if er := recordSpeculativeBaselineOwned(e, id, snapshot, hinted); er != nil {
				return nil, er
			}
			return snapshot, nil
		}
		owner, snapshot, err := origin.resolveMountedShardMemoryCopy(id)
		if err != nil {
			return nil, err
		}
		if err := recordSpeculativeBaselineOwned(e, id, snapshot, owner); err != nil {
			return nil, err
		}
		return snapshot, nil
	}

	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.GetID(id)
}

func (e *Engine) storeTagIDs(tag string) ([]string, error) {
	// Tag predicates inside a lazy transaction must see all mounted local shards.
	// The traversal already knows each result's owner, so preserve those hints for
	// later exact reads instead of scanning the shard set again per ID.
	if origin, ok := speculativeOriginEngine(e); ok {
		ids, owners, err := origin.listTagFabricOwned(tag)
		if err != nil {
			return nil, err
		}
		if err := recordSpeculativeOwnerHints(e, owners); err != nil {
			return nil, err
		}
		return ids, nil
	}
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.TagIDs(tag)
}

func (e *Engine) storeAllIDs() ([]string, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.AllIDs()
}

func (e *Engine) storePhysicalFeatureIDs(feature string) ([]string, error) {
	if origin, ok := speculativeOriginEngine(e); ok {
		ids, owners, err := origin.physicalFeatureIDsFabricOwned(feature)
		if err != nil {
			return nil, err
		}
		if err := recordSpeculativeOwnerHints(e, owners); err != nil {
			return nil, err
		}
		return ids, nil
	}
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.PhysicalFeatureIDs(feature)
}

func (e *Engine) storeHasPhysicalFeatureIndex() (bool, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return false, err
	}
	defer release()
	return st.HasPhysicalFeatureIndex()
}

func (e *Engine) storeMemoryCount() int {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return 0
	}
	defer release()
	return st.memoryCount
}

func (e *Engine) validateStore() error {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return err
	}
	defer release()
	return validateIndexedStore(st)
}

// closeStore is used only when an Engine is no longer available to new work.
// The write guard waits for any in-flight physical reads before closing the
// descriptor.
func (e *Engine) closeStore() {
	if e == nil {
		return
	}
	e.dataMu.Lock()
	st := e.store
	if st == nil {
		e.dataMu.Unlock()
		return
	}
	g := indexedStoreGuard(st)
	g.Lock()
	e.store = nil
	st.Close()
	g.Unlock()
	indexedStoreLifetime.Delete(st)
	e.dataMu.Unlock()
}
