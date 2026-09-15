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
	root := fabricRootFor(e)
	if root != nil {
		owner, _, err := root.resolveLocalFabricMemory(id)
		if err == nil && owner != nil {
			rememberFabricOwner(root, owner)
			return owner.run(id, f)
		}
	}
	return e.run(id, f)
}

func (s *txnScheduler) run(e *Engine, id string, f *Frame) error {
	if e == nil {
		return errors.New("transaction scheduler requires engine")
	}
	if f == nil {
		return errors.New("transaction scheduler requires frame")
	}
	if e.speculative {
		return e.run(id, f)
	}

	root := fabricRootFor(e)
	if root == nil {
		root = e
	}
	e = root

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
			// No speculative page-in will occur after this point. Release the
			// primary Store R lease before taking the canonical commit lane; a
			// persistence finalizer may already hold dataMu while waiting for the
			// Store write guard.
			releaseSpeculativeStoreLease(ce)
			s.commitMu.Lock()
			defer s.commitMu.Unlock()
			return s.canonical(e, id, f)
		}
		return err
	}

	diff := diffSnapshot(base, ce)
	// The read-set and owner map are complete once diff is formed. Keep that
	// logical context, but stop pinning the primary IndexedStore before commit.
	releaseSpeculativeStoreLease(ce)

	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if !e.commitFabricSnapshotDiff(base, diff, ce) {
		atomic.AddUint64(&speculativeConflicted, 1)
		return s.canonical(e, id, f)
	}
	assignFrame(f, cf)
	atomic.AddUint64(&speculativeCommitted, 1)
	return nil
}

func (s *txnScheduler) classifyTargets(e *Engine, ids []string) []map[string]any {
	rows := make([]map[string]any, 0, len(ids))
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	root := fabricRootFor(e)
	if root == nil {
		root = e
	}
	for _, id := range ordered {
		_, m, err := root.resolveLocalFabricMemory(id)
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
		"mode":                "lazy-local-fabric-readset-conflict-replay",
		"runs":                atomic.LoadUint64(&s.runs),
		"canonical_runs":      atomic.LoadUint64(&s.canonicalRuns),
		"speculative":         speculativeInfo(),
		"snapshot_scope":      "first-read-across-local-fabric",
		"store_lifetime":      "execution-long-lease+release-before-commit",
		"canonical_replay":    "fabric-owner-aware",
		"creation_policy":     "canonical-only-bounded-placement",
		"cognitive_priority":  false,
		"semantic_scheduling": false,
	}
}

func (s *txnScheduler) String() string {
	return fmt.Sprintf("txnScheduler(runs=%d)", atomic.LoadUint64(&s.runs))
}
