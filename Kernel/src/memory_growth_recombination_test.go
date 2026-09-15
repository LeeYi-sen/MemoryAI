package main

import "testing"

func TestRecombineValidatedStructuresDeterministicallyMutatesProgram(t *testing.T) {
	left := &MemoryStructure{ID: "parent-a", PatternHash: "pattern-a", State: memoryStructureValidatedState, SourceExperienceIDs: []string{"a1", "a2"}, Program: []Op{{Code: "set", A: "x", B: "left"}, {Code: "halt"}}}
	right := &MemoryStructure{ID: "parent-b", PatternHash: "pattern-b", State: memoryStructureValidatedState, SourceExperienceIDs: []string{"b1", "b2"}, Program: []Op{{Code: "set", A: "x", B: "right"}, {Code: "halt"}}}
	candidate, err := RecombineValidatedStructures(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.State != memoryStructureRecombinationCandidateState {
		t.Fatalf("state=%q", candidate.State)
	}
	if len(candidate.ParentStructureIDs) != 2 || candidate.ParentStructureIDs[0] != "parent-a" || candidate.ParentStructureIDs[1] != "parent-b" {
		t.Fatalf("parents=%v", candidate.ParentStructureIDs)
	}
	if len(candidate.SourceExperienceIDs) != 4 {
		t.Fatalf("sources=%v", candidate.SourceExperienceIDs)
	}
	if candidate.MutationIndex != 0 {
		t.Fatalf("mutation index=%d, want 0", candidate.MutationIndex)
	}
	if candidate.Program[0].B != "right" {
		t.Fatalf("expected right parent's operation, got=%#v", candidate.Program[0])
	}
	if candidate.Program[1].Code != "halt" {
		t.Fatalf("terminal operation was unexpectedly replaced: %#v", candidate.Program)
	}
}

func TestMaterializeRecombinationCandidateUsesRealExperiencePattern(t *testing.T) {
	candidate := &MemoryStructureCandidate{ID: "recombination-candidate", PatternHash: "pre-execution", State: memoryStructureRecombinationCandidateState, ParentStructureIDs: []string{"a", "b"}, Program: []Op{{Code: "set", A: "x", B: "y"}, {Code: "halt"}}, MutationIndex: 0}
	experience := &MemoryExperience{ID: "execution-1", Observation: map[string]string{"input": "1"}, Action: map[string]string{"op": "recombined"}, Outcome: map[string]string{"result": "ok"}, PredictionHash: "p1"}
	materialized, err := MaterializeRecombinationCandidate(candidate, experience)
	if err != nil {
		t.Fatal(err)
	}
	if materialized.State != memoryStructureCandidateState {
		t.Fatalf("state=%q", materialized.State)
	}
	if materialized.PatternHash != memoryStructurePatternHash(*experience) {
		t.Fatal("materialized candidate did not adopt the real execution fact pattern")
	}
	if !containsString(materialized.SourceExperienceIDs, "execution-1") {
		t.Fatalf("execution experience missing from provenance: %v", materialized.SourceExperienceIDs)
	}
	if len(materialized.Program) != len(candidate.Program) || materialized.Program[0].B != "y" {
		t.Fatal("execution materialization changed candidate program")
	}
}

func TestFinalizeRecombinedStructurePreservesProgramAndLineage(t *testing.T) {
	candidate := &MemoryStructureCandidate{ID: "candidate", PatternHash: "pattern", State: memoryStructureCandidateState, ParentStructureIDs: []string{"a", "b"}, Program: []Op{{Code: "set", A: "x", B: "y"}, {Code: "halt"}}, MutationIndex: 1}
	structure := &MemoryStructure{ID: "structure", PatternHash: "pattern", State: memoryStructureValidatedState}
	finalized, err := FinalizeRecombinedStructure(structure, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.CandidateID != candidate.ID {
		t.Fatalf("candidate id=%q", finalized.CandidateID)
	}
	if len(finalized.ParentStructureIDs) != 2 {
		t.Fatalf("parents=%v", finalized.ParentStructureIDs)
	}
	if finalized.MutationIndex != 1 {
		t.Fatalf("mutation index=%d", finalized.MutationIndex)
	}
	if len(finalized.Program) != 2 || finalized.Program[0].B != "y" || finalized.Program[1].Code != "halt" {
		t.Fatalf("program=%#v", finalized.Program)
	}
}

func TestRecombineValidatedStructuresRejectsUnvalidatedParent(t *testing.T) {
	left := &MemoryStructure{ID: "parent-a", State: memoryStructureValidatedState, Program: []Op{{Code: "set", A: "x", B: "left"}, {Code: "halt"}}}
	right := &MemoryStructure{ID: "parent-b", State: memoryStructureCandidateState, Program: []Op{{Code: "set", A: "x", B: "right"}, {Code: "halt"}}}
	if _, err := RecombineValidatedStructures(left, right); err == nil {
		t.Fatal("unvalidated parent was accepted")
	}
}
