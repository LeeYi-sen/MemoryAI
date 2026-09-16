package main

import "testing"

func TestMemoryStructureFormationRequiresRecurrence(t *testing.T) {
	formation := NewMemoryStructureFormation()
	experiences := []*MemoryExperience{
		{ID: "e1", Observation: map[string]string{"state": "cold"}, Action: map[string]string{"action": "heat"}, Outcome: map[string]string{"state": "warm"}},
	}
	if _, err := formation.FormCandidates(experiences, 1); err == nil {
		t.Fatal("expected recurrence threshold rejection")
	}
	candidates, err := formation.FormCandidates(experiences, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("single experience formed candidate: %+v", candidates)
	}
}

func TestMemoryStructureFormationBuildsStableCandidate(t *testing.T) {
	formation := NewMemoryStructureFormation()
	experiences := []*MemoryExperience{
		{ID: "e2", ParentIDs: []string{"p2"}, Context: map[string]string{"room": "lab"}, Observation: map[string]string{"state": "cold"}, Action: map[string]string{"action": "heat"}, Outcome: map[string]string{"state": "warm"}},
		{ID: "e1", ParentIDs: []string{"p1"}, Context: map[string]string{"room": "lab"}, Observation: map[string]string{"state": "cold"}, Action: map[string]string{"action": "heat"}, Outcome: map[string]string{"state": "warm"}},
		{ID: "different", Context: map[string]string{"room": "lab"}, Observation: map[string]string{"state": "cold"}, Action: map[string]string{"action": "wait"}, Outcome: map[string]string{"state": "cold"}},
	}
	candidates, err := formation.FormCandidates(experiences, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%d, want 1", len(candidates))
	}
	candidate := candidates[0]
	if candidate.State != memoryStructureCandidateState {
		t.Fatalf("state=%q", candidate.State)
	}
	if len(candidate.SourceExperienceIDs) != 2 || candidate.SourceExperienceIDs[0] != "e1" || candidate.SourceExperienceIDs[1] != "e2" {
		t.Fatalf("sources=%v", candidate.SourceExperienceIDs)
	}
	if len(candidate.PatternHash) != 64 {
		t.Fatalf("pattern hash=%q", candidate.PatternHash)
	}
}

func TestMemoryStructureFormationAccumulatesNewEvidenceIdempotently(t *testing.T) {
	formation := NewMemoryStructureFormation()
	base := []*MemoryExperience{
		{ID: "e1", Observation: map[string]string{"x": "1"}, Action: map[string]string{"a": "go"}, Outcome: map[string]string{"y": "2"}},
		{ID: "e2", Observation: map[string]string{"x": "1"}, Action: map[string]string{"a": "go"}, Outcome: map[string]string{"y": "2"}},
	}
	candidates, err := formation.FormCandidates(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("initial candidates=%d", len(candidates))
	}
	id := candidates[0].ID

	more := append(base, &MemoryExperience{ID: "e3", ParentIDs: []string{"other-parent"}, Observation: map[string]string{"x": "1"}, Action: map[string]string{"a": "go"}, Outcome: map[string]string{"y": "2"}})
	candidates, err = formation.FormCandidates(more, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ID != id {
		t.Fatalf("candidate identity changed: %+v", candidates)
	}
	if len(candidates[0].SourceExperienceIDs) != 3 {
		t.Fatalf("sources=%v", candidates[0].SourceExperienceIDs)
	}

	candidates, err = formation.FormCandidates(more, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || len(candidates[0].SourceExperienceIDs) != 3 {
		t.Fatalf("repeated formation duplicated evidence: %+v", candidates)
	}
}

func TestMemoryStructureFormationPatternIgnoresLineage(t *testing.T) {
	a := MemoryExperience{ParentIDs: []string{"p1"}, Observation: map[string]string{"x": "1"}, Action: map[string]string{"a": "go"}, Outcome: map[string]string{"y": "2"}}
	b := MemoryExperience{ParentIDs: []string{"p2"}, Observation: map[string]string{"x": "1"}, Action: map[string]string{"a": "go"}, Outcome: map[string]string{"y": "2"}}
	if memoryStructurePatternHash(a) != memoryStructurePatternHash(b) {
		t.Fatal("lineage changed factual pattern hash")
	}
}
