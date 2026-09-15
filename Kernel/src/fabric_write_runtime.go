package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

func (e *Engine) localFabricEngines() []*Engine {
	if e == nil {
		return nil
	}
	type mounted struct {
		path string
		eng  *Engine
	}
	e.spaceMu.RLock()
	items := make([]mounted, 0, len(e.spaces))
	for path, sp := range e.spaces {
		if sp != nil && sp != e {
			items = append(items, mounted{path: path, eng: sp})
		}
	}
	e.spaceMu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	out := make([]*Engine, 0, len(items)+1)
	out = append(out, e)
	for _, item := range items {
		out = append(out, item.eng)
	}
	return out
}

func (e *Engine) mountedFabricEngines() []*Engine {
	all := e.localFabricEngines()
	out := make([]*Engine, 0, len(all))
	for _, candidate := range all {
		if candidate != nil && candidate != e {
			out = append(out, candidate)
		}
	}
	return out
}

func (e *Engine) resolveLocalFabricMemory(id string) (*Engine, *Memory, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil, io.EOF
	}
	var owner *Engine
	var found *Memory
	for _, candidate := range e.localFabricEngines() {
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
			return nil, nil, fmt.Errorf("duplicate local Fabric Memory id %q", id)
		}
		owner = candidate
		found = m
	}
	if owner == nil {
		return nil, nil, io.EOF
	}
	return owner, found, nil
}

func listTagAcrossEngines(engines []*Engine, tag string) ([]string, map[string]*Engine, error) {
	seen := map[string]*Engine{}
	for _, candidate := range engines {
		if candidate == nil {
			continue
		}
		ids, err := candidate.listTagLocal(tag)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range ids {
			if previous := seen[id]; previous != nil && previous != candidate {
				return nil, nil, fmt.Errorf("duplicate local Fabric Memory id %q while listing tag %q", id, tag)
			}
			seen[id] = candidate
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, seen, nil
}

func (e *Engine) listTagFabricOwned(tag string) ([]string, map[string]*Engine, error) {
	return listTagAcrossEngines(e.localFabricEngines(), tag)
}

func (e *Engine) listTagMountedShardsOwned(tag string) ([]string, map[string]*Engine, error) {
	return listTagAcrossEngines(e.mountedFabricEngines(), tag)
}

func (e *Engine) listTagFabric(tag string) ([]string, error) {
	ids, _, err := e.listTagFabricOwned(tag)
	return ids, err
}

func upsertExplicitMemoryOnOwner(owner *Engine, q *Memory) error {
	if owner == nil || q == nil || strings.TrimSpace(q.ID) == "" {
		return fmt.Errorf("explicit Memory update requires physical owner and id")
	}
	persistedOld, persistedErr := owner.storeGetID(q.ID)
	if persistedErr != nil && !errors.Is(persistedErr, io.EOF) {
		return persistedErr
	}
	owner.dataMu.Lock()
	if old := owner.cache[q.ID]; old != nil {
		for _, tag := range old.Tags {
			owner.tagDeltaRemoveLocked(q.ID, tag)
		}
	} else if persistedErr == nil && persistedOld != nil {
		for _, tag := range persistedOld.Tags {
			owner.tagDeltaRemoveLocked(q.ID, tag)
		}
	}
	owner.cache[q.ID] = q
	for _, tag := range q.Tags {
		owner.tagDeltaAddLocked(q.ID, tag)
	}
	if errors.Is(persistedErr, io.EOF) {
		owner.newIDs[q.ID] = true
	} else {
		delete(owner.newIDs, q.ID)
	}
	delete(owner.deletedIDs, q.ID)
	owner.dirtyIDs[q.ID] = true
	owner.dirty = true
	owner.dataMu.Unlock()
	return nil
}

func (e *Engine) upsertExplicitMemoryBounded(m *Memory) error {
	if e == nil || m == nil || strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("explicit Memory upsert requires engine and id")
	}
	q := copyMemory(m)
	if q.State == nil {
		q.State = map[string]any{}
	}
	automaticShardMu.Lock()
	defer automaticShardMu.Unlock()
	owner, _, err := e.resolveLocalFabricMemory(q.ID)
	if err == nil {
		if owner != e && owner.manifest.Role != "storage" {
			return fmt.Errorf("Memory %q is owned by non-writable local body role %q", q.ID, owner.manifest.Role)
		}
		return upsertExplicitMemoryOnOwner(owner, q)
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	switch e.manifest.Role {
	case "core":
		return e.placeRuntimeMemoryLocked(q)
	case "storage":
		if !shardHasCapacity(e, 1) {
			return fmt.Errorf("storage body full: %d Memory limit reached", memoryShardMax())
		}
		e.addRuntimeMemory(q)
		return nil
	default:
		return fmt.Errorf("explicit Memory creation requires core or storage body, got role %q", e.manifest.Role)
	}
}

func (e *Engine) deleteExplicitMemoryBounded(id string) error {
	owner, m, err := e.resolveLocalFabricMemory(id)
	if err != nil {
		return err
	}
	if owner != e && owner.manifest.Role != "storage" {
		return fmt.Errorf("Memory %q is owned by non-writable local body role %q", id, owner.manifest.Role)
	}
	owner.dataMu.Lock()
	for _, tag := range m.Tags {
		owner.tagDeltaRemoveLocked(id, tag)
	}
	owner.deletedIDs[id] = true
	delete(owner.cache, id)
	delete(owner.newIDs, id)
	delete(owner.dirtyIDs, id)
	owner.dirty = true
	owner.dataMu.Unlock()
	return nil
}
