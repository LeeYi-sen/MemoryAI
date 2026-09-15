package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// txnScheduler coordinates physical snapshot/diff execution. It does not decide
// which cognition is valuable; callers choose the executable Memory. Kernel
// only isolates mutation, detects conflicts and protects external side effects.
type txnScheduler struct {
	commitMu      sync.Mutex
	runs          uint64
	canonicalRuns uint64
}

var globalTxnScheduler = &txnScheduler{}

func (s *txnScheduler) canonical(e *Engine, id string, f *Frame) error {
	atomic.AddUint64(&s.canonicalRuns, 1)
	// commitMu is also the canonical mutation lane. This guarantees that a
	// conflict replay cannot interleave with another snapshot commit.
	return e.run(id, f)
}

func (s *txnScheduler) run(e *Engine, id string, f *Frame) error {
	if e == nil {
		return errors.New("transaction scheduler requires engine")
	}
	if f == nil {
		return errors.New("transaction scheduler requires frame")
	}
	// Nested speculative execution stays inside the existing private snapshot.
	// The outer scheduler already pins the shared physical store for the whole
	// speculative transaction.
	if e.speculative {
		return e.run(id, f)
	}

	atomic.AddUint64(&s.runs, 1)
	atomic.AddUint64(&speculativeStarted, 1)
	inflight := atomic.AddInt64(&speculativeInflight, 1)
	for {
		peak := atomic.LoadInt64(&speculativePeak)
		if inflight <= peak || atomic.CompareAndSwapInt64(&speculativePeak, peak, inflight) {
			break
		}
	}
	defer atomic.AddInt64(&speculativeInflight, -1)

	// Lazy speculative Engines share the primary IndexedStore pointer. Pin that
	// physical descriptor before snapshot creation and keep it pinned through
	// conflict detection/commit so persistence cannot swap+close the old body in
	// the middle of cognition.
	_, releaseStore, err := e.acquireStoreLifetimeLease()
	if err != nil {
		return err
	}
	defer releaseStore()

	ce, base, err := e.snapshotForSpeculationLazy(snapshotMemoryLimit())
	if err != nil {
		if strings.Contains(err.Error(), "snapshot working-set limit exceeded") {
			atomic.AddUint64(&speculativeLimitFallback, 1)
			s.commitMu.Lock()
			defer s.commitMu.Unlock()
			return s.canonical(e, id, f)
		}
		return err
	}
	defer releaseLazySpeculation(ce)

	cf := cloneFrame(f)
	err = ce.run(id, cf)
	if err != nil {
		if errors.Is(err, errSpeculativeSideEffect) || ce.speculativeSideEffectHit() {
			atomic.AddUint64(&speculativeSideEffectFallback, 1)
			s.commitMu.Lock()
			defer s.commitMu.Unlock()
			return s.canonical(e, id, f)
		}
		return err
	}

	diff := diffSnapshot(base, ce)
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if !e.commitSnapshotDiff(base, diff) {
		atomic.AddUint64(&speculativeConflicted, 1)
		return s.canonical(e, id, f)
	}
	assignFrame(f, cf)
	atomic.AddUint64(&speculativeCommitted, 1)
	return nil
}

// classifyTargets reports only physical execution properties. It intentionally
// contains no utility/priority/confidence class and is diagnostic, not a
// scheduling policy input.
func (s *txnScheduler) classifyTargets(e *Engine, ids []string) []map[string]any {
	rows := make([]map[string]any, 0, len(ids))
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	for _, id := range ordered {
		m, err := e.resolveIDLocal(id)
		if err != nil || m == nil || len(m.Program) == 0 {
			continue
		}
		sideEffect := false
		for _, op := range m.Program {
			if speculativeForbiddenPrimitive(op.Code) || speculativeAdditionalForbiddenPrimitive(op.Code) {
				sideEffect = true
				break
			}
		}
		rows = append(rows, map[string]any{
			"id":                      id,
			"speculative_safe_direct": !sideEffect,
			"external_side_effect":    sideEffect,
			"classification":          "physical-boundary-only",
		})
	}
	return rows
}

func (s *txnScheduler) Info() map[string]any {
	return map[string]any{
		"mode":                "lazy-readset-snapshot-diff-conflict-replay",
		"runs":                atomic.LoadUint64(&s.runs),
		"canonical_runs":      atomic.LoadUint64(&s.canonicalRuns),
		"speculative":         speculativeInfo(),
		"snapshot_scope":      "dirty-new-plus-first-read",
		"store_lifetime":      "shared-physical-read-lease",
		"cognitive_priority":  false,
		"semantic_scheduling": false,
	}
}

func (s *txnScheduler) String() string {
	return fmt.Sprintf("txnScheduler(runs=%d)", atomic.LoadUint64(&s.runs))
}
