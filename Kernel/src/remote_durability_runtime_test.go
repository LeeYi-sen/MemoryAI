package main

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireRemoteMutationACKRejectsApplicationFailure(t *testing.T) {
	if err := requireRemoteMutationACK("space_put", `{"ok":true,"id":"m1"}`); err != nil {
		t.Fatalf("positive ACK rejected: %v", err)
	}
	if err := requireRemoteMutationACK("space_put", `{"ok":false,"error":"disk full"}`); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("negative ACK was not surfaced: %v", err)
	}
	if err := requireRemoteMutationACK("space_put", `not-json`); err == nil {
		t.Fatal("malformed mutation ACK was accepted")
	}
}

func TestTransferMemoryDurableMovePersistsTargetBeforeSourceRemoval(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	target := filepath.Join(dir, "MoveTarget.mem")
	rootMemory := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	source := &Memory{ID: "move-me", Layer: "emergent", Tags: []string{"memory", "move"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{rootMemory, source})
	writeBodyForPersistenceTest(t, target, "storage", nil)

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := transferMemoryDurable(e, source.ID, target, true)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if newID != source.ID {
		e.close()
		t.Fatalf("unexpected move id: got %q want %q", newID, source.ID)
	}
	e.close()

	targetEngine, err := loadEngine(target)
	if err != nil {
		t.Fatal(err)
	}
	got, err := targetEngine.resolveIDLocal(source.ID)
	if err != nil || got == nil || got.State["v"] != "1" {
		targetEngine.close()
		t.Fatalf("destination was not durable after successful move: got=%v err=%v", got, err)
	}
	targetEngine.close()

	sourceEngine, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceEngine.close()
	if _, err := sourceEngine.resolveIDLocal(source.ID); !errors.Is(err, io.EOF) {
		t.Fatalf("source remained durable after successful move: %v", err)
	}
}

func TestTransferMemoryDurableTargetFailureRetainsSource(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	target := filepath.Join(dir, "MoveTarget.mem")
	rootMemory := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	source := &Memory{ID: "retain-me", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "safe"}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{rootMemory, source})
	writeBodyForPersistenceTest(t, target, "storage", nil)

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := e.mountSpace(target)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	e.spaceMu.RLock()
	dst := e.spaces[cp]
	e.spaceMu.RUnlock()
	if dst == nil {
		e.close()
		t.Fatal("target did not mount")
	}
	originalBodyPath := dst.bodyPath
	dst.bodyPath = filepath.Join(dir, "missing-parent", "cannot-persist.mem")

	_, err = transferMemoryDurable(e, source.ID, target, true)
	dst.bodyPath = originalBodyPath
	if err == nil {
		e.close()
		t.Fatal("target persistence failure was reported as move success")
	}
	owner, got, resolveErr := e.resolveLocalFabricMemoryCopy(source.ID)
	if resolveErr != nil || owner != e || got == nil || got.State["v"] != "safe" {
		e.close()
		t.Fatalf("source was not retained after destination failure: owner=%p got=%v err=%v", owner, got, resolveErr)
	}
	if _, targetErr := resolveSpecificOwnerMemoryCopy(dst, source.ID); !errors.Is(targetErr, io.EOF) {
		e.close()
		t.Fatalf("failed transfer candidate remained in live target after rollback: %v", targetErr)
	}
	unrelated := &Memory{ID: "later-write", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "later"}, Revision: 1}
	dst.addRuntimeMemory(unrelated)
	if persistErr := persistEngineIfDirty(dst); persistErr != nil {
		e.close()
		t.Fatalf("later unrelated target persistence failed: %v", persistErr)
	}
	e.close()

	targetEngine, err := loadEngine(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, targetErr := targetEngine.resolveIDLocal(source.ID); !errors.Is(targetErr, io.EOF) {
		targetEngine.close()
		t.Fatalf("failed transfer candidate leaked into later durable target commit: %v", targetErr)
	}
	if later, laterErr := targetEngine.resolveIDLocal(unrelated.ID); laterErr != nil || later == nil || later.State["v"] != "later" {
		targetEngine.close()
		t.Fatalf("later unrelated target write missing: got=%v err=%v", later, laterErr)
	}
	targetEngine.close()

	sourceEngine, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceEngine.close()
	if _, err := sourceEngine.resolveIDLocal(source.ID); err != nil {
		t.Fatalf("durable source disappeared after destination failure: %v", err)
	}
}

func TestTransferSourceChangePreventsDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	source := &Memory{ID: "changing", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{source})
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	snapshot, err := resolveSpecificOwnerMemoryCopy(e, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	live, err := e.resolveIDLocal(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.dataMu.Lock()
	live.State["v"] = "2"
	live.Revision = 2
	e.dirtyIDs[live.ID] = true
	e.dirty = true
	e.dataMu.Unlock()

	err = markTransferSourceDeletedIfUnchanged(e, source.ID, structuralMemoryDigest(snapshot))
	if err == nil || !strings.Contains(err.Error(), "changed after destination durability") {
		t.Fatalf("changed source was deleted or wrong error returned: %v", err)
	}
	got, err := resolveSpecificOwnerMemoryCopy(e, source.ID)
	if err != nil || got.State["v"] != "2" {
		t.Fatalf("changed source was not retained: got=%v err=%v", got, err)
	}
}

func TestTransferDeletePersistenceFailureRestoresSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Memory.mem")
	source := &Memory{ID: "restore-me", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{source})
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	originalBodyPath := e.bodyPath
	e.bodyPath = filepath.Join(dir, "missing-parent", "cannot-persist.mem")

	snapshot, err := resolveSpecificOwnerMemoryCopy(e, source.ID)
	if err != nil {
		e.bodyPath = originalBodyPath
		e.close()
		t.Fatal(err)
	}
	err = durableDeleteTransferSource(e, snapshot)
	e.bodyPath = originalBodyPath
	if err == nil {
		e.close()
		t.Fatal("source delete persistence failure was reported as success")
	}
	got, resolveErr := resolveSpecificOwnerMemoryCopy(e, source.ID)
	if resolveErr != nil || got == nil || got.State["v"] != "1" {
		e.close()
		t.Fatalf("source was not restored after delete persistence failure: got=%v err=%v", got, resolveErr)
	}
	e.close()
}

