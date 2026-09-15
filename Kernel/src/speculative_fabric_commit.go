package main

import (
	"errors"
	"io"
)

type speculativeStructuralPlan struct {
	id        string
	owner     *Engine
	current   *Memory
	changed   *Memory
	deleted   bool
	wasNew    bool
	persisted bool
	execDelta uint64
}

type speculativeExecPlan struct {
	id      string
	owner   *Engine
	current *Memory
	delta   uint64
}

func physicalOwnerMemoryCopy(owner *Engine, id string) (*Memory, error) {
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

func preflightSpeculativeStructuralPlan(
	ce *Engine,
	base *memorySnapshot,
	d transactionDiff,
	id string,
	deleted bool,
) (speculativeStructuralPlan, bool) {
	owner, ok := speculativePhysicalOwner(ce, id)
	if !ok || owner == nil {
		return speculativeStructuralPlan{}, false
	}
	baseline := base.memories[id]
	if baseline == nil {
		return speculativeStructuralPlan{}, false
	}
	current, err := physicalOwnerMemoryCopy(owner, id)
	if err != nil || memoryDigestNoRuntimeExec(current) != memoryDigestNoRuntimeExec(baseline) {
		return speculativeStructuralPlan{}, false
	}
	_, persistedErr := owner.storeGetID(id)
	if persistedErr != nil && !errors.Is(persistedErr, io.EOF) {
		return speculativeStructuralPlan{}, false
	}
	owner.dataMu.RLock()
	wasNew := owner.newIDs[id]
	owner.dataMu.RUnlock()
	return speculativeStructuralPlan{
		id:        id,
		owner:     owner,
		current:   current,
		changed:   d.changed[id],
		deleted:   deleted,
		wasNew:    wasNew,
		persisted: persistedErr == nil,
		execDelta: d.execDelta[id],
	}, true
}

func applySpeculativeStructuralPlan(plan speculativeStructuralPlan) {
	owner := plan.owner
	owner.dataMu.Lock()
	defer owner.dataMu.Unlock()

	live := owner.cache[plan.id]
	if live == nil {
		live = plan.current
	}
	if live != nil {
		for _, tag := range live.Tags {
			owner.tagDeltaRemoveLocked(plan.id, tag)
		}
	}

	if plan.deleted {
		delete(owner.cache, plan.id)
		delete(owner.newIDs, plan.id)
		delete(owner.dirtyIDs, plan.id)
		owner.deletedIDs[plan.id] = true
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
	if plan.wasNew && !plan.persisted {
		owner.newIDs[plan.id] = true
	} else {
		delete(owner.newIDs, plan.id)
	}
	delete(owner.deletedIDs, plan.id)
	owner.dirtyIDs[plan.id] = true
	owner.dirty = true
}

func applySpeculativeExecPlan(plan speculativeExecPlan) bool {
	if plan.owner == nil || plan.delta == 0 {
		return plan.owner != nil
	}
	plan.owner.dataMu.Lock()
	defer plan.owner.dataMu.Unlock()
	if plan.owner.deletedIDs[plan.id] {
		return false
	}
	m := plan.owner.cache[plan.id]
	if m == nil {
		if plan.current == nil {
			return false
		}
		m = copyMemory(plan.current)
		plan.owner.cache[plan.id] = m
	}
	m.RuntimeExecCount += plan.delta
	// RuntimeExecCount is physical telemetry only. Do not mark the body dirty;
	// structural persistence deliberately ignores this counter.
	return true
}

// commitFabricSnapshotDiff applies an optimistic lazy transaction back to each
// Memory's original local physical owner. The function performs all structural
// conflict checks before any mutation, preventing a conflict in a later shard
// from leaving an earlier shard partially committed.
func (e *Engine) commitFabricSnapshotDiff(base *memorySnapshot, d transactionDiff, ce *Engine) bool {
	if e == nil || base == nil || ce == nil {
		return false
	}

	// Creation changes physical placement/cardinality. memory_new/memory_copy are
	// forbidden before side effects in speculative execution, so any created
	// record here is an invariant violation and must be replayed canonically.
	if len(d.created) != 0 {
		return false
	}

	structural := make([]speculativeStructuralPlan, 0, len(d.changed)+len(d.deleted))
	structuralIDs := map[string]bool{}
	for id := range d.changed {
		plan, ok := preflightSpeculativeStructuralPlan(ce, base, d, id, false)
		if !ok {
			return false
		}
		structural = append(structural, plan)
		structuralIDs[id] = true
	}
	for id := range d.deleted {
		plan, ok := preflightSpeculativeStructuralPlan(ce, base, d, id, true)
		if !ok {
			return false
		}
		structural = append(structural, plan)
		structuralIDs[id] = true
	}

	execPlans := make([]speculativeExecPlan, 0, len(d.execDelta))
	for id, delta := range d.execDelta {
		if delta == 0 || structuralIDs[id] {
			continue
		}
		owner, ok := speculativePhysicalOwner(ce, id)
		if !ok || owner == nil {
			return false
		}
		current, err := physicalOwnerMemoryCopy(owner, id)
		if err != nil {
			return false
		}
		execPlans = append(execPlans, speculativeExecPlan{id: id, owner: owner, current: current, delta: delta})
	}

	for _, plan := range structural {
		applySpeculativeStructuralPlan(plan)
	}
	for _, plan := range execPlans {
		if !applySpeculativeExecPlan(plan) {
			return false
		}
	}
	return true
}
