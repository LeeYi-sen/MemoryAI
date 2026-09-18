package main

import (
	"archive/zip"
	"fmt"
	"io"
	"path/filepath"
	"sync"
)

type persistenceSnapshot struct {
	present map[string]string // Memory id -> structural digest written to disk
}

var enginePersistenceLocks sync.Map // map[*Engine]*sync.Mutex

func lockEnginePersistence(e *Engine) func() {
	actual, _ := enginePersistenceLocks.LoadOrStore(e, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// structuralMemoryDigest is the single production structural-identity
// definition. Physical runtime/security metadata must never make a Memory look
// semantically different merely because it executed or was re-signed.
func structuralMemoryDigest(m *Memory) string {
	return memoryJSONDigest(m)
}

// snapshotMemoriesForPersistence deep-copies every record that will be written.
// Each copy is taken under dataMu so buildIndexedSections never races with a
// concurrent Memory mutation. Records created/deleted after the ID page is
// captured remain dirty and are handled by the next persistence pass.
func snapshotMemoriesForPersistence(e *Engine) ([]*Memory, *persistenceSnapshot, error) {
	if e == nil {
		return nil, nil, fmt.Errorf("persistence snapshot requires engine")
	}
	ids, err := e.localIDs()
	if err != nil {
		return nil, nil, err
	}
	out := make([]*Memory, 0, len(ids))
	snap := &persistenceSnapshot{present: make(map[string]string, len(ids))}
	for _, id := range ids {
		m, er := e.resolveIDLocal(id)
		if er == io.EOF {
			continue
		}
		if er != nil {
			return nil, nil, er
		}
		e.dataMu.RLock()
		if e.deletedIDs[id] {
			e.dataMu.RUnlock()
			continue
		}
		live := e.cache[id]
		if live == nil {
			live = m
		}
		cp := copyMemory(live)
		e.dataMu.RUnlock()
		out = append(out, cp)
		snap.present[id] = structuralMemoryDigest(cp)
	}
	return out, snap, nil
}

func tagSet(tags []string) map[string]bool {
	out := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if tag != "" {
			out[tag] = true
		}
	}
	return out
}

// rebuildPendingTagDeltasLocked rebuilds only the deltas that still differ from
// the newly persisted store. dataMu and the old-store write guard are held by
// the caller; st is the new private store and is not yet visible to readers.
func rebuildPendingTagDeltasLocked(e *Engine, st *IndexedStore) {
	e.tagAdded = map[string]map[string]bool{}
	e.tagRemoved = map[string]map[string]bool{}
	pending := map[string]bool{}
	for id := range e.dirtyIDs {
		pending[id] = true
	}
	for id := range e.newIDs {
		pending[id] = true
	}
	for id, yes := range e.deletedIDs {
		if yes {
			pending[id] = true
		}
	}
	for id := range pending {
		persisted, _ := st.GetID(id)
		if e.deletedIDs[id] {
			if persisted != nil {
				for _, tag := range persisted.Tags {
					e.tagDeltaRemoveLocked(id, tag)
				}
			}
			continue
		}
		current := e.cache[id]
		if current == nil {
			continue
		}
		oldTags := map[string]bool{}
		if persisted != nil {
			oldTags = tagSet(persisted.Tags)
		}
		newTags := tagSet(current.Tags)
		for tag := range oldTags {
			if !newTags[tag] {
				e.tagDeltaRemoveLocked(id, tag)
			}
		}
		for tag := range newTags {
			if !oldTags[tag] {
				e.tagDeltaAddLocked(id, tag)
			}
		}
	}
}

// finalizePersistedBody rebinds an Engine to the newly renamed physical body.
// writeDetZip replaces the pathname atomically, so the prior IndexedStore file
// descriptor still points at the old inode until we explicitly reopen it.
//
// A speculative transaction may still be reading that old descriptor. The
// shared IndexedStore lifetime guard waits for those transactions before close.
// Dirty state is reconciled against the exact structural snapshot that was
// written so mutations that occurred during persistence are never lost.
func finalizePersistedBody(e *Engine, out string, snap *persistenceSnapshot) error {
	if e == nil {
		return fmt.Errorf("persist finalize requires engine")
	}
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	bodyAbs, err := filepath.Abs(e.bodyPath)
	if err != nil {
		return err
	}
	// saveBody may also be used as an export/copy operation. Only a write back to
	// the Engine's active body mutates live store state.
	if filepath.Clean(outAbs) != filepath.Clean(bodyAbs) {
		return nil
	}

	zr, err := zip.OpenReader(outAbs)
	if err != nil {
		return err
	}
	defer zr.Close()

	var mf Manifest
	if err = readJSONZip(&zr.Reader, "manifest.json", &mf); err != nil {
		return err
	}
	var g Genesis
	if err = readJSONZip(&zr.Reader, mf.GenesisPath, &g); err != nil {
		return err
	}
	st, err := openIndexedStore(outAbs, zr, mf.Store)
	if err != nil {
		return err
	}
	if err = validateIndexedStore(st); err != nil {
		st.Close()
		return err
	}
	if snap == nil {
		st.Close()
		return fmt.Errorf("persist finalize requires written snapshot")
	}

	// Lock ordering is dataMu -> shared store guard. acquireStoreLifetimeLease
	// uses the same ordering, preventing pointer races and lock inversion.
	e.dataMu.Lock()
	old := e.store
	oldGuard := indexedStoreGuard(old)
	if oldGuard != nil {
		oldGuard.Lock()
	}

	// Work from the mutation sets as they exist now, not as they existed when
	// persistence started. Concurrent mutations are therefore retained.
	newDirty := map[string]bool{}
	newNew := map[string]bool{}
	newDeleted := map[string]bool{}
	candidates := map[string]bool{}
	for id := range e.dirtyIDs {
		candidates[id] = true
	}
	for id := range e.newIDs {
		candidates[id] = true
	}
	for id, yes := range e.deletedIDs {
		if yes {
			candidates[id] = true
		}
	}

	for id := range candidates {
		if e.deletedIDs[id] {
			// A deletion is durable only when the just-written snapshot omitted it.
			if _, stillOnDisk := snap.present[id]; stillOnDisk {
				newDeleted[id] = true
			}
			continue
		}
		current := e.cache[id]
		writtenDigest, written := snap.present[id]
		if current == nil || !written || structuralMemoryDigest(current) != writtenDigest {
			newDirty[id] = true
			if _, er := st.GetID(id); er != nil {
				newNew[id] = true
			}
		}
	}

	e.store = st
	e.dirtyIDs = newDirty
	e.newIDs = newNew
	e.deletedIDs = newDeleted
	rebuildPendingTagDeltasLocked(e, st)
	e.dirty = len(newDirty) > 0 || len(newNew) > 0 || len(newDeleted) > 0

	if old != nil {
		old.Close()
	}
	if oldGuard != nil {
		oldGuard.Unlock()
		indexedStoreLifetime.Delete(old)
	}
	e.dataMu.Unlock()

	// The persisted physical index is now authoritative. Rebuild only the small
	// mutable activation overlay; new-format stores take the non-scanning path.
	if e.manifest.Role == "core" {
		if err := globalActivationRuntime.Build(e); err != nil {
			return err
		}
	}
	return nil
}

func persistEngineIfDirty(e *Engine) error {
	if e == nil || !e.isDirty() {
		return nil
	}
	return e.saveBody(e.bodyPath)
}
