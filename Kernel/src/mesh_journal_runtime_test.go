package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testMeshJournalRuntime(t *testing.T, dir, nodeID string) *meshRuntime {
	t.Helper()
	bodyPath := filepath.Join(dir, "Memory.mem")
	if err := os.WriteFile(bodyPath, []byte("test-body"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &meshRuntime{
		role:      "node",
		nodeID:    nodeID,
		engine:    &Engine{bodyPath: bodyPath},
		directory: map[string]MeshNode{},
		shared:    map[string]MeshRecord{},
		client:    &http.Client{},
	}
	t.Cleanup(func() { forgetMeshJournalState(m) })
	return m
}

func TestMeshDeferredJournalPersistsAndRecovers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", "8")
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_BYTES", "65536")
	m := testMeshJournalRuntime(t, dir, "node-recover")
	req := MeshRequest{Op: "shared_propose", MemoryID: "memory.a", ProposalDigest: "digest-a", Revision: 1, OriginNode: "node-recover"}
	if err := deferMeshRequest(m, req); err != nil {
		t.Fatal(err)
	}
	info := meshDeferredJournalInfo(m)
	path, _ := info["path"].(string)
	if path == "" {
		t.Fatal("mesh journal did not expose a physical spool path")
	}
	if stat, err := os.Stat(path); err != nil || stat.Size() == 0 {
		t.Fatalf("mesh journal spool was not persisted: stat=%v err=%v", stat, err)
	}

	m2 := testMeshJournalRuntime(t, dir, "node-recover")
	if err := recoverMeshDeferredJournal(m2); err != nil {
		t.Fatal(err)
	}
	m2.mu.RLock()
	defer m2.mu.RUnlock()
	if len(m2.journal) != 1 || m2.journal[0].MemoryID != "memory.a" || m2.journal[0].ProposalDigest != "digest-a" {
		t.Fatalf("recovered deferred journal mismatch: %+v", m2.journal)
	}
}

func TestMeshDeferredJournalBackpressureAndProposalReplacement(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", "2")
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_BYTES", "65536")
	m := testMeshJournalRuntime(t, dir, "node-bounded")

	first := MeshRequest{Op: "shared_propose", MemoryID: "memory.a", ProposalDigest: "digest-1", Revision: 1, OriginNode: "node-bounded"}
	if err := deferMeshRequest(m, first); err != nil {
		t.Fatal(err)
	}
	newer := first
	newer.ProposalDigest = "digest-2"
	newer.Revision = 2
	if err := deferMeshRequest(m, newer); err != nil {
		t.Fatal(err)
	}
	if err := deferMeshRequest(m, MeshRequest{Op: "shared_propose", MemoryID: "memory.b", ProposalDigest: "digest-b", OriginNode: "node-bounded"}); err != nil {
		t.Fatal(err)
	}
	if err := deferMeshRequest(m, MeshRequest{Op: "shared_propose", MemoryID: "memory.c", ProposalDigest: "digest-c", OriginNode: "node-bounded"}); err == nil {
		t.Fatal("bounded mesh journal accepted an entry beyond max_entries")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.journal) != 2 {
		t.Fatalf("proposal replacement increased bounded journal length: %d", len(m.journal))
	}
	if m.journal[0].MemoryID != "memory.a" || m.journal[0].ProposalDigest != "digest-2" || m.journal[0].Revision != 2 {
		t.Fatalf("newer proposal did not replace stale deferred proposal: %+v", m.journal[0])
	}
}

func TestMeshDeferredJournalByteBackpressureDoesNotMutateSpool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", "8")
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_BYTES", "300")
	m := testMeshJournalRuntime(t, dir, "node-bytes")
	base := MeshRequest{Op: "shared_propose", MemoryID: "memory.a", ProposalDigest: "a", OriginNode: "node-bytes"}
	if err := deferMeshRequest(m, base); err != nil {
		t.Fatal(err)
	}
	info := meshDeferredJournalInfo(m)
	path := info["path"].(string)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	huge := MeshRequest{Op: "shared_propose", MemoryID: "memory.b", ProposalDigest: string(make([]byte, 512)), OriginNode: "node-bytes"}
	if err := deferMeshRequest(m, huge); err == nil {
		t.Fatal("mesh journal accepted a request beyond max_bytes")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed backpressure attempt mutated durable mesh spool")
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

	m := testMeshJournalRuntime(t, dir, "node-flush")
	m.authorityURL = server.URL
	m.client = server.Client()
	if err := deferMeshRequest(m, MeshRequest{Op: "shared_propose", MemoryID: "memory.a", ProposalDigest: "digest-a", OriginNode: "node-flush"}); err != nil {
		t.Fatal(err)
	}
	if err := deferMeshRequest(m, MeshRequest{Op: "shared_propose", MemoryID: "memory.b", ProposalDigest: "digest-b", OriginNode: "node-flush"}); err != nil {
		t.Fatal(err)
	}
	if err := flushMeshDeferredJournal(m); err != nil {
		t.Fatal(err)
	}
	m.mu.RLock()
	remaining := len(m.journal)
	m.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("flush left in-memory deferred entries: %d", remaining)
	}

	m2 := testMeshJournalRuntime(t, dir, "node-flush")
	if err := recoverMeshDeferredJournal(m2); err != nil {
		t.Fatal(err)
	}
	m2.mu.RLock()
	defer m2.mu.RUnlock()
	if len(m2.journal) != 0 {
		t.Fatalf("flush did not durably remove deferred entries: %+v", m2.journal)
	}
}

func TestMeshDeferredJournalRejectsOversizedRecovery(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", "1")
	t.Setenv("MEMORYAI_MESH_JOURNAL_MAX_BYTES", "65536")
	m := testMeshJournalRuntime(t, dir, "node-oversized")
	st := meshJournalStateFor(m)
	path, err := meshJournalPath(m)
	if err != nil {
		t.Fatal(err)
	}
	st.path = path
	raw, err := json.Marshal(meshJournalDisk{Version: meshJournalVersion, NodeID: "node-oversized", Entries: []MeshRequest{
		{Op: "shared_propose", MemoryID: "a"},
		{Op: "shared_propose", MemoryID: "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverMeshDeferredJournal(m); err == nil {
		t.Fatal("oversized deferred journal was accepted during recovery")
	}
}
