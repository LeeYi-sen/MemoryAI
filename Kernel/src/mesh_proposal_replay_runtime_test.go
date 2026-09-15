package main

import (
	"strings"
	"testing"
)

func replayTestEngine(t *testing.T) *Engine {
	t.Helper()
	e := &Engine{bodyPath: t.TempDir() + "/Memory.mem"}
	t.Cleanup(func() { forgetMeshProposalReplayState(e) })
	return e
}

func replayTestFrame(revision, digest string, tags ...string) *Frame {
	f := newFrame()
	f.Vars["memory_id"] = "memory.replay.target"
	f.Vars["origin_node"] = "node-a"
	f.Vars["proposal_digest"] = digest
	f.Vars["revision"] = revision
	f.Lists["tags"] = append([]string(nil), tags...)
	return f
}

func TestMeshProposalReplayExecutesOnceAndRestoresDecision(t *testing.T) {
	e := replayTestEngine(t)
	runs := 0
	first := replayTestFrame("7", "digest-a", "beta", "alpha")
	ev1 := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", first.Vars)
	if err := executeMeshProposalEventOnce(e, first, ev1, func() error {
		runs++
		first.Vars["mesh_decision"] = "approve"
		first.Vars["mesh_reason"] = "memory-owned-policy"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	requestID := first.Vars["mesh_request_id"]
	if requestID == "" || runs != 1 {
		t.Fatalf("first proposal did not execute exactly once: request=%q runs=%d", requestID, runs)
	}

	second := replayTestFrame("7", "digest-a", "alpha", "beta")
	ev2 := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", second.Vars)
	if err := executeMeshProposalEventOnce(e, second, ev2, func() error {
		runs++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("duplicate proposal re-executed Memory event: runs=%d", runs)
	}
	if second.Vars["mesh_request_id"] != requestID || second.Vars["mesh_decision"] != "approve" || second.Vars["mesh_reason"] != "memory-owned-policy" {
		t.Fatalf("replayed proposal did not restore original decision: vars=%v", second.Vars)
	}
}

func TestMeshProposalReplaySurvivesRuntimeRestart(t *testing.T) {
	dir := t.TempDir()
	e1 := &Engine{bodyPath: dir + "/Memory.mem"}
	first := replayTestFrame("3", "digest-restart", "tag")
	ev := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", first.Vars)
	runs := 0
	if err := executeMeshProposalEventOnce(e1, first, ev, func() error {
		runs++
		first.Vars["decision"] = "denied"
		first.Vars["reason"] = "test-decision"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	requestID := first.Vars["mesh_request_id"]
	forgetMeshProposalReplayState(e1)

	e2 := &Engine{bodyPath: dir + "/Memory.mem"}
	t.Cleanup(func() { forgetMeshProposalReplayState(e2) })
	second := replayTestFrame("3", "digest-restart", "tag")
	ev2 := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", second.Vars)
	if err := executeMeshProposalEventOnce(e2, second, ev2, func() error {
		runs++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || second.Vars["mesh_request_id"] != requestID || second.Vars["decision"] != "denied" || second.Vars["reason"] != "test-decision" {
		t.Fatalf("restart replay failed: runs=%d vars=%v", runs, second.Vars)
	}
}

func TestMeshProposalReplayFenceNeverReexecutesIndeterminateRequest(t *testing.T) {
	e := replayTestEngine(t)
	f := replayTestFrame("9", "digest-fenced", "tag")
	ev := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", f.Vars)
	_, slot, requestID, err := meshProposalIdentityFromFrame(f, ev)
	if err != nil {
		t.Fatal(err)
	}
	st, err := meshProposalReplayStateFor(e)
	if err != nil {
		t.Fatal(err)
	}
	entry := meshProposalReplayEntry{Slot: slot, RequestID: requestID, Revision: 9, State: "executing", UpdatedNano: 1}
	raw, err := marshalMeshProposalReplay(map[string]meshProposalReplayEntry{requestID: entry})
	if err != nil {
		t.Fatal(err)
	}
	if err := persistMeshJournalFile(st.path, raw); err != nil {
		t.Fatal(err)
	}
	forgetMeshProposalReplayState(e)

	runs := 0
	retry := replayTestFrame("9", "digest-fenced", "tag")
	ev2 := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", retry.Vars)
	err = executeMeshProposalEventOnce(e, retry, ev2, func() error {
		runs++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "durably fenced") {
		t.Fatalf("indeterminate replay was not fail-closed: err=%v", err)
	}
	if runs != 0 {
		t.Fatalf("indeterminate proposal was re-executed: runs=%d", runs)
	}
	if retry.Vars["mesh_request_id"] != requestID {
		t.Fatalf("fenced retry lost stable request id: got=%q want=%q", retry.Vars["mesh_request_id"], requestID)
	}
}

func TestMeshProposalReplayHigherRevisionSupersedesOldFence(t *testing.T) {
	e := replayTestEngine(t)
	old := replayTestFrame("4", "digest-old", "tag")
	oldEvent := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", old.Vars)
	_, slot, requestID, err := meshProposalIdentityFromFrame(old, oldEvent)
	if err != nil {
		t.Fatal(err)
	}
	st, err := meshProposalReplayStateFor(e)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := marshalMeshProposalReplay(map[string]meshProposalReplayEntry{
		requestID: {Slot: slot, RequestID: requestID, Revision: 4, State: "executing", UpdatedNano: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := persistMeshJournalFile(st.path, raw); err != nil {
		t.Fatal(err)
	}
	forgetMeshProposalReplayState(e)

	newer := replayTestFrame("5", "digest-new", "tag")
	newEvent := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", newer.Vars)
	runs := 0
	if err := executeMeshProposalEventOnce(e, newer, newEvent, func() error {
		runs++
		newer.Vars["mesh_decision"] = "approve"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || newer.Vars["mesh_decision"] != "approve" {
		t.Fatalf("higher revision failed to supersede crash-left fence: runs=%d vars=%v", runs, newer.Vars)
	}
}

func TestMeshProposalReplayRejectsConflictingSameRevision(t *testing.T) {
	e := replayTestEngine(t)
	first := replayTestFrame("11", "digest-one", "tag")
	ev1 := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", first.Vars)
	if err := executeMeshProposalEventOnce(e, first, ev1, func() error {
		first.Vars["mesh_decision"] = "approve"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	second := replayTestFrame("11", "digest-two", "tag")
	ev2 := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", second.Vars)
	runs := 0
	err := executeMeshProposalEventOnce(e, second, ev2, func() error {
		runs++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting mesh proposal identity") {
		t.Fatalf("same-revision conflict was not rejected: err=%v", err)
	}
	if runs != 0 {
		t.Fatalf("conflicting same-revision proposal executed: runs=%d", runs)
	}
}

func TestMeshProposalReplayOldReceiptSurvivesHigherRevision(t *testing.T) {
	e := replayTestEngine(t)
	runs := 0
	old := replayTestFrame("20", "digest-old", "tag")
	oldEvent := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", old.Vars)
	if err := executeMeshProposalEventOnce(e, old, oldEvent, func() error {
		runs++
		old.Vars["mesh_decision"] = "denied"
		old.Vars["mesh_reason"] = "old-result"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	oldRequestID := old.Vars["mesh_request_id"]

	newer := replayTestFrame("21", "digest-new", "tag")
	newEvent := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", newer.Vars)
	if err := executeMeshProposalEventOnce(e, newer, newEvent, func() error {
		runs++
		newer.Vars["mesh_decision"] = "approve"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Fatalf("new revision did not execute once: runs=%d", runs)
	}

	lateOld := replayTestFrame("20", "digest-old", "tag")
	lateOldEvent := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", lateOld.Vars)
	if err := executeMeshProposalEventOnce(e, lateOld, lateOldEvent, func() error {
		runs++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Fatalf("late old receipt re-executed Memory event: runs=%d", runs)
	}
	if lateOld.Vars["mesh_request_id"] != oldRequestID || lateOld.Vars["mesh_decision"] != "denied" || lateOld.Vars["mesh_reason"] != "old-result" {
		t.Fatalf("late old replay did not recover historical receipt: vars=%v", lateOld.Vars)
	}
}

func TestMeshProposalReplayPathsSeparateCoreBodies(t *testing.T) {
	dir := t.TempDir()
	a := &Engine{bodyPath: dir + "/A.mem"}
	b := &Engine{bodyPath: dir + "/B.mem"}
	pa, err := meshProposalReplayPath(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := meshProposalReplayPath(b)
	if err != nil {
		t.Fatal(err)
	}
	if pa == pb {
		t.Fatalf("distinct core bodies share replay ledger: %q", pa)
	}
}

func TestMeshProposalReplayBackpressurePreservesExistingReceipt(t *testing.T) {
	e := replayTestEngine(t)
	first := replayTestFrame("30", "digest-first", "tag")
	firstEvent := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", first.Vars)
	if err := executeMeshProposalEventOnce(e, first, firstEvent, func() error {
		first.Vars["mesh_decision"] = "approved"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := meshProposalReplayStateFor(e)
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.maxEntries = 1
	st.mu.Unlock()

	newer := replayTestFrame("31", "digest-new", "tag")
	newEvent := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", newer.Vars)
	runs := 0
	err = executeMeshProposalEventOnce(e, newer, newEvent, func() error {
		runs++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "ledger full") {
		t.Fatalf("full replay ledger did not apply backpressure: err=%v", err)
	}
	if runs != 0 {
		t.Fatalf("backpressured proposal executed: runs=%d", runs)
	}

	retry := replayTestFrame("30", "digest-first", "tag")
	retryEvent := newPhysicalEvent("mesh.shared.proposal", "memory.replay.target", retry.Vars)
	if err := executeMeshProposalEventOnce(e, retry, retryEvent, func() error {
		runs++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || retry.Vars["mesh_decision"] != "approved" {
		t.Fatalf("existing receipt was lost under backpressure: runs=%d vars=%v", runs, retry.Vars)
	}
}
