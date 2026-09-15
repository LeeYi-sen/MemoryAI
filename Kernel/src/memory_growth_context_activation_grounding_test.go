package main

import (
	"testing"
)

func TestContextActivationCreatesFreshGroundedIdentityForValidatedChildOnly(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	parent, _, children := fullyValidateRootContextSplit(t, e)
	child := children["P"]

	_, formation, validation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	experiences, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	parentStored := findHistoricalStructureByID(validation, parent.ID)
	childStored := findValidatedStructureByID(validation, child.ID)
	if parentStored == nil || childStored == nil {
		t.Fatal("context lineage unavailable")
	}
	parentStored.Action = map[string]string{
		groundedAutonomousActionKey: "true",
		groundedAdapterActionKey:    "adapter-test",
		groundedActionIDKey:         "parent-action-id",
	}
	childStored.Action = cloneStringMap(parentStored.Action)
	validation.mu.Lock()
	validation.structures[parentStored.CandidateID] = cloneMemoryStructure(parentStored)
	validation.structures[childStored.CandidateID] = cloneMemoryStructure(childStored)
	validation.validated[childStored.CandidateID] = cloneMemoryStructure(childStored)
	validation.mu.Unlock()
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}

	resolution, err := ResolveContextualExecutionTarget(e, child.ID, map[string]string{"structure": "P"})
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.GroundedActionFresh || resolution.Structure.Action[groundedActionIDKey] == "parent-action-id" {
		t.Fatalf("context child did not receive fresh physical action identity: %#v", resolution)
	}
	if resolution.Structure.Action[groundedParentActionIDKey] != "parent-action-id" {
		t.Fatalf("parent action provenance missing: %#v", resolution.Structure.Action)
	}
	_, _, reloadedValidation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	persisted := findValidatedStructureByID(reloadedValidation, child.ID)
	if persisted == nil || persisted.Action[groundedActionIDKey] != resolution.Structure.Action[groundedActionIDKey] {
		t.Fatalf("fresh grounded identity was not persisted in Memory Growth state: %#v", persisted)
	}
}

func TestContextualRunFeedsRealityBackIntoLeafBelief(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	parent, _, _ := fullyValidateRootContextSplit(t, e)
	liveContext := map[string]string{"structure": "P", "temperature": "warm", "color": "red"}
	resolution, err := ResolveContextualExecutionTarget(e, parent.ID, liveContext)
	if err != nil {
		t.Fatal(err)
	}
	frame := newFrame()
	for key, value := range liveContext {
		frame.Vars[key] = value
	}
	if err := globalTxnScheduler.run(e, resolution.ResolvedID, frame); err != nil {
		t.Fatal(err)
	}
	feedback, err := RecordContextualExecutionFeedback(e, resolution.Structure, liveContext, frame, resolution.ActivationFactID)
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Experience == nil || feedback.Experience.Context["temperature"] != "warm" {
		t.Fatalf("live Context was not preserved in Experience: %#v", feedback)
	}
	beliefs, err := MemoryBeliefsForStructure(e, resolution.ResolvedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(beliefs) == 0 || !containsString(beliefs[0].EvidenceExperienceIDs, feedback.Experience.ID) {
		t.Fatalf("leaf Belief did not receive contextual execution Reality: %#v", beliefs)
	}
}
