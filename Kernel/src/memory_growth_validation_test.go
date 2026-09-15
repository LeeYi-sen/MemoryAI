package main

import "testing"

func testMemoryStructureExperience(id string, outcome string) *MemoryExperience {
	return &MemoryExperience{
		ID: id,
		Context: map[string]string{"state": "ready"},
		Observation: map[string]string{"input": "x"},
		Action: map[string]string{"operation": "act"},
		Outcome: map[string]string{"result": outcome},
		PredictionHash: "prediction-1",
	}
}

func testCandidateForValidation(t *testing.T) (*MemoryStructureCandidate, *MemoryExperience) {
	t.Helper()
	a := testMemoryStructureExperience("source-a", "ok")
	b := testMemoryStructureExperience("source-b", "ok")
	formation := NewMemoryStructureFormation()
	candidates, err := formation.FormCandidates([]*MemoryExperience{a, b}, 2)
	if err != nil {
		t.Fatalf("FormCandidates() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	return candidates[0], a
}

func TestMemoryStructureValidationRequiresIndependentEvidence(t *testing.T) {
	candidate, source := testCandidateForValidation(t)
	ledger := NewMemoryStructureValidationLedger()

	if _, _, err := ledger.ValidateCandidate(candidate, source, source, 2); err == nil {
		t.Fatal("expected source experience to be rejected as validation evidence")
	}
}

func TestMemoryStructureValidationPromotesAfterIndependentSuccesses(t *testing.T) {
	candidate, source := testCandidateForValidation(t)
	ledger := NewMemoryStructureValidationLedger()
	failure := testMemoryStructureExperience("witness-failure", "different")
	success1 := testMemoryStructureExperience("witness-success-1", "ok")
	success2 := testMemoryStructureExperience("witness-success-2", "ok")

	validation, structure, err := ledger.ValidateCandidate(candidate, source, failure, 2)
	if err != nil {
		t.Fatalf("failure validation error = %v", err)
	}
	if validation.Success {
		t.Fatal("failure witness was marked successful")
	}
	if structure != nil {
		t.Fatal("structure promoted before success threshold")
	}

	validation, structure, err = ledger.ValidateCandidate(candidate, source, success1, 2)
	if err != nil {
		t.Fatalf("first success validation error = %v", err)
	}
	if !validation.Success || structure != nil {
		t.Fatal("structure promoted after only one independent success")
	}

	validation, structure, err = ledger.ValidateCandidate(candidate, source, success2, 2)
	if err != nil {
		t.Fatalf("second success validation error = %v", err)
	}
	if !validation.Success || structure == nil {
		t.Fatal("structure was not promoted after success threshold")
	}
	if structure.State != memoryStructureValidatedState {
		t.Fatalf("structure state = %q, want %q", structure.State, memoryStructureValidatedState)
	}
	if structure.SuccessCount != 2 || structure.FailureCount != 1 || structure.ValidationCount != 3 {
		t.Fatalf("validation counts = (%d,%d,%d), want (2,1,3)", structure.SuccessCount, structure.FailureCount, structure.ValidationCount)
	}
	if len(structure.SourceExperienceIDs) != 2 {
		t.Fatalf("source provenance count = %d, want 2", len(structure.SourceExperienceIDs))
	}
	if len(structure.ValidationExperienceIDs) != 2 {
		t.Fatalf("validation provenance count = %d, want 2", len(structure.ValidationExperienceIDs))
	}
	if structure.Action["operation"] != "act" || structure.ExpectedOutcome["result"] != "ok" {
		t.Fatal("promoted structure lost executable/expected factual fields")
	}
}

func TestMemoryStructureValidationRejectsDuplicateWitness(t *testing.T) {
	candidate, source := testCandidateForValidation(t)
	ledger := NewMemoryStructureValidationLedger()
	witness := testMemoryStructureExperience("witness", "ok")

	if _, _, err := ledger.ValidateCandidate(candidate, source, witness, 2); err != nil {
		t.Fatalf("first validation error = %v", err)
	}
	if _, _, err := ledger.ValidateCandidate(candidate, source, witness, 2); err == nil {
		t.Fatal("expected duplicate witness to be rejected")
	}
}

func TestMemoryStructureValidationHistoryPreservesFailures(t *testing.T) {
	candidate, source := testCandidateForValidation(t)
	ledger := NewMemoryStructureValidationLedger()
	failure := testMemoryStructureExperience("witness-failure", "different")

	if _, _, err := ledger.ValidateCandidate(candidate, source, failure, 1); err != nil {
		t.Fatalf("validation error = %v", err)
	}
	history := ledger.ValidationHistory(candidate.ID)
	if len(history) != 1 || history[0].Success {
		t.Fatalf("history = %#v, want one failed validation", history)
	}
	if _, ok := ledger.GetValidated(candidate.ID); ok {
		t.Fatal("failed validation must not promote structure")
	}
}
