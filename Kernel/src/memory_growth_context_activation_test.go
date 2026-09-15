package main

import (
	"strings"
	"testing"
)

func fullyValidateRootContextSplit(t *testing.T, e *Engine) (*MemoryStructure, *MemoryContextSplitState, map[string]*MemoryStructure) {
	t.Helper()
	parent, experiences, formation, validation := seedContextBranchParent(t, e)
	recordContextReality(t, e, experiences, parent, "activate-p-1", "P", "expand")
	recordContextReality(t, e, experiences, parent, "activate-p-2", "P", "expand")
	recordContextReality(t, e, experiences, parent, "activate-q-1", "Q", "stable")
	recordContextReality(t, e, experiences, parent, "activate-q-2", "Q", "stable")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	formed, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if formed.SplitID == "" {
		t.Fatal("root context split was not formed")
	}

	experiences, formation, validation, err = LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	recordContextReality(t, e, experiences, parent, "activate-p-3", "P", "expand")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	if _, err := RunAutonomousContextBranchingCycle(e); err != nil {
		t.Fatal(err)
	}

	experiences, formation, validation, err = LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	recordContextReality(t, e, experiences, parent, "activate-q-3", "Q", "stable")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	completed, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.ParentRetired {
		t.Fatalf("root context split did not retire parent: %#v", completed)
	}

	split, _, err := loadMemoryContextSplit(e, formed.SplitID)
	if err != nil {
		t.Fatal(err)
	}
	if split == nil || split.Status != memoryContextSplitValidated {
		t.Fatalf("root split is not validated: %#v", split)
	}
	_, _, validation, err = LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	children := map[string]*MemoryStructure{}
	for _, branch := range split.Branches {
		child, ok := validation.GetValidated(branch.CandidateID)
		if !ok {
			t.Fatalf("validated branch missing: %s", branch.CandidateID)
		}
		children[branch.ContextValue] = child
	}
	return parent, split, children
}

func TestContextActivationSelectsExactValidatedBranchAndSurvivesRestart(t *testing.T) {
	e, path := newMemoryGrowthRuntimeTestEngine(t)
	parent, _, children := fullyValidateRootContextSplit(t, e)

	resolution, err := ResolveContextualExecutionTarget(e, parent.ID, map[string]string{"structure": "P", "color": "red"})
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if !resolution.Contextual || resolution.ResolvedID != children["P"].ID || resolution.Conditions["structure"] != "P" || resolution.ActivationFactID == "" {
		e.close()
		t.Fatalf("wrong contextual resolution: %#v", resolution)
	}
	if value, ok, err := contextualExecutableCondition(e, resolution.ResolvedID, "structure"); err != nil || !ok || value != "P" {
		e.close()
		t.Fatalf("context condition was not materialized into executable Memory: value=%q ok=%v err=%v", value, ok, err)
	}
	factID := resolution.ActivationFactID
	e.close()

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	fact, err := loadMemoryContextActivationFact(reloaded, factID)
	if err != nil {
		t.Fatal(err)
	}
	if fact.Status != memoryContextActivationMatched || fact.ResolvedStructureID != children["P"].ID || fact.MatchedConditions["structure"] != "P" {
		t.Fatalf("activation fact did not survive restart: %#v", fact)
	}
	if value, ok, err := contextualExecutableCondition(reloaded, children["P"].ID, "structure"); err != nil || !ok || value != "P" {
		t.Fatalf("materialized executable condition did not survive restart: value=%q ok=%v err=%v", value, ok, err)
	}
}

func TestContextActivationRefusesMissingUnknownAndDirectLeafMismatch(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	parent, _, children := fullyValidateRootContextSplit(t, e)

	missing, err := ResolveContextualExecutionTarget(e, parent.ID, map[string]string{"color": "red"})
	if err == nil || missing == nil || missing.ActivationFactID == "" || !strings.Contains(err.Error(), "missing required context key") {
		t.Fatalf("missing Context was not rejected with durable fact: resolution=%#v err=%v", missing, err)
	}
	fact, err := loadMemoryContextActivationFact(e, missing.ActivationFactID)
	if err != nil || fact.Status != memoryContextActivationMissing {
		t.Fatalf("missing-context fact unavailable: fact=%#v err=%v", fact, err)
	}

	unknown, err := ResolveContextualExecutionTarget(e, parent.ID, map[string]string{"structure": "Z"})
	if err == nil || unknown == nil || unknown.ActivationFactID == "" || !strings.Contains(err.Error(), "no validated context branch") {
		t.Fatalf("unknown Context was not rejected: resolution=%#v err=%v", unknown, err)
	}

	direct, err := ResolveContextualExecutionTarget(e, children["P"].ID, map[string]string{"structure": "Q"})
	if err == nil || direct == nil || direct.ActivationFactID == "" || !strings.Contains(err.Error(), "context mismatch") {
		t.Fatalf("direct contextual leaf bypassed its condition: resolution=%#v err=%v", direct, err)
	}
}

func recordNestedContextReality(t *testing.T, e *Engine, experiences *experienceLedger, structure *MemoryStructure, id, temperature, result string) {
	t.Helper()
	actual := map[string]string{"result": result}
	experience, err := experiences.Record(MemoryExperience{
		ID:      id,
		Context: map[string]string{"structure": "P", "color": "red", "temperature": temperature},
		Observation: map[string]string{
			"stimulus":            "heat",
			"memory_structure_id": structure.ID,
		},
		Action:          cloneStringMap(structure.Action),
		Outcome:         actual,
		PredictionHash:  structure.PredictionHash,
		PredictionError: calculatePredictionError(structure.ExpectedOutcome, actual),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateMemoryBeliefsFromExperience(e, structure, experience); err != nil {
		t.Fatal(err)
	}
}
