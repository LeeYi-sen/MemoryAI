package main

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func proposalReplayFrame(revision string) *Frame {
	f := newFrame()
	f.Vars["memory_id"] = "shared.memory"
	f.Vars["origin_node"] = "node-a"
	f.Vars["proposal_digest"] = "digest-a"
	f.Vars["revision"] = revision
	return f
}

func TestMeshProposalReplayReceiptLivesInsideMemoryAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m, e := testMeshMemoryRuntime(t, dir, "sovereign-a", "sovereign")
	_ = m
	f := proposalReplayFrame("1")
	ev := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f.Vars)
	st, entry, receiptRevision, replay, err := prepareMeshProposalReplay(e, f, ev)
	if err != nil || replay {
		t.Fatalf("prepare failed replay=%v err=%v", replay, err)
	}
	f.Vars["mesh_decision"] = "approved"
	if err := finalizeMeshProposalReplay(e, st, entry, receiptRevision, f, nil); err != nil {
		t.Fatal(err)
	}
	assertNoMeshJSONSidecars(t, dir)

	e.close()
	forgetMeshProposalReplayState(e)
	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	f2 := proposalReplayFrame("1")
	st2, entry2, _, replay2, err := prepareMeshProposalReplay(restored, f2, ev)
	_ = st2
	if err != nil || !replay2 || entry2.State != meshProposalReplayDone {
		t.Fatalf("restart did not recover completed receipt: replay=%v entry=%+v err=%v", replay2, entry2, err)
	}
	if err := applyMeshProposalReplayResult(f2, entry2); err != nil {
		t.Fatal(err)
	}
	if f2.Vars["mesh_decision"] != "approved" {
		t.Fatalf("replayed result missing: %#v", f2.Vars)
	}
}

func TestMeshProposalReplayExecutingFenceRefusesRestartReplay(t *testing.T) {
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "sovereign-b", "sovereign")
	f := proposalReplayFrame("1")
	ev := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f.Vars)
	_, entry, _, replay, err := prepareMeshProposalReplay(e, f, ev)
	if err != nil || replay {
		t.Fatalf("prepare failed replay=%v err=%v", replay, err)
	}
	if entry.State != meshProposalReplayExecuting {
		t.Fatalf("expected executing fence, got %+v", entry)
	}

	e.close()
	forgetMeshProposalReplayState(e)
	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	_, recovered, _, replay, err := prepareMeshProposalReplay(restored, proposalReplayFrame("1"), ev)
	if !replay || err == nil || recovered.State != meshProposalReplayExecuting {
		t.Fatalf("indeterminate receipt was not restart-fenced: replay=%v entry=%+v err=%v", replay, recovered, err)
	}
}

func TestMeshProposalReplaySupersededDoneReceiptCanBeCollectedWithoutReexecution(t *testing.T) {
	t.Setenv("MEMORYAI_MESH_REPLAY_MAX_ENTRIES", "1")
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "sovereign-gc", "sovereign")

	f1 := proposalReplayFrame("1")
	ev1 := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f1.Vars)
	st1, entry1, receiptRevision1, replay, err := prepareMeshProposalReplay(e, f1, ev1)
	if err != nil || replay {
		t.Fatalf("revision 1 prepare failed replay=%v err=%v", replay, err)
	}
	f1.Vars["mesh_decision"] = "approved-v1"
	if err := finalizeMeshProposalReplay(e, st1, entry1, receiptRevision1, f1, nil); err != nil {
		t.Fatal(err)
	}

	f2 := proposalReplayFrame("2")
	f2.Vars["proposal_digest"] = "digest-b"
	ev2 := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f2.Vars)
	st2, entry2, receiptRevision2, replay, err := prepareMeshProposalReplay(e, f2, ev2)
	if err != nil || replay {
		t.Fatalf("revision 2 should reclaim superseded done receipt instead of hitting ledger backpressure: replay=%v err=%v", replay, err)
	}
	if _, _, found, err := loadMeshProposalReplayEntry(e, entry1.RequestID); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatalf("superseded revision 1 receipt still occupies replay ledger")
	}
	f2.Vars["mesh_decision"] = "approved-v2"
	if err := finalizeMeshProposalReplay(e, st2, entry2, receiptRevision2, f2, nil); err != nil {
		t.Fatal(err)
	}

	e.close()
	forgetMeshProposalReplayState(e)
	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	_, recovered, _, replay, err := prepareMeshProposalReplay(restored, proposalReplayFrame("1"), ev1)
	if replay || err == nil || !strings.Contains(err.Error(), "stale unseen mesh proposal revision") {
		t.Fatalf("collected old request must remain restart-fenced by slot high-watermark: replay=%v entry=%+v err=%v", replay, recovered, err)
	}
}

func TestMeshProposalReplayGCNeverCollectsExecutingFence(t *testing.T) {
	t.Setenv("MEMORYAI_MESH_REPLAY_MAX_ENTRIES", "1")
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "sovereign-gc-executing", "sovereign")

	f1 := proposalReplayFrame("1")
	ev1 := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f1.Vars)
	_, entry1, _, replay, err := prepareMeshProposalReplay(e, f1, ev1)
	if err != nil || replay || entry1.State != meshProposalReplayExecuting {
		t.Fatalf("revision 1 prepare failed replay=%v entry=%+v err=%v", replay, entry1, err)
	}

	e.close()
	forgetMeshProposalReplayState(e)
	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	f2 := proposalReplayFrame("2")
	f2.Vars["proposal_digest"] = "digest-b"
	ev2 := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f2.Vars)
	_, _, _, replay, err = prepareMeshProposalReplay(restored, f2, ev2)
	if replay || err == nil || !strings.Contains(err.Error(), "ledger full") {
		t.Fatalf("executing receipt must remain durable and force backpressure: replay=%v err=%v", replay, err)
	}
	if recovered, _, found, err := loadMeshProposalReplayEntry(restored, entry1.RequestID); err != nil {
		t.Fatal(err)
	} else if !found || recovered.State != meshProposalReplayExecuting {
		t.Fatalf("executing receipt was collected: found=%v entry=%+v", found, recovered)
	}
}

func TestSovereignDirectoryAndSharedAuthorizationRecoverFromMemory(t *testing.T) {
	dir := t.TempDir()
	m, e := testMeshMemoryRuntime(t, dir, "sovereign-c", "sovereign")
	node := MeshNode{ID: "node-x", Role: "node", Endpoint: "https://node-x.invalid", LastSeen: 123}
	if err := persistSovereignMeshNode(m, node); err != nil {
		t.Fatal(err)
	}
	record := MeshRecord{MemoryID: "memory.remote", OriginNode: "node-x", Endpoint: node.Endpoint, Digest: "abc", Revision: 7, Tags: []string{"fact"}}
	if err := persistSovereignSharedRecord(m, record); err != nil {
		t.Fatal(err)
	}
	e.close()

	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	m2 := &meshRuntime{role: "sovereign", nodeID: "sovereign-c", engine: restored, directory: map[string]MeshNode{}, shared: map[string]MeshRecord{}, client: &http.Client{}}
	if err := recoverSovereignMeshState(m2); err != nil {
		t.Fatal(err)
	}
	if got := m2.directory["node-x"]; got.Endpoint != node.Endpoint {
		t.Fatalf("directory did not recover from Memory: %+v", got)
	}
	if got := m2.shared["memory.remote"]; got.Revision != 7 || got.OriginNode != "node-x" {
		t.Fatalf("shared authorization did not recover from Memory: %+v", got)
	}
}
