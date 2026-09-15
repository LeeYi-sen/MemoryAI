package main

import (
	"os"
	"path/filepath"
	"testing"
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