func TestTransferMemoryDurableRejectsFullExplicitTarget(t *testing.T) {
	t.Setenv("MEMORYAI_SHARD_MAX_MEMORIES", "1")
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	target := filepath.Join(dir, "FullTarget.mem")
	rootMemory := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	source := &Memory{ID: "capacity-source", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "1"}, Revision: 1}
	occupied := &Memory{ID: "occupied", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{rootMemory, source})
	writeBodyForPersistenceTest(t, target, "storage", []*Memory{occupied})

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transferMemoryDurable(e, source.ID, target, false)
	if err == nil || !strings.Contains(err.Error(), "target full") {
		e.close()
		t.Fatalf("full explicit target accepted transfer: %v", err)
	}
	if _, got, resolveErr := e.resolveLocalFabricMemoryCopy(source.ID); resolveErr != nil || got == nil {
		e.close()
		t.Fatalf("source disappeared after capacity rejection: got=%v err=%v", got, resolveErr)
	}
	e.close()

	targetEngine, err := loadEngine(target)
	if err != nil {
		t.Fatal(err)
	}
	defer targetEngine.close()
	if _, err := targetEngine.resolveIDLocal(source.ID); !errors.Is(err, io.EOF) {
		t.Fatalf("capacity-rejected source leaked into durable target: %v", err)
	}
	if _, err := targetEngine.resolveIDLocal(occupied.ID); err != nil {
		t.Fatalf("existing target Memory disappeared: %v", err)
	}
}

func TestRollbackUnpersistedTransferCandidateRemovesOnlyExactNewCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", nil)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	candidate := &Memory{ID: "rollback-candidate", Layer: "emergent", Tags: []string{"memory", "transfer"}, State: map[string]any{"v": "1"}, Revision: 1}
	if err := addTransferTargetCandidateBounded(e, candidate); err != nil {
		t.Fatal(err)
	}
	if !rollbackUnpersistedTransferCandidate(e, candidate) {
		t.Fatal("exact unpersisted transfer candidate was not rolled back")
	}
	if _, err := resolveSpecificOwnerMemoryCopy(e, candidate.ID); !errors.Is(err, io.EOF) {
		t.Fatalf("rolled-back candidate still resolves: %v", err)
	}
	if e.isDirty() {
		t.Fatal("candidate-only rollback left target body dirty")
	}
}

func TestRollbackUnpersistedTransferCandidatePreservesOtherDirtyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", nil)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	pending := &Memory{ID: "pending-before-transfer", Layer: "emergent", Tags: []string{"memory", "pending"}, State: map[string]any{"v": "keep"}, Revision: 1}
	e.addRuntimeMemory(pending)
	candidate := &Memory{ID: "rollback-second", Layer: "emergent", Tags: []string{"memory", "transfer"}, State: map[string]any{"v": "drop"}, Revision: 1}
	if err := addTransferTargetCandidateBounded(e, candidate); err != nil {
		t.Fatal(err)
	}
	if !rollbackUnpersistedTransferCandidate(e, candidate) {
		t.Fatal("transfer candidate rollback unexpectedly failed")
	}
	if !e.isDirty() {
		t.Fatal("rollback cleared pre-existing dirty target state")
	}
	if got, err := resolveSpecificOwnerMemoryCopy(e, pending.ID); err != nil || got == nil || got.State["v"] != "keep" {
		t.Fatalf("rollback damaged unrelated dirty Memory: got=%v err=%v", got, err)
	}
	if _, err := resolveSpecificOwnerMemoryCopy(e, candidate.ID); !errors.Is(err, io.EOF) {
		t.Fatalf("rollback candidate still resolves: %v", err)
	}
}

func TestFailedTransferPersistenceKeepsCandidateWhenDiskAlreadyContainsIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", nil)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	candidate := &Memory{ID: "already-durable", Layer: "emergent", Tags: []string{"memory", "transfer"}, State: map[string]any{"v": "1"}, Revision: 1}
	if err := addTransferTargetCandidateBounded(e, candidate); err != nil {
		t.Fatal(err)
	}
	// Simulate a post-rename/finalization failure: the physical body contains the
	// candidate even though the live Engine still believes it is pending.
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{copyMemory(candidate)})
	if outcome := handleFailedTransferTargetPersistence(e, candidate); outcome != "durable_duplicate_retained" {
		t.Fatalf("durable candidate was rolled back or misclassified: %s", outcome)
	}
	if got, err := resolveSpecificOwnerMemoryCopy(e, candidate.ID); err != nil || got == nil {
		t.Fatalf("live candidate disappeared after durable-on-disk detection: got=%v err=%v", got, err)
	}
}
