package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryGrowthStatePersistsInsideMemoryMem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "core", []*Memory{{
		ID: "seed", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"value": "seed"}, Revision: 1,
	}})

	e, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}

	experiences := NewMemoryExperienceLedger()
	a, err := experiences.Record(MemoryExperience{
		ID: "experience-a", Context: map[string]string{"state": "ready"},
		Observation: map[string]string{"input": "x"}, Action: map[string]string{"operation": "act"},
		Outcome: map[string]string{"result": "ok"}, PredictionHash: "prediction-1",
	})
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	b, err := experiences.Record(MemoryExperience{
		ID: "experience-b", Context: map[string]string{"state": "ready"},
		Observation: map[string]string{"input": "x"}, Action: map[string]string{"operation": "act"},
		Outcome: map[string]string{"result": "ok"}, PredictionHash: "prediction-1",
	})
	if err != nil {
		e.close()
		t.Fatal(err)
	}

	formation := NewMemoryStructureFormation()
	candidates, err := formation.FormCandidates([]*MemoryExperience{a, b}, 2)
	if err != nil || len(candidates) != 1 {
		e.close()
		t.Fatalf("candidate formation failed: count=%d err=%v", len(candidates), err)
	}
	validation := NewMemoryStructureValidationLedger()
	witness, err := experiences.Record(MemoryExperience{
		ID: "experience-witness", Context: map[string]string{"state": "ready"},
		Observation: map[string]string{"input": "x"}, Action: map[string]string{"operation": "act"},
		Outcome: map[string]string{"result": "ok"}, PredictionHash: "prediction-1",
	})
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if _, _, err := validation.ValidateCandidate(candidates[0], a, witness, 1); err != nil {
		e.close()
		t.Fatal(err)
	}

	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		e.close()
		t.Fatal(err)
	}
	if err := e.saveBody(e.bodyPath); err != nil {
		e.close()
		t.Fatal(err)
	}
	e.close()

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path) {
			t.Fatalf("unexpected runtime sidecar/artifact: %s", entry.Name())
		}
	}

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	loadedExperiences, loadedFormation, loadedValidation, err := LoadMemoryGrowthState(reloaded)
	if err != nil {
		reloaded.close()
		t.Fatal(err)
	}
	if len(loadedExperiences.Snapshot()) != 3 {
		reloaded.close()
		t.Fatalf("experience count after restart = %d, want 3", len(loadedExperiences.Snapshot()))
	}
	loadedCandidate, ok := loadedFormation.Get(candidates[0].ID)
	if !ok || len(loadedCandidate.SourceExperienceIDs) != 2 {
		reloaded.close()
		t.Fatal("candidate provenance did not survive restart")
	}
	loadedStructure, ok := loadedValidation.GetValidated(candidates[0].ID)
	if !ok || loadedStructure.SuccessCount != 1 || len(loadedStructure.ValidationExperienceIDs) != 1 {
		reloaded.close()
		t.Fatal("validated Memory Structure did not survive restart")
	}
	if len(loadedValidation.ValidationHistory(candidates[0].ID)) != 1 {
		reloaded.close()
		t.Fatal("validation history did not survive restart")
	}
	reloaded.close()
}

func TestMemoryGrowthStateUsesReservedMemoryRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "core", []*Memory{{
		ID: "seed", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"value": "seed"}, Revision: 1,
	}})
	e, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	if err := PersistMemoryGrowthState(e, NewMemoryExperienceLedger(), NewMemoryStructureFormation(), NewMemoryStructureValidationLedger()); err != nil {
		t.Fatal(err)
	}
	m, err := e.resolveIDLocal(memoryGrowthStateRecordID)
	if err != nil {
		t.Fatal(err)
	}
	if m.State["memory_growth_state"] == nil {
		t.Fatal("growth state was not placed inside Memory State")
	}
}
