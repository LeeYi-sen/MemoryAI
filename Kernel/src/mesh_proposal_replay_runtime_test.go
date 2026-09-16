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

func proposalReplayRequest(revision uint64, digest string) MeshRequest {
	return MeshRequest{
		Op:             "shared_propose",
		MemoryID:       "shared.memory",
		OriginNode:     "node-a",
		ProposalDigest: digest,
		Revision:       revision,
	}
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

func TestMeshProposalReplayAckCollectsDoneReceiptAndKeepsRestartFence(t *testing.T) {
	dir := t.TempDir()
	m, e := testMeshMemoryRuntime(t, dir, "sovereign-ack", "sovereign")
	f := proposalReplayFrame("1")
	ev := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f.Vars)
	st, entry, receiptRevision, replay, err := prepareMeshProposalReplay(e, f, ev)
	if err != nil || replay {
		t.Fatalf("prepare failed replay=%v err=%v", replay, err)
	}
	f.Vars["mesh_decision"] = "approved-v1"
	if err := finalizeMeshProposalReplay(e, st, entry, receiptRevision, f, nil); err != nil {
		t.Fatal(err)
	}

	ack := proposalReplayRequest(1, "digest-a")
	ack.Op = "shared_proposal_ack"
	if res := m.handleAuthority(ack); !res.OK || res.Status != "acknowledged" {
		t.Fatalf("completed proposal ack failed: %+v", res)
	}
	if _, _, found, err := loadMeshProposalReplayEntry(e, entry.RequestID); err != nil {
		t.Fatal(err)
	} else if found {
		t.Fatalf("acknowledged done receipt was not collected")
	}

	_, _, _, replay, err = prepareMeshProposalReplay(e, proposalReplayFrame("1"), ev)
	if replay || err == nil || !strings.Contains(err.Error(), "conflicting mesh proposal identity at revision 1") {
		t.Fatalf("acknowledged request must remain fenced before restart: replay=%v err=%v", replay, err)
	}

	e.close()
	forgetMeshProposalReplayState(e)
	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	_, _, _, replay, err = prepareMeshProposalReplay(restored, proposalReplayFrame("1"), ev)
	if replay || err == nil || !strings.Contains(err.Error(), "conflicting mesh proposal identity at revision 1") {
		t.Fatalf("acknowledged request lost restart fence: replay=%v err=%v", replay, err)
	}
}

func TestMeshProposalReplayAckRefusesExecutingReceipt(t *testing.T) {
	dir := t.TempDir()
	m, e := testMeshMemoryRuntime(t, dir, "sovereign-ack-executing", "sovereign")
	f := proposalReplayFrame("1")
	ev := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f.Vars)
	_, entry, _, replay, err := prepareMeshProposalReplay(e, f, ev)
	if err != nil || replay {
		t.Fatalf("prepare failed replay=%v err=%v", replay, err)
	}

	ack := proposalReplayRequest(1, "digest-a")
	ack.Op = "shared_proposal_ack"
	if res := m.handleAuthority(ack); res.OK || !strings.Contains(res.Error, "not completed") {
		t.Fatalf("executing receipt must reject ack: %+v", res)
	}
	if recovered, _, found, err := loadMeshProposalReplayEntry(e, entry.RequestID); err != nil {
		t.Fatal(err)
	} else if !found || recovered.State != meshProposalReplayExecuting {
		t.Fatalf("executing fence was removed by ack: found=%v entry=%+v", found, recovered)
	}
}

func TestMeshProposalReplayLedgerBackpressurePreservesUnacknowledgedDoneResult(t *testing.T) {
	t.Setenv("MEMORYAI_MESH_REPLAY_MAX_ENTRIES", "1")
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "sovereign-unacked", "sovereign")
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
	_, _, _, replay, err = prepareMeshProposalReplay(e, f2, ev2)
	if replay || err == nil || !strings.Contains(err.Error(), "ledger full") {
		t.Fatalf("unacknowledged result must force bounded backpressure: replay=%v err=%v", replay, err)
	}

	replayed := proposalReplayFrame("1")
	_, recovered, _, replay, err := prepareMeshProposalReplay(e, replayed, ev1)
	if err != nil || !replay || recovered.State != meshProposalReplayDone {
		t.Fatalf("unacknowledged completed result stopped replaying: replay=%v entry=%+v err=%v", replay, recovered, err)
	}
	if err := applyMeshProposalReplayResult(replayed, recovered); err != nil {
		t.Fatal(err)
	}
	if replayed.Vars["mesh_decision"] != "approved-v1" {
		t.Fatalf("unacknowledged replay result changed: %#v", replayed.Vars)
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
