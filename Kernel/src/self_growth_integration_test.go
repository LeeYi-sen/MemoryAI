package main

import (
	"os"
	"path/filepath"
	"testing"
)

func loadCurrentBodyForGrowthTest(t *testing.T) *Engine {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "data", "Memory.mem"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Memory.mem")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	e, err := loadEngineWithMutationJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.close)
	return e
}

func hasString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestMemoryOwnedRecombineAndFission(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)

	a, err := e.resolve("evolution.mutate.parent")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.resolve("evolution.feedback.parent")
	if err != nil {
		t.Fatal(err)
	}
	f := newFrame()
	f.Vars["parent_a"] = a.ID
	f.Vars["parent_b"] = b.ID
	if err := e.fireEvent("structure.recombine.request", "", f); err != nil {
		t.Fatal(err)
	}
	recombinedID := f.Vars["recombined_id"]
	if recombinedID == "" {
		t.Fatal("recombination produced no child")
	}
	child, err := e.resolve(recombinedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(child.Program) != len(a.Program)+1 {
		t.Fatalf("recombination did not splice a program fragment: parent=%d child=%d", len(a.Program), len(child.Program))
	}
	a, _ = e.resolve(a.ID)
	b, _ = e.resolve(b.ID)
	if !hasString(a.MutationVariants, recombinedID) || !hasString(b.MutationVariants, recombinedID) {
		t.Fatalf("recombined child missing from parent variant lineage: a=%v b=%v child=%s", a.MutationVariants, b.MutationVariants, recombinedID)
	}

	ff := newFrame()
	ff.Vars["parent_id"] = b.ID
	if err := e.fireEvent("structure.fission.request", "", ff); err != nil {
		t.Fatal(err)
	}
	leftID, rightID := ff.Vars["fission_left_id"], ff.Vars["fission_right_id"]
	if leftID == "" || rightID == "" || leftID == rightID {
		t.Fatalf("fission children invalid: left=%q right=%q", leftID, rightID)
	}
	left, err := e.resolve(leftID)
	if err != nil {
		t.Fatal(err)
	}
	right, err := e.resolve(rightID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left.Program) != len(b.Program)-1 || len(right.Program) != len(b.Program)-1 {
		t.Fatalf("fission did not derive two partial executable variants: parent=%d left=%d right=%d", len(b.Program), len(left.Program), len(right.Program))
	}
	b, _ = e.resolve(b.ID)
	if !hasString(b.MutationVariants, leftID) || !hasString(b.MutationVariants, rightID) {
		t.Fatalf("fission lineage missing: %v", b.MutationVariants)
	}
}

func TestMemoryOwnedExecutionOutcomeHistory(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	targetID := "evolution.mutate.parent"

	success := newFrame()
	success.Vars["__event.structure_id"] = targetID
	success.Vars["__event.gain"] = "2"
	success.Vars["__event.cost"] = "1"
	if err := globalTxnScheduler.run(e, "execution.outcome.record.parent", success); err != nil {
		t.Fatal(err)
	}
	target, err := e.resolve(targetID)
	if err != nil {
		t.Fatal(err)
	}
	if len(target.SuccessHistory) != 1 || len(target.FailureHistory) != 0 {
		t.Fatalf("success history not Memory-owned/durable: success=%v failure=%v", target.SuccessHistory, target.FailureHistory)
	}
	rec, err := e.resolve(target.SuccessHistory[0])
	if err != nil {
		t.Fatal(err)
	}
	if rec.State["outcome"] != "success" {
		t.Fatalf("success record mismatch: %#v", rec.State)
	}

	failure := newFrame()
	failure.Vars["__event.structure_id"] = targetID
	failure.Vars["__event.gain"] = "0"
	failure.Vars["__event.cost"] = "1"
	if err := globalTxnScheduler.run(e, "execution.outcome.record.parent", failure); err != nil {
		t.Fatal(err)
	}
	target, _ = e.resolve(targetID)
	if len(target.SuccessHistory) != 1 || len(target.FailureHistory) != 1 {
		t.Fatalf("failure history not recorded: success=%v failure=%v", target.SuccessHistory, target.FailureHistory)
	}
	rec, err = e.resolve(target.FailureHistory[0])
	if err != nil {
		t.Fatal(err)
	}
	if rec.State["outcome"] != "failure" {
		t.Fatalf("failure record mismatch: %#v", rec.State)
	}
}

func TestMemoryOwnedBodyFusionFlow(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	source := filepath.Join(t.TempDir(), "fusion-source.mem")
	unique := &Memory{ID: "fusion.unique", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeBodyForPersistenceTest(t, source, "storage", []*Memory{unique})

	f := newFrame()
	f.Vars["source_path"] = source
	f.Vars["target_path"] = "primary"
	if err := e.fireEvent("memory.fusion.request", "", f); err != nil {
		t.Fatal(err)
	}
	candidateID := f.Vars["fusion_candidate_id"]
	if candidateID == "" {
		t.Fatal("fusion discovery produced no candidate")
	}
	candidate, err := e.resolve(candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.State["status"] != "fused" {
		t.Fatalf("fusion chain did not reach execute stage: %#v", candidate.State)
	}
	if _, err := e.resolveIDLocal(unique.ID); err != nil {
		t.Fatalf("fusion did not copy unique Memory into target: %v", err)
	}
	for _, mounted := range e.mountedSpacePaths() {
		if mounted == source {
			t.Fatalf("fusion source remained mounted after execution: %v", e.mountedSpacePaths())
		}
	}
}
