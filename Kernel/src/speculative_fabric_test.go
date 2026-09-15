package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newSpeculativeFabricTestEngine(t *testing.T) (*Engine, *Engine) {
	t.Helper()
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	shardPath := filepath.Join(dir, "Memory.1.mem")
	root := &Memory{
		ID: "root", Layer: "inherited", Tags: []string{"memory"},
		State: map[string]any{}, Revision: 1,
	}
	target := &Memory{
		ID: "shard.target", Layer: "emergent", Tags: []string{"memory", "txn-shard"},
		State: map[string]any{"value": "before"}, Revision: 1,
	}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{root})
	writeBodyForPersistenceTest(t, shardPath, "storage", []*Memory{target})

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.close() })
	e.mountAutomaticStorageShards()

	owner, got, err := e.resolveLocalFabricMemory(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner == nil || owner == e || owner.manifest.Role != "storage" {
		t.Fatalf("target did not resolve to passive shard: owner=%v", owner)
	}
	if got == nil || got.ID != target.ID {
		t.Fatalf("unexpected target resolution: %#v", got)
	}
	return e, owner
}

func TestSpeculativeCommitPreservesShardOwner(t *testing.T) {
	e, owner := newSpeculativeFabricTestEngine(t)

	ce, base, err := e.snapshotForSpeculationLazy(8)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLazySpeculation(ce)

	m, err := ce.resolveIDLocal("shard.target")
	if err != nil {
		t.Fatal(err)
	}
	if m.State == nil {
		m.State = map[string]any{}
	}
	m.State["value"] = "after"
	ce.dataMu.Lock()
	ce.dirtyIDs[m.ID] = true
	ce.dirty = true
	ce.dataMu.Unlock()

	diff := diffSnapshot(base, ce)
	if !e.commitFabricSnapshotDiff(base, diff, ce) {
		t.Fatal("cross-shard speculative commit unexpectedly conflicted")
	}

	updated, err := owner.resolveIDLocal("shard.target")
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.State["value"]; got != "after" {
		t.Fatalf("shard owner did not receive speculative update: %v", got)
	}
	if _, err := e.resolveIDLocal("shard.target"); err == nil {
		t.Fatal("speculative commit copied shard Memory into primary body")
	}
	if resolvedOwner, _, err := e.resolveLocalFabricMemory("shard.target"); err != nil || resolvedOwner != owner {
		t.Fatalf("physical owner changed after commit: owner=%v err=%v", resolvedOwner, err)
	}
}

func TestSpeculativeShardConflictDoesNotOverwriteConcurrentMutation(t *testing.T) {
	e, owner := newSpeculativeFabricTestEngine(t)

	ce, base, err := e.snapshotForSpeculationLazy(8)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLazySpeculation(ce)

	m, err := ce.resolveIDLocal("shard.target")
	if err != nil {
		t.Fatal(err)
	}
	m.State["value"] = "speculative"
	ce.dataMu.Lock()
	ce.dirtyIDs[m.ID] = true
	ce.dirty = true
	ce.dataMu.Unlock()

	current, err := owner.resolveIDLocal("shard.target")
	if err != nil {
		t.Fatal(err)
	}
	q := copyMemory(current)
	q.State["value"] = "concurrent"
	q.Revision++
	if err := upsertExplicitMemoryOnOwner(owner, q); err != nil {
		t.Fatal(err)
	}

	diff := diffSnapshot(base, ce)
	if e.commitFabricSnapshotDiff(base, diff, ce) {
		t.Fatal("conflicting shard transaction committed instead of replaying")
	}

	final, err := owner.resolveIDLocal("shard.target")
	if err != nil {
		t.Fatal(err)
	}
	if got := final.State["value"]; got != "concurrent" {
		t.Fatalf("conflict path overwrote concurrent shard mutation: %v", got)
	}
	if _, err := os.Stat(owner.bodyPath); err != nil {
		t.Fatalf("shard body unexpectedly disappeared: %v", err)
	}
}

func TestSpeculativeShardFallbackDoesNotReenterPinnedPrimaryStore(t *testing.T) {
	e, _ := newSpeculativeFabricTestEngine(t)
	ce, _, err := e.snapshotForSpeculationLazy(8)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLazySpeculation(ce)

	guard := indexedStoreGuard(e.store)
	writerStarted := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		close(writerStarted)
		guard.Lock()
		guard.Unlock()
		close(writerDone)
	}()
	<-writerStarted
	time.Sleep(20 * time.Millisecond)

	readDone := make(chan error, 1)
	go func() {
		_, er := ce.resolveIDLocal("shard.target")
		readDone <- er
	}()
	select {
	case er := <-readDone:
		if er != nil {
			t.Fatalf("shard fallback failed while primary writer waited: %v", er)
		}
	case <-time.After(500 * time.Millisecond):
		releaseLazySpeculation(ce)
		t.Fatal("shard fallback deadlocked by re-entering pinned primary Store")
	}

	releaseLazySpeculation(ce)
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("primary Store writer did not resume after snapshot release")
	}
}

func TestSpeculativeUnreadOwnerHintRetargetsAfterShardMove(t *testing.T) {
	e, oldOwner := newSpeculativeFabricTestEngine(t)
	ce, base, err := e.snapshotForSpeculationLazy(8)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLazySpeculation(ce)

	// Discover a physical owner without actually reading/baselining the Memory.
	ids, err := ce.storeTagIDs("txn-shard")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "shard.target" {
		t.Fatalf("unexpected tag candidates: %#v", ids)
	}
	if len(base.memories) != 0 {
		t.Fatalf("owner hint unexpectedly consumed read-set: %d", len(base.memories))
	}
	if hinted, ok := speculativePhysicalOwner(ce, "shard.target"); !ok || hinted != oldOwner {
		t.Fatalf("old owner hint missing: owner=%v ok=%v", hinted, ok)
	}

	// Move the same physical identity after the hint but before the first read.
	if err := e.deleteExplicitMemoryBounded("shard.target"); err != nil {
		t.Fatal(err)
	}
	moved := &Memory{
		ID: "shard.target", Layer: "emergent", Tags: []string{"memory", "txn-shard"},
		State: map[string]any{"value": "moved"}, Revision: 2,
	}
	newPath := filepath.Join(filepath.Dir(e.bodyPath), "Memory.2.mem")
	writeBodyForPersistenceTest(t, newPath, "storage", []*Memory{moved})
	if _, err := e.mountSpace(newPath); err != nil {
		t.Fatal(err)
	}
	newOwner, got, err := e.resolveLocalFabricMemory("shard.target")
	if err != nil {
		t.Fatal(err)
	}
	if newOwner == oldOwner || got.State["value"] != "moved" {
		t.Fatalf("test topology did not move owner: old=%p new=%p value=%v", oldOwner, newOwner, got.State["value"])
	}

	resolved, err := ce.resolveIDLocal("shard.target")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State["value"] != "moved" {
		t.Fatalf("speculative read used stale owner data: %v", resolved.State["value"])
	}
	if len(base.memories) != 1 {
		t.Fatalf("moved Memory not baselined exactly once: %d", len(base.memories))
	}
	if owner, ok := speculativePhysicalOwner(ce, "shard.target"); !ok || owner != newOwner {
		t.Fatalf("stale owner hint was not retargeted: owner=%p new=%p ok=%v", owner, newOwner, ok)
	}
}
