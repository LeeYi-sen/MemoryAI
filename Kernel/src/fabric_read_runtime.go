package main

import (
	"errors"
	"io"
	"sort"
)

func resolveSpecificOwnerMemoryCopy(owner *Engine, id string) (*Memory, error) {
	if owner == nil {
		return nil, io.EOF
	}
	m, err := owner.resolveIDLocal(id)
	if err != nil {
		return nil, err
	}
	owner.dataMu.RLock()
	defer owner.dataMu.RUnlock()
	if owner.deletedIDs[id] {
		return nil, io.EOF
	}
	if cached := owner.cache[id]; cached != nil {
		return copyMemory(cached), nil
	}
	if m == nil {
		return nil, io.EOF
	}
	return copyMemory(m), nil
}

func (e *Engine) resolveLocalFabricMemoryCopy(id string) (*Engine, *Memory, error) {
	owner, _, err := e.resolveLocalFabricMemory(id)
	if err != nil {
		return nil, nil, err
	}
	out, err := resolveSpecificOwnerMemoryCopy(owner, id)
	if err != nil {
		return nil, nil, err
	}
	return owner, out, nil
}

func (e *Engine) resolveMountedShardMemoryCopy(id string) (*Engine, *Memory, error) {
	if e == nil {
		return nil, nil, io.EOF
	}
	var owner *Engine
	for _, candidate := range e.localFabricEngines() {
		if candidate == nil || candidate == e {
			continue
		}
		m, err := candidate.resolveIDLocal(id)
		if err != nil {
			if errors.Is(err, io.EOF) {
				continue
			}
			return nil, nil, err
		}
		if m == nil {
			continue
		}
		if owner != nil && owner != candidate {
			return nil, nil, duplicateFabricIdentityError(id)
		}
		owner = candidate
	}
	if owner == nil {
		return nil, nil, io.EOF
	}
	out, err := resolveSpecificOwnerMemoryCopy(owner, id)
	if err != nil {
		return nil, nil, err
	}
	return owner, out, nil
}

func memoryHasActivationFeature(m *Memory, feature string) bool {
	for _, candidate := range activationFeaturesForMemory(m, 0) {
		if candidate == feature {
			return true
		}
	}
	return false
}

func (e *Engine) physicalFeatureIDsFabricOwned(feature string) ([]string, map[string]*Engine, error) {
	seen := map[string]*Engine{}
	for _, candidate := range e.localFabricEngines() {
		shadowed := activationShadowedIDs(candidate)
		ids, err := candidate.storePhysicalFeatureIDsLocal(feature)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, nil, err
		}
		for _, id := range ids {
			if shadowed[id] {
				continue
			}
			if previous := seen[id]; previous != nil && previous != candidate {
				return nil, nil, duplicateFabricIdentityError(id)
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
				return nil, nil, duplicateFabricIdentityError(id)
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
	return out, seen, nil
}

func (e *Engine) physicalFeatureIDsFabric(feature string) ([]string, error) {
	ids, _, err := e.physicalFeatureIDsFabricOwned(feature)
	return ids, err
}

func duplicateFabricIdentityError(id string) error {
	return &fabricIdentityError{id: id}
}

type fabricIdentityError struct{ id string }

func (e *fabricIdentityError) Error() string {
	return "duplicate local Fabric Memory id " + `"` + e.id + `"`
}
