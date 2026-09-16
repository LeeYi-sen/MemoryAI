package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testMeshMemoryRuntime(t *testing.T, dir, nodeID, role string) (*meshRuntime, *Engine) {
	t.Helper()
	path := filepath.Join(dir, "Memory.mem")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		writeBodyForPersistenceTest(t, path, "core", []*Memory{{
			ID: "mesh.test.seed", Layer: "emergent", Tags: []string{"memory"}, Revision: 1,
		}})
	}
	e, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	// Normalize the body once so the in-body mutation journal is available.
	if err := e.saveBody(e.bodyPath); err != nil {
		e.close()
		t.Fatal(err)
	}
	m := &meshRuntime{
		role: role, nodeID: nodeID, engine: e,
		directory: map[string]MeshNode{}, shared: map[string]MeshRecord{},
		client: &http.Client{},
	}
	t.Cleanup(func() {
		forgetMeshJournalState(m)
		e.close()
	})
	return m, e
}

func assertNoMeshJSONSidecars(t *testing.T, dir string) {
	t.Helper()
	for _, pattern := range []string{"Memory.mesh-journal.*.json", "Memory.mesh-proposal-replay.*.json"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("forbidden runtime sidecars exist: %v", matches)
		}
	}
}

func TestMeshDeferredJournalPersistsInsideMemoryAndRecovers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", "8")
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_BYTES", "65536")
	m, _ := testMeshMemoryRuntime(t, dir, "node-recover", "node")
	req := MeshRequest{Op: "shared_propose", MemoryID: "memory.a", ProposalDigest: "digest-a", Revision: 1, OriginNode: "node-recover"}
	if err := deferMeshRequest(m, req); err != nil {
		t.Fatal(err)
	}
	info := meshDeferredJournalInfo(m)
	if info["durability"] != "memory.mem" || info["sidecar"] != false {
		t.Fatalf("unexpected journal durability info: %#v", info)
	}
	assertNoMeshJSONSidecars(t, dir)

	m.engine.close()
	forgetMeshJournalState(m)
	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	m2 := &meshRuntime{role: "node", nodeID: "node-recover", engine: restored, directory: map[string]MeshNode{}, shared: map[string]MeshRecord{}, client: &http.Client{}}
	defer restored.close()
	defer forgetMeshJournalState(m2)
	if err := recoverMeshDeferredJournal(m2); err != nil {
		t.Fatal(err)
	}
	m2.mu.RLock()
	defer m2.mu.RUnlock()
	if len(m2.journal) != 1 || m2.journal[0].MemoryID != "memory.a" || m2.journal[0].ProposalDigest != "digest-a" {
		t.Fatalf("recovered deferred journal mismatch: %+v", m2.journal)
	}
}

func TestMeshDeferredJournalBackpressureDoesNotMutateMemory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", "1")
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_BYTES", "65536")
	m, e := testMeshMemoryRuntime(t, dir, "node-bounded", "node")
	first := MeshRequest{Op: "shared_propose", MemoryID: "memory.a", ProposalDigest: "one", OriginNode: "node-bounded"}
	if err := deferMeshRequest(m, first); err != nil {
		t.Fatal(err)
	}
	if err := deferMeshRequest(m, MeshRequest{Op: "shared_propose", MemoryID: "memory.b", ProposalDigest: "two", OriginNode: "node-bounded"}); err == nil {
		t.Fatal("bounded Memory journal accepted an entry beyond max_entries")
	}
	_, memory, err := e.resolveLocalFabricMemory(meshDeferredJournalMemoryIDForNode("node-bounded"))
	if err != nil {
		t.Fatal(err)
	}
	disk, _, err := decodeMeshJournalMemory(memory, 8, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Entries) != 1 || disk.Entries[0].MemoryID != "memory.a" {
		t.Fatalf("failed backpressure mutated durable Memory journal: %+v", disk.Entries)
	}
}

func TestMeshDeferredJournalFlushRemovesDurableEntries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", "8")
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_BYTES", "65536")
	t.Setenv("MEMORYAI_MESH_CAPABILITY_KEY", "journal-test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(MeshResponse{OK: true, Status: "accepted"})
	}))
	defer server.Close()
	m, e := testMeshMemoryRuntime(t, dir, "node-flush", "node")
	m.authorityURL = server.URL
	m.client = server.Client()
	if err := deferMeshRequest(m, MeshRequest{Op: "shared_propose", MemoryID: "memory.a", ProposalDigest: "digest-a", OriginNode: "node-flush"}); err != nil {
		t.Fatal(err)
	}
	if err := flushMeshDeferredJournal(m); err != nil {
		t.Fatal(err)
	}
	_, memory, err := e.resolveLocalFabricMemory(meshDeferredJournalMemoryIDForNode("node-flush"))
	if err != nil {
		t.Fatal(err)
	}
	disk, _, err := decodeMeshJournalMemory(memory, 8, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Entries) != 0 {
		t.Fatalf("flush did not durably remove deferred entries: %+v", disk.Entries)
	}
}
