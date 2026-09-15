package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func setTestShardMax(t *testing.T, value string) {
	t.Helper()
	old := os.Getenv("MEMORYAI_SHARD_MAX_MEMORIES")
	if err := os.Setenv("MEMORYAI_SHARD_MAX_MEMORIES", value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("MEMORYAI_SHARD_MAX_MEMORIES", old) })
}

func loadFabricWriteTestEngine(t *testing.T, shardMemories []*Memory) *Engine {
	t.Helper()
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{root})
	writeBodyForPersistenceTest(t, filepath.Join(dir, "Memory.1.mem"), "storage", shardMemories)
	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	e.mountAutomaticStorageShards()
	if len(e.mountedSpacePaths()) != 1 {
		e.close()
		t.Fatalf("storage shard not recovered: %v", e.mountedSpacePaths())
	}
	t.Cleanup(e.close)
	return e
}

func TestExplicitImportCreatesInBoundedShard(t *testing.T) {
	setTestShardMax(t, "1")
	e := loadFabricWriteTestEngine(t, nil)

	incoming := &Memory{
		ID: "imported-child", Layer: "emergent", Tags: []string{"memory", "imported"},
		State: map[string]any{"v": "1"}, Revision: 1,
	}
	payload, _ := json.Marshal(incoming)
	id, status, err := e.importMemoryJSON(string(payload), false)
	if err != nil || id != incoming.ID || status != "imported" {
		t.Fatalf("bounded import failed: id=%q status=%q err=%v", id, status, err)
	}
	if _, err := e.resolveIDLocal(incoming.ID); err == nil {
		t.Fatal("full primary accepted explicit import instead of bounded storage shard")
	}
	owner, got, err := e.resolveLocalFabricMemory(incoming.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner == e || owner.manifest.Role != "storage" || got.Revision != 1 {
		t.Fatalf("import owner mismatch: ownerRole=%q revision=%d", owner.manifest.Role, got.Revision)
	}
	if localMemoryCountFast(e) != 1 || localMemoryCountFast(owner) != 1 {
		t.Fatalf("body bounds changed: primary=%d shard=%d", localMemoryCountFast(e), localMemoryCountFast(owner))
	}
}

func TestExplicitImportUpdatesExistingShardOwnerWithoutDuplicate(t *testing.T) {
	setTestShardMax(t, "1")
	existing := &Memory{
		ID: "existing-in-shard", Layer: "emergent", Tags: []string{"memory", "old"},
		State: map[string]any{"v": "1"}, Revision: 1,
	}
	e := loadFabricWriteTestEngine(t, []*Memory{existing})

	incoming := copyMemory(existing)
	incoming.Tags = []string{"memory", "new"}
	incoming.State = map[string]any{"v": "2"}
	incoming.Revision = 2
	payload, _ := json.Marshal(incoming)
	_, status, err := e.importMemoryJSON(string(payload), false)
	if err != nil || status != "imported" {
		t.Fatalf("shard update failed: status=%q err=%v", status, err)
	}
	if _, err := e.resolveIDLocal(existing.ID); err == nil {
		t.Fatal("existing shard ID was duplicated into primary")
	}
	owner, got, err := e.resolveLocalFabricMemory(existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner == e || got.Revision != 2 || got.State["v"] != "2" {
		t.Fatalf("existing shard owner not updated in place: owner=%p revision=%d state=%v", owner, got.Revision, got.State)
	}
	if localMemoryCountFast(owner) != 1 {
		t.Fatalf("update changed shard cardinality: %d", localMemoryCountFast(owner))
	}
}

func TestDuplicateFabricIdentityIsRejected(t *testing.T) {
	setTestShardMax(t, "2")
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	root := &Memory{ID: "duplicate", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{root})
	copyInShard := &Memory{ID: "duplicate", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	writeBodyForPersistenceTest(t, filepath.Join(dir, "Memory.1.mem"), "storage", []*Memory{copyInShard})
	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	e.mountAutomaticStorageShards()

	if _, _, err := e.resolveLocalFabricMemory("duplicate"); err == nil {
		t.Fatal("duplicate physical Memory identity was silently accepted")
	}
}

func TestSourceAdapterLivesAcrossStorageShard(t *testing.T) {
	setTestShardMax(t, "1")
	e := loadFabricWriteTestEngine(t, nil)

	first, err := e.upsertSourceAdapter(sourceAdapter{
		ID: "adapter-sharded", Config: map[string]string{"url": "http://127.0.0.1:1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, got, err := e.resolveLocalFabricMemory(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner == e || owner.manifest.Role != "storage" || got.Revision != 1 {
		t.Fatalf("source adapter not placed in shard: role=%q revision=%d", owner.manifest.Role, got.Revision)
	}

	second, err := e.upsertSourceAdapter(sourceAdapter{
		ID: "adapter-sharded", Config: map[string]string{"url": "http://127.0.0.1:2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	owner2, got2, err := e.resolveLocalFabricMemory(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner2 != owner || got2.Revision != 2 {
		t.Fatalf("source adapter update moved owner or revision: sameOwner=%v revision=%d", owner2 == owner, got2.Revision)
	}
	rows := e.listSourceAdapters()
	if len(rows) != 1 || rows[0]["id"] != "adapter-sharded" {
		t.Fatalf("source adapter Fabric listing failed: %#v", rows)
	}
	if err := e.deleteSourceAdapter("adapter-sharded"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.resolveLocalFabricMemory("adapter-sharded"); !errorsIsEOF(err) {
		t.Fatalf("deleted sharded source adapter still resolves: %v", err)
	}
}

func errorsIsEOF(err error) bool {
	return err == io.EOF
}
