package main

import (
	"net/http"
	"path/filepath"
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

func TestMeshProposalAckGcPersistsFenceAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "sovereign-ack", "sovereign")
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
	ack := MeshRequest{
		Op: "shared_proposal_ack", ReceiptID: entry.RequestID,
		MemoryID: "shared.memory", OriginNode: "node-a", Revision: 1,
	}
	status, err := ackMeshProposalReplay(e, ack)
	if err != nil || status != "acked-gc" {
		t.Fatalf("ACK/GC failed status=%q err=%v", status, err)
	}
	if _, _, found, err := loadMeshProposalReplayEntry(e, entry.RequestID); err != nil || found {
		t.Fatalf("done receipt survived ACK GC: found=%v err=%v", found, err)
	}
	if through, err := loadMeshProposalAckedThrough(e, entry.Slot); err != nil || through != 1 {
		t.Fatalf("ACK fence not durable: through=%d err=%v", through, err)
	}
	if status, err := ackMeshProposalReplay(e, ack); err != nil || status != "acked-gc" {
		t.Fatalf("duplicate ACK was not idempotent status=%q err=%v", status, err)
	}

	e.close()
	forgetMeshProposalReplayState(e)
	restored, err := loadEngineWithMutationJournal(filepath.Join(dir, "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	if status, err := ackMeshProposalReplay(restored, ack); err != nil || status != "acked-gc" {
		t.Fatalf("restart lost ACK fence status=%q err=%v", status, err)
	}
}

func TestMeshProposalAckRefusesExecutingReceipt(t *testing.T) {
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "sovereign-executing", "sovereign")
	f := proposalReplayFrame("1")
	ev := newPhysicalEvent("mesh.shared.proposal", "shared.memory", f.Vars)
	_, entry, _, replay, err := prepareMeshProposalReplay(e, f, ev)
	if err != nil || replay {
		t.Fatalf("prepare failed replay=%v err=%v", replay, err)
	}
	ack := MeshRequest{
		Op: "shared_proposal_ack", ReceiptID: entry.RequestID,
		MemoryID: "shared.memory", OriginNode: "node-a", Revision: 1,
	}
	if _, err := ackMeshProposalReplay(e, ack); err == nil {
		t.Fatal("executing receipt was GC eligible")
	}
	got, _, found, err := loadMeshProposalReplayEntry(e, entry.RequestID)
	if err != nil || !found || got.State != meshProposalReplayExecuting {
		t.Fatalf("executing receipt was altered: found=%v entry=%+v err=%v", found, got, err)
	}
}

func TestMeshProposalAckFailsClosedWithoutAckFence(t *testing.T) {
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "sovereign-missing", "sovereign")
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
	if err := e.explicitDeleteMemory(entry.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
	ack := MeshRequest{
		Op: "shared_proposal_ack", ReceiptID: entry.RequestID,
		MemoryID: "shared.memory", OriginNode: "node-a", Revision: 1,
	}
	if _, err := ackMeshProposalReplay(e, ack); err == nil {
		t.Fatal("missing receipt without ACK fence was accepted")
	}
}

func TestMeshProposalAckRejectsFutureRevision(t *testing.T) {
	dir := t.TempDir()
	_, e := testMeshMemoryRuntime(t, dir, "sovereign-future", "sovereign")
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
	ack := MeshRequest{
		Op: "shared_proposal_ack", ReceiptID: entry.RequestID,
		MemoryID: "shared.memory", OriginNode: "node-a", Revision: 2,
	}
	if _, err := ackMeshProposalReplay(e, ack); err == nil {
		t.Fatal("future revision ACK was accepted")
	}
}

func TestSovereignProposalResponseCarriesReceiptAndAckGc(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	m := &meshRuntime{
		role: "sovereign", nodeID: "sovereign-real-ack", engine: e,
		directory: map[string]MeshNode{}, shared: map[string]MeshRecord{}, client: &http.Client{},
	}
	req := MeshRequest{
		Op: "shared_propose", MemoryID: "remote.goal", OriginNode: "node-real",
		ProposalDigest: "digest-real", Revision: 1, Tags: []string{"memory", "cog.goal"},
	}
	res := m.authorizeSharedProposal(req)
	if !res.OK || res.ReceiptID == "" {
		t.Fatalf("real Sovereign proposal response missing durable receipt: %+v", res)
	}
	ack := m.ackSharedProposal(MeshRequest{
		Op: "shared_proposal_ack", ReceiptID: res.ReceiptID,
		MemoryID: req.MemoryID, OriginNode: req.OriginNode, Revision: req.Revision,
	})
	if !ack.OK || ack.Status != "acked-gc" {
		t.Fatalf("real Sovereign ACK/GC failed: %+v", ack)
	}
	if _, _, found, err := loadMeshProposalReplayEntry(e, res.ReceiptID); err != nil || found {
		t.Fatalf("real authority receipt survived ACK GC: found=%v err=%v", found, err)
	}
}
