package main

import "sort"

// ExactFeatureIDs returns the complete exact physical posting for one opaque
// feature. Unlike Activate it applies no transport page cap and no ranking, so
// runtime plumbing such as event dispatch cannot silently drop handlers.
func (r *SparseActivationRuntime) ExactFeatureIDs(e *Engine, feature string) ([]string, error) {
	hit := map[string]struct{}{}
	r.mu.RLock()
	persistent := r.persistentBase
	r.mu.RUnlock()

	if persistent {
		// storePhysicalFeatureIDs is the authoritative Fabric view: it already
		// merges persisted postings with per-body dirty/new/deleted overlays.
		// Filtering its result again through root-only shadow state would remove
		// valid live records from the primary body.
		ids, err := e.storePhysicalFeatureIDs(feature)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			hit[id] = struct{}{}
		}
	}

	// Historical non-persistent mode alone uses the in-memory posting map.
	if !persistent {
		r.mu.RLock()
		for id := range r.postings[feature] {
			hit[id] = struct{}{}
		}
		r.mu.RUnlock()
	}

	ids := make([]string, 0, len(hit))
	for id := range hit {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
