package main

import (
	"path/filepath"
	"testing"
)

func newMemoryGrowthRuntimeTestEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "core", []*Memory{{
		ID: "seed", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"value": "seed"}, Revision: 1,
	}})
	e, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	return e, path
}

func seedValidatedRecombinationParents(t *testing.T, e *Engine) {
	t.Helper()
	experiences := NewMemoryExperienceLedger()
	leftSource, err := experiences.Record(MemoryExperience{
		ID:             "experience-parent-a",
		Context:        map[string]string{"state": "ready"},
		Observation:    map[string]string{"input": "x"},
		Action:         map[string]string{"operation": "parent-a"},
		Outcome:        map[string]string{"result": "left"},
		PredictionHash: "prediction-parent-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	rightSource, err := experiences.Record(MemoryExperience{
		ID:             "experience-parent-b",
		Context:        map[string]string{"state": "ready"},
		Observation:    map[string]string{"input": "x"},
		Action:         map[string]string{"operation": "parent-b"},
		Outcome:        map[string]string{"result": "right"},
		PredictionHash: "prediction-parent-b",
	})
	if err != nil {
		t.Fatal(err)
	}

	formation := NewMemoryStructureFormation()
	validation := NewMemoryStructureValidationLedger()
	left := &MemoryStructure{
		ID:                  "memory-structure-a",
		CandidateID:         "candidate-parent-a",
		PatternHash:         memoryStructurePatternHash(*leftSource),
		SourceExperienceIDs: []string{leftSource.ID},
		Context:             cloneStringMap(leftSource.Context),
		Observation:         cloneStringMap(leftSource.Observation),
		Action:              cloneStringMap(leftSource.Action),
		ExpectedOutcome:     cloneStringMap(leftSource.Outcome),
		PredictionHash:      leftSource.PredictionHash,
		State:               memoryStructureValidatedState,
		ValidationCount:     1,
		SuccessCount:        1,
		Program:             []Op{{Code: "set", A: "result", B: "left"}, {Code: "halt"}},
	}
	right := &MemoryStructure{
		ID:                  "memory-structure-b",
		CandidateID:         "candidate-parent-b",
		PatternHash:         memoryStructurePatternHash(*rightSource),
		SourceExperienceIDs: []string{rightSource.ID},
		Context:             cloneStringMap(rightSource.Context),
		Observation:         cloneStringMap(rightSource.Observation),
		Action:              cloneStringMap(rightSource.Action),
		ExpectedOutcome:     cloneStringMap(rightSource.Outcome),
		PredictionHash:      rightSource.PredictionHash,
		State:               memoryStructureValidatedState,
		ValidationCount:     1,
		SuccessCount:        1,
		Program:             []Op{{Code: "set", A: "result", B: "right"}, {Code: "halt"}},
	}
	validation.structures[left.CandidateID] = cloneMemoryStructure(left)
	validation.validated[left.CandidateID] = cloneMemoryStructure(left)
	validation.structures[right.CandidateID] = cloneMemoryStructure(right)
	validation.validated[right.CandidateID] = cloneMemoryStructure(right)
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
}

