package main

import (
	"io"
	"sort"
)

type speculativeStructuralPlan struct {
	id        string
	owner     *Engine
	baseline  *Memory
	changed   *Memory
	deleted   bool
	execDelta uint64
	current   *Memory
}

type speculativeExecPlan struct {
	id      string
	owner   *Engine
	delta   uint64
	current *Memory
}

// physicalOwnerMemoryLocked returns the current structural view while the
// caller holds owner.dataMu.Lock. If the record is not cached, read the pinned
// physical Store directly under its shared lifetime guard. This keeps the lock
// order dataMu -> IndexedStore guard, matching persistence/close paths.
func physicalOwnerMemoryLocked(owner *Engine, id string) (*Memory, error) {
	if owner == nil || owner.deletedIDs[id] {
		return nil, io.EOF
	}
	if cached := owner.cache[id]; cached != nil {
		return copyMemory(cached), nil
	}
	st := owner.store
	if st == nil {
		return nil, io.EOF
	}
	guard := indexedStoreGuard(st)
	guard.RLock()
	m, err := st.GetID(id)
	guard.RUnlock()
	if err != nil {
		return nil, err
	}
	return copyMemory(m), nil
}

func speculativeCommitOwners(structural []speculativeStructuralPlan, execPlans []speculativeExecPlan) []*Engine {
	seen := map[*Engine]bool{}
	owners := make([]*Engine, 0)
	add := func(owner *Engine) {
		if owner == nil || seen[owner] {
			return
		}
		seen[owner] = true
		owners = append(owners, owner)
	}
	for _, plan := range structural {
		add(plan.owner)
	}
	for _, plan := range execPlans {
		add(plan.owner)
	}
	sort.Slice(owners, func(i, j int) bool {
		if owners[i].bodyPath != owners[j].bodyPath {
			return owners[i].bodyPath < owners[j].bodyPath
		}
		return owners[i].manifest.BodyID < owners[j].manifest.BodyID
	})
	return owners
}

func lockSpeculativeCommitOwners(owners []*Engine) func() {
	for _, owner := range owners {
		owner.dataMu.Lock()
	}
	return func() {
		for i := len(owners) - 1; i >= 0; i-- {
			owners[i].dataMu.Unlock()
		}
	}
}

func applySpeculativeStructuralPlanLocked(plan speculativeStructuralPlan) {
	owner := plan.owner
	live := plan.current
	if live != nil {
		for _, tag := range live.Tags {
			owner.tagDeltaRemoveLocked(plan.id, tag)
		}
	}
	wasNew := owner.newIDs[plan.id]

	if plan.deleted {
		delete(owner.cache, plan.id)
		delete(owner.newIDs, plan.id)
		delete(owner.dirtyIDs, plan.id)
		if wasNew {
			// A record that has never reached this body's persisted Store needs no
			// tombstone; removing its pending creation is sufficient.
			delete(owner.deletedIDs, plan.id)
		} else {
			owner.deletedIDs[plan.id] = true
		}
		owner.dirty = true
		return
	}

	q := copyMemory(plan.changed)
	currentExec := uint64(0)
	if live != nil {
		currentExec = live.RuntimeExecCount
	}
	q.RuntimeExecCount = currentExec + plan.execDelta
	owner.cache[plan.id] = q
	for _, tag := range q.Tags {
		owner.tagDeltaAddLocked(plan.id, tag)
	}
	if wasNew {
		owner.newIDs[plan.id] = true
	} else {
		delete(owner.newIDs, plan.id)
	}
	delete(owner.deletedIDs, plan.id)
	owner.dirtyIDs[plan.id] = true
	owner.dirty = true
}

func applySpeculativeExecPlanLocked(plan speculativeExecPlan) {
	owner := plan.owner
	m := owner.cache[plan.id]
	if m == nil {
		m = copyMemory(plan.current)
		owner.cache[plan.id] = m
	}
	m.RuntimeExecCount += plan.delta
	// RuntimeExecCount is physical telemetry only. Do not mark the body dirty;
	// structural persistence deliberately ignores this counter.
}

// commitFabricSnapshotDiff atomically commits one optimistic transaction across
// all touched local physical owners. The Store page-in lease has already been
// released by txnScheduler before this function is entered.
//
// All owners are locked in deterministic physical-path order. Every baseline is
// then revalidated while those locks are held. If any owner changed, no mutation
// has occurred and the caller replays canonically. Only after every validation
// passes are all structural/telemetry deltas applied, eliminating the previous
// preflight-to-apply TOCTOU and partial-cross-shard commit windows.
func (e *Engine) commitFabricSnapshotDiff(base *memorySnapshot, d transactionDiff, ce *Engine) bool {
	if e == nil || base == nil || ce == nil {
		return false
	}
	if len(d.created) != 0 {
		// Physical creation/cardinality changes are canonical-only.
		return false
	}

	structural := make([]speculativeStructuralPlan, 0, len(d.changed)+len(d.deleted))
	structuralIDs := map[string]bool{}
	for id, changed := range d.changed {
		owner, ok := speculativePhysicalOwner(ce, id)
		baseline := base.memories[id]
		if !ok || owner == nil || baseline == nil {
			return false
		}
		structural = append(structural, speculativeStructuralPlan{
			id: id, owner: owner, baseline: baseline, changed: changed,
			execDelta: d.execDelta[id],
		})
		structuralIDs[id] = true
	}
	for id := range d.deleted {
		owner, ok := speculativePhysicalOwner(ce, id)
		baseline := base.memories[id]
		if !ok || owner == nil || baseline == nil {
			return false
		}
		structural = append(structural, speculativeStructuralPlan{
			id: id, owner: owner, baseline: baseline, deleted: true,
			execDelta: d.execDelta[id],
		})
		structuralIDs[id] = true
	}

	execPlans := make([]speculativeExecPlan, 0, len(d.execDelta))
	for id, delta := range d.execDelta {
		if delta == 0 || structuralIDs[id] {
			continue
		}
		owner, ok := speculativePhysicalOwner(ce, id)
		if !ok || owner == nil || base.memories[id] == nil {
			return false
		}
		execPlans = append(execPlans, speculativeExecPlan{id: id, owner: owner, delta: delta})
	}

	owners := speculativeCommitOwners(structural, execPlans)
	unlock := lockSpeculativeCommitOwners(owners)
	defer unlock()

	// Phase 1: lock-held validation only. No writes are allowed before this phase
	// has succeeded for every touched physical owner.
	for i := range structural {
		current, err := physicalOwnerMemoryLocked(structural[i].owner, structural[i].id)
		if err != nil || memoryDigestNoRuntimeExec(current) != memoryDigestNoRuntimeExec(structural[i].baseline) {
			return false
		}
		structural[i].current = current
	}
	for i := range execPlans {
		current, err := physicalOwnerMemoryLocked(execPlans[i].owner, execPlans[i].id)
		if err != nil {
			return false
		}
		execPlans[i].current = current
	}

	// Phase 2: all baselines are still protected by the same owner locks.
	for _, plan := range structural {
		applySpeculativeStructuralPlanLocked(plan)
	}
	for _, plan := range execPlans {
		applySpeculativeExecPlanLocked(plan)
	}
	return true
}
