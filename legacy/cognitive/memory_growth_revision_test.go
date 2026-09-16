package main

import "testing"

func TestReconcileValidatedStructureFromExecutionCreatesValidatedRevision(t *testing.T) {
	experiences := NewMemoryExperienceLedger()
	formation := NewMemoryStructureFormation()
	validation := NewMemoryStructureValidationLedger()

	original, err := experiences.Record(MemoryExperience{
		Context:        map[string]string{"state": "x"},
		Observation:    map[string]string{"input": "1"},
		Action:         map[string]string{"op": "inc"},
		Outcome:        map[string]string{"result": "2"},
		PredictionHash: "prediction-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	originalWitness, err := experiences.Record(MemoryExperience{
		Context:        map[string]string{"state": "x"},
		Observation:    map[string]string{"input": "1"},
		Action:         map[string]string{"op": "inc"},
		Outcome:        map[string]string{"result": "2"},
		PredictionHash: "prediction-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidateList, err := formation.FormCandidates(experiences.Snapshot(), 2)
	if err != nil || len(candidateList) != 1 {
		t.Fatalf("unexpected original candidates: %v len=%d", err, len(candidateList))
	}
	_, current, err := validation.ValidateCandidate(candidateList[0], original, originalWitness, 1)
	if err != nil || current == nil {
		t.Fatalf("original validation failed: %v", err)
	}
	current.Program = []Op{{Code: "set", Args: []string{"result", "2"}}}
	if current.State != memoryStructureValidatedState {
		t.Fatal("expected validated current structure")
	}

	// 两次真实反馈都记录了同一个修正事实，足以形成新的候选模式。
	for i := 0; i < 2; i++ {
		if _, err := experiences.Record(MemoryExperience{
			Context:         map[string]string{"state": "x"},
			Observation:     map[string]string{"input": "1", "memory_structure_id": current.ID},
			Action:          map[string]string{"op": "inc", "memory_structure_id": current.ID},
			Outcome:         map[string]string{"result": "3"},
			PredictionHash:  "prediction-v1",
			PredictionError: map[string]string{"result": "expected=2;actual=3"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	revised, err := ReconcileValidatedStructureFromExecution(experiences, formation, validation, current, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if revised == nil {
		t.Fatal("expected a revised structure")
	}
	if revised.State != memoryStructureValidatedState {
		t.Fatalf("expected revised structure to be validated, got %q", revised.State)
	}
	if revised.ExpectedOutcome["result"] != "3" {
		t.Fatalf("expected corrected outcome, got %#v", revised.ExpectedOutcome)
	}
	if len(revised.Program) != 1 || revised.Program[0].Code != "set" {
		t.Fatalf("expected original executable program to be preserved, got %#v", revised.Program)
	}
	if _, ok := validation.GetValidated(candidateList[0].ID); ok {
		t.Fatal("expected original structure to be superseded")
	}
}

func TestReconcileValidatedStructureFromExecutionDoesNotMutateWithoutRepeatedFeedback(t *testing.T) {
	experiences := NewMemoryExperienceLedger()
	formation := NewMemoryStructureFormation()
	validation := NewMemoryStructureValidationLedger()

	first, err := experiences.Record(MemoryExperience{
		Observation:     map[string]string{"memory_structure_id": "s1", "input": "1"},
		Action:          map[string]string{"op": "inc"},
		Outcome:         map[string]string{"result": "3"},
		PredictionHash:  "p1",
		PredictionError: map[string]string{"result": "expected=2;actual=3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	current := &MemoryStructure{
		ID:          "s1",
		PatternHash: "old-pattern",
		State:       memoryStructureValidatedState,
		Program:     []Op{{Code: "set", Args: []string{"result", "2"}}},
	}
	if first == nil {
		t.Fatal("expected feedback experience")
	}

	revised, err := ReconcileValidatedStructureFromExecution(experiences, formation, validation, current, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if revised != nil {
		t.Fatal("expected no revision from a single feedback witness")
	}
}
