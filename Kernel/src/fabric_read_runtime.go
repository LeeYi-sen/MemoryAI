package main

import (
	"errors"
	"io"
	"sort"
)

// resolveLocalFabricMemoryCopy returns a detached snapshot of one local Memory
// plus its physical owner. The copy boundary prevents speculative cognition from
// retaining a live pointer into another shard's mutable cache.
func (e *Engine) resolveLocalFabricMemoryCopy(id string) (*Engine, *Memory, error) {
	owner, resolved, err := e.resolveLocalFabricMemory(id)
	if err != nil {
		return nil, nil, err
	}
	owner.dataMu.RLock()
	if owner.deletedIDs[id] {
		owner.dataMu.RUnlock()
		return nil, nil, io.EOF
	}
	if cached := owner.cache[id]; cached != nil {
		out := copyMemory(cached)
		owner.dataMu.RUnlock()
		return owner, out, nil
	}
	owner.dataMu.RUnlock()
	if resolved == nil {
		return nil, nil, io.EOF
	}
	return owner, copyMemory(resolved), nil
}

func memoryHasActivationFeature(m *Memory, feature string) bool {
	for _, candidate := range activationFeaturesForMemory(m, 0) {
		if candidate == feature {
			return true
		}
	}
	return false
}

// physicalFeatureIDsFabric merges the exact physical secondary indexes of all
// mounted local bodies. Dirty/new overlays shadow persisted postings in their
// owning body. No semantic score or rank is introduced here.
func (e *Engine) physicalFeatureIDsFabric(feature string) ([]string, error) {
	seen := map[string]*Engine{}
	for _, candidate := range e.localFabricEngines() {
		shadowed := activationShadowedIDs(candidate)
		ids, err := candidate.storePhysicalFeatureIDs(feature)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for _, id := range ids {
			if shadowed[id] {
				continue
			}
			if previous := seen[id]; previous != nil && previous != candidate {
				return nil, duplicateFabricIdentityError(id)
			}
			seen[id] = candidate
		}

		candidate.dataMu.RLock()
		for id := range shadowed {
			if candidate.deletedIDs[id] {
				continue
			}
			m := candidate.cache[id]
			if m == nil || !memoryHasActivationFeature(m, feature) {
				continue
			}
			if previous := seen[id]; previous != nil && previous != candidate {
				candidate.dataMu.RUnlock()
				return nil, duplicateFabricIdentityError(id)
			}
			seen[id] = candidate
		}
		candidate.dataMu.RUnlock()
	}

	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func duplicateFabricIdentityError(id string) error {
	return &fabricIdentityError{id: id}
}

type fabricIdentityError struct{ id string }

func (e *fabricIdentityError) Error() string {
	return "duplicate local Fabric Memory id " + `"` + e.id + `"`
}