func TestAutonomousMemoryGrowthCycleMaterializesValidatesAndSurvivesRestart(t *testing.T) {
	e, path := newMemoryGrowthRuntimeTestEngine(t)
	seedValidatedRecombinationParents(t, e)

	first, err := RunAutonomousMemoryGrowthCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if first.CandidateID == "" || first.WitnessExperienceID == "" || !first.Persisted {
		e.close()
		t.Fatalf("first cycle did not materialize a real candidate: %#v", first)
	}
	if first.PromotedStructureID != "" {
		e.close()
		t.Fatalf("candidate was promoted without an independent witness: %#v", first)
	}
	candidateID := first.CandidateID

	second, err := RunAutonomousMemoryGrowthCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if second.CandidateID != candidateID || !second.ValidationSuccess || second.PromotedStructureID == "" || !second.Persisted {
		e.close()
		t.Fatalf("second cycle did not validate/promote recombination: %#v", second)
	}
	if _, executable, err := e.resolveLocalFabricMemory(second.PromotedStructureID); err != nil || executable == nil || len(executable.Program) != 2 {
		e.close()
		t.Fatalf("promoted structure was not installed in the existing Fabric: memory=%#v err=%v", executable, err)
	}
	e.close()

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	_, formation, validation, err := LoadMemoryGrowthState(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	candidate, ok := formation.Get(candidateID)
	if !ok || len(candidate.ParentStructureIDs) != 2 || len(candidate.Program) != 2 {
		t.Fatalf("materialized recombination candidate lost across restart: %#v", candidate)
	}
	structure, ok := validation.GetValidated(candidateID)
	if !ok {
		t.Fatalf("recombined structure lost candidate-key mapping across restart: %s", candidateID)
	}
	if structure.CandidateID != candidateID || len(structure.ParentStructureIDs) != 2 || len(structure.Program) != 2 {
		t.Fatalf("recombined structure lost lineage/program across restart: %#v", structure)
	}
}

func TestAutonomousMemoryGrowthCyclePreservesFailedRealityWitness(t *testing.T) {
	e, path := newMemoryGrowthRuntimeTestEngine(t)

	experiences := NewMemoryExperienceLedger()
	source, err := experiences.Record(MemoryExperience{
		ID:          "experience-recombination-source",
		Context:     map[string]string{"state": "ready"},
		Observation: map[string]string{"memory_growth_kind": memoryGrowthKindRecombination, "memory_growth_pair": "a=>b", "memory_growth_candidate_id": "recombination-candidate"},
		Action:      map[string]string{"memory_growth_operation": memoryGrowthProbeOperation},
		Outcome:     map[string]string{"result": "expected"},
	})
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	candidate := &MemoryStructureCandidate{
		ID:                  "recombination-candidate",
		PatternHash:         memoryStructurePatternHash(*source),
		SourceExperienceIDs: []string{source.ID},
		State:               memoryStructureCandidateState,
		ParentStructureIDs:  []string{"a", "b"},
		Program:             []Op{{Code: "set", A: "result", B: "actual"}, {Code: "halt"}},
	}
	formation := NewMemoryStructureFormation()
	if err := formation.AdoptCandidate(candidate); err != nil {
		e.close()
		t.Fatal(err)
	}
	validation := NewMemoryStructureValidationLedger()
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		e.close()
		t.Fatal(err)
	}

	result, err := RunAutonomousMemoryGrowthCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if result.ValidationSuccess || result.PromotedStructureID != "" || !result.RealityRejected {
		e.close()
		t.Fatalf("mismatching reality witness was not rejected correctly: %#v", result)
	}
	loadedExperiences, loadedFormation, loadedValidation, err := LoadMemoryGrowthState(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	history := loadedValidation.ValidationHistory(candidate.ID)
	if len(history) != 1 || history[0].Success {
		e.close()
		t.Fatalf("failed witness was not retained in validation history: %#v", history)
	}
	witness, ok := loadedExperiences.Get(result.WitnessExperienceID)
	if !ok || witness.Outcome["result"] != "actual" {
		e.close()
		t.Fatalf("real failed witness Experience was not retained: %#v", witness)
	}
	rejected, ok := loadedFormation.Get(candidate.ID)
	if !ok || rejected.State != memoryStructureRealityRejectedState {
		e.close()
		t.Fatalf("failed candidate did not leave the automatic retry queue: %#v", rejected)
	}
	before := len(loadedExperiences.Snapshot())
	second, err := RunAutonomousMemoryGrowthCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if !second.Skipped {
		e.close()
		t.Fatalf("reality-rejected candidate was retried automatically: %#v", second)
	}
	afterExperiences, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if got := len(afterExperiences.Snapshot()); got != before {
		e.close()
		t.Fatalf("rejected candidate created repeated evidence: before=%d after=%d", before, got)
	}
	e.close()

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	_, reloadedFormation, _, err := LoadMemoryGrowthState(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	reloadedCandidate, ok := reloadedFormation.Get(candidate.ID)
	if !ok || reloadedCandidate.State != memoryStructureRealityRejectedState {
		t.Fatalf("reality rejection did not survive restart: %#v", reloadedCandidate)
	}
}

func TestAutonomousMemoryGrowthCycleContinuesSuccessfulLineageIntoNextGeneration(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	seedValidatedRecombinationParents(t, e)

	first, err := RunAutonomousMemoryGrowthCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if first.CandidateID == "" {
		t.Fatalf("first generation candidate missing: %#v", first)
	}
	second, err := RunAutonomousMemoryGrowthCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if !second.ValidationSuccess || second.PromotedStructureID == "" {
		t.Fatalf("first generation did not promote: %#v", second)
	}
	firstGenerationID := second.PromotedStructureID

	third, err := RunAutonomousMemoryGrowthCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if third.CandidateID == "" || third.PromotedStructureID != "" {
		t.Fatalf("second generation was not materialized as a fresh candidate: %#v", third)
	}
	_, formation, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	secondGenerationCandidate, ok := formation.Get(third.CandidateID)
	if !ok {
		t.Fatalf("second generation candidate missing: %s", third.CandidateID)
	}
	if !containsString(secondGenerationCandidate.ParentStructureIDs, firstGenerationID) {
		t.Fatalf("successful child was not used as the next growth frontier: parents=%v frontier=%s", secondGenerationCandidate.ParentStructureIDs, firstGenerationID)
	}

	fourth, err := RunAutonomousMemoryGrowthCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.CandidateID != third.CandidateID || !fourth.ValidationSuccess || fourth.PromotedStructureID == "" {
		t.Fatalf("second generation did not receive independent validation: %#v", fourth)
	}
	_, _, validation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	secondGeneration, ok := validation.GetValidated(third.CandidateID)
	if !ok || !containsString(secondGeneration.ParentStructureIDs, firstGenerationID) {
		t.Fatalf("multi-generation lineage was not retained: %#v", secondGeneration)
	}
}
