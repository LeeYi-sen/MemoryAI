package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeBodyForPersistenceTest(t *testing.T, path, role string, memories []*Memory) {
	t.Helper()
	records, ididx, tagidx, taglists, err := buildIndexedSections(memories)
	if err != nil {
		t.Fatal(err)
	}
	rootID := ""
	if role == "core" && len(memories) > 0 {
		rootID = memories[0].ID
	}
	g := Genesis{Format: "memoryai-genesis-index-v1", Root: rootID, MemoryCount: len(memories)}
	gb, _ := json.MarshalIndent(g, "", "  ")
	gb = append(gb, '\n')
	sm := StoreManifest{
		Records: "store/records.bin", IDIndex: "store/id.idx", TagIndex: "store/tag.idx",
		TagLists: "store/taglists.bin", IndexFormat: indexFormatV2, MemoryCount: len(memories),
	}
	entries := []zipEntry{
		{"genesis.json", gb, false},
		{sm.Records, records, true},
		{sm.IDIndex, ididx, true},
		{sm.TagIndex, tagidx, true},
		{sm.TagLists, taglists, true},
	}
	hashes := map[string]string{}
	for _, q := range entries {
		hashes[q.name] = fmt.Sprintf("%x", sha256.Sum256(q.b))
	}
	mf := Manifest{
		Role: role, Format: "memoryai-body-v2", Version: imageVersion, MemoryABI: memoryABI,
		BodyID: "test-body", Root: rootID, GenesisPath: "genesis.json", Entry: map[string]string{},
		Hashes: hashes, Store: sm,
	}
	mb, _ := json.MarshalIndent(mf, "", "  ")
	mb = append(mb, '\n')
	entries = append(entries, zipEntry{"manifest.json", mb, false})
	if err := writeDetZip(path, entries); err != nil {
		t.Fatal(err)
	}
}

func TestPersistReopensStoreAndClearsDeltas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", nil)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	oldStore := e.store
	m := &Memory{ID: "persisted-memory", Layer: "emergent", Tags: []string{"persist-test"}, State: map[string]any{"v": "1"}, Revision: 1}
	e.addRuntimeMemory(m)
	if !e.isDirty() {
		t.Fatal("new Memory did not mark body dirty")
	}
	if err := e.saveBody(path); err != nil {
		t.Fatal(err)
	}
	if e.store == oldStore {
		t.Fatal("persist did not reopen IndexedStore after atomic file replacement")
	}
	if e.isDirty() || len(e.newIDs) != 0 || len(e.dirtyIDs) != 0 || len(e.deletedIDs) != 0 || len(e.tagAdded) != 0 || len(e.tagRemoved) != 0 {
		t.Fatalf("persist deltas not cleared: dirty=%v new=%d changed=%d deleted=%d tagAdd=%d tagRemove=%d", e.isDirty(), len(e.newIDs), len(e.dirtyIDs), len(e.deletedIDs), len(e.tagAdded), len(e.tagRemoved))
	}
	got, err := e.store.GetID(m.ID)
	if err != nil || got == nil || got.ID != m.ID {
		t.Fatalf("reopened store cannot resolve persisted Memory: got=%v err=%v", got, err)
	}
}

func TestPersistAllSkipsCleanPrimaryRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", nil)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	before := e.store
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
	if e.store != before {
		t.Fatal("clean persistAll unnecessarily rewrote and reopened primary body")
	}
}

func TestBoundedShardPlacementUsesRecoveredStorageShard(t *testing.T) {
	old := os.Getenv("MEMORYAI_SHARD_MAX_MEMORIES")
	if err := os.Setenv("MEMORYAI_SHARD_MAX_MEMORIES", "1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("MEMORYAI_SHARD_MAX_MEMORIES", old) })

	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{root})
	if err := createEmptyBody(filepath.Join(dir, "Memory.1.mem")); err != nil {
		t.Fatal(err)
	}

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	e.mountAutomaticStorageShards()
	if len(e.mountedSpacePaths()) != 1 {
		t.Fatalf("automatic storage shard not recovered: %v", e.mountedSpacePaths())
	}

	child := &Memory{ID: "child", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	if err := e.placeRuntimeMemory(child); err != nil {
		t.Fatal(err)
	}
	if _, err := e.resolveIDLocal(child.ID); err == nil {
		t.Fatal("full primary incorrectly accepted new Memory")
	}
	owner := e.ownerOf(child.ID)
	if owner == nil || owner == e || owner.manifest.Role != "storage" {
		t.Fatalf("new Memory was not placed in passive shard: owner=%v", owner)
	}
	if localMemoryCountFast(owner) != 1 {
		t.Fatalf("unexpected shard count: %d", localMemoryCountFast(owner))
	}
}

func TestWildcardShardFilesRemainValidZipBodies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Memory.1.mem")
	if err := createEmptyBody(path); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr.Close()
}
