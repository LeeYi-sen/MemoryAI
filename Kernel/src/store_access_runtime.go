package main

import (
	"errors"
	"io"
	"sync"
)

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

func (e *Engine) storeAllIDsLocal() ([]string, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.AllIDs()
}

func (e *Engine) storeAllIDs() ([]string, error) {
	return e.storeAllIDsLocal()
}

func (e *Engine) storePhysicalFeatureIDsLocal(feature string) ([]string, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return nil, err
	}
	defer release()
	return st.PhysicalFeatureIDs(feature)
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
	if e != nil && e.manifest.Role == "core" {
		ids, _, err := e.physicalFeatureIDsFabricOwned(feature)
		return ids, err
	}
	return e.storePhysicalFeatureIDsLocal(feature)
}

func (e *Engine) storeHasPhysicalFeatureIndexLocal() (bool, error) {
	st, release, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return false, err
	}
	defer release()
	return st.HasPhysicalFeatureIndex()
}

func (e *Engine) storeHasPhysicalFeatureIndex() (bool, error) {
	target := e
	if origin, ok := speculativeOriginEngine(e); ok {
		target = origin
	}
	if target != nil && target.manifest.Role == "core" {
		// The Fabric adapter itself provides exact feature lookup for every body.
		// Indexed bodies use their persisted secondary index; a legacy body gets a
		// compatibility scan isolated to that body inside physicalFeatureIDsFabric.
		// Therefore one legacy shard must not force Activation.Build into its old
		// primary-only full-scan mode. Still probe every Store here so I/O/index
		// corruption remains a hard error rather than being silently hidden.
		for _, candidate := range target.localFabricEngines() {
			if _, err := candidate.storeHasPhysicalFeatureIndexLocal(); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	return target.storeHasPhysicalFeatureIndexLocal()
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
