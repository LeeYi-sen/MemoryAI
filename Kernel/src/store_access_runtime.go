package main

import (
	"errors"
	"io"
	"sort"
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

func sortedOwnedIDs(seen map[string]*Engine) []string {
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func mergeOwnedIDs(dst map[string]*Engine, src map[string]*Engine) error {
	for id, owner := range src {
		if err := addFabricOwner(dst, id, owner); err != nil {
			return err
		}
	}
	return nil
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
			if er == nil {
				if er := recordSpeculativeBaselineOwned(e, id, snapshot, hinted); er != nil {
					return nil, er
				}
				return snapshot, nil
			}
			if !errors.Is(er, io.EOF) {
				return nil, er
			}
			// The index-derived owner was only an unread routing hint. A local
			// topology mutation may have moved or deleted the Memory since the
			// candidate set was produced, so invalidate the hint and rediscover
			// across the currently mounted shards. Baselined owners are never
			// cleared by clearSpeculativeOwnerHint.
			clearSpeculativeOwnerHint(e, id, hinted)
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
	if st, ok := pinnedSpeculativeStore(e); ok {
		origin, exists := speculativeOriginEngine(e)
		if !exists {
			return nil, io.EOF
		}
		seen := map[string]*Engine{}
		primaryIDs, err := st.TagIDs(tag)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for _, id := range primaryIDs {
			if err := addFabricOwner(seen, id, origin); err != nil {
				return nil, err
			}
		}
		_, shardOwners, err := origin.listTagMountedShardsOwned(tag)
		if err != nil {
			return nil, err
		}
		if err := mergeOwnedIDs(seen, shardOwners); err != nil {
			return nil, err
		}
		if err := recordSpeculativeOwnerHints(e, seen); err != nil {
			return nil, err
		}
		return sortedOwnedIDs(seen), nil
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
	if st, ok := pinnedSpeculativeStore(e); ok {
		origin, exists := speculativeOriginEngine(e)
		if !exists {
			return nil, io.EOF
		}
		seen := map[string]*Engine{}
		shadowed := activationShadowedIDs(e)

		indexed, err := st.HasPhysicalFeatureIndex()
		if err != nil {
			return nil, err
		}
		if indexed {
			ids, err := st.PhysicalFeatureIDs(feature)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			for _, id := range ids {
				if shadowed[id] {
					continue
				}
				if err := addFabricOwner(seen, id, origin); err != nil {
					return nil, err
				}
			}
		} else {
			ids, err := st.AllIDs()
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			for _, id := range ids {
				if shadowed[id] {
					continue
				}
				m, er := st.GetID(id)
				if er != nil {
					if errors.Is(er, io.EOF) {
						continue
					}
					return nil, er
				}
				if memoryHasActivationFeature(m, feature) {
					if err := addFabricOwner(seen, id, origin); err != nil {
						return nil, err
					}
				}
			}
		}

		_, shardOwners, err := origin.physicalFeatureIDsMountedShardsOwned(feature)
		if err != nil {
			return nil, err
		}
		if err := mergeOwnedIDs(seen, shardOwners); err != nil {
			return nil, err
		}

		// Private speculative changes shadow both primary and shard base postings.
		for id := range shadowed {
			delete(seen, id)
		}
		e.dataMu.RLock()
		for id := range shadowed {
			if e.deletedIDs[id] {
				continue
			}
			m := e.cache[id]
			if m == nil || !memoryHasActivationFeature(m, feature) {
				continue
			}
			owner := origin
			if hinted, exists := speculativePhysicalOwner(e, id); exists && hinted != nil {
				owner = hinted
			}
			if err := addFabricOwner(seen, id, owner); err != nil {
				e.dataMu.RUnlock()
				return nil, err
			}
		}
		e.dataMu.RUnlock()

		if err := recordSpeculativeOwnerHints(e, seen); err != nil {
			return nil, err
		}
		return sortedOwnedIDs(seen), nil
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
	if st, ok := pinnedSpeculativeStore(e); ok {
		if _, err := st.HasPhysicalFeatureIndex(); err != nil {
			return false, err
		}
		origin, exists := speculativeOriginEngine(e)
		if !exists {
			return false, io.EOF
		}
		for _, candidate := range origin.mountedFabricEngines() {
			if _, err := candidate.storeHasPhysicalFeatureIndexLocal(); err != nil {
				return false, err
			}
		}
		return true, nil
	}

	if e != nil && e.manifest.Role == "core" {
		for _, candidate := range e.localFabricEngines() {
			if _, err := candidate.storeHasPhysicalFeatureIndexLocal(); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	return e.storeHasPhysicalFeatureIndexLocal()
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
