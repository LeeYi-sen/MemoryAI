package main

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func seedContextBranchParent(t *testing.T, e *Engine) (*MemoryStructure, *experienceLedger, *memoryStructureFormation, *memoryStructureValidationLedger) {
	t.Helper()
	experiences := NewMemoryExperienceLedger()
	seed, err := experiences.Record(MemoryExperience{
		ID:             "context-parent-seed",
		Context:        map[string]string{"structure": "P", "color": "red"},
		Observation:    map[string]string{"stimulus": "heat"},
		Action:         map[string]string{"operation": "predict"},
		Outcome:        map[string]string{"result": "expand"},
		PredictionHash: "prediction-heat",
	})
	if err != nil {
		t.Fatal(err)
	}
	parent := &MemoryStructure{
		ID:                  "memory-structure-heat",
		CandidateID:         "candidate-heat",
		PatternHash:         memoryStructurePatternHash(*seed),
		SourceExperienceIDs: []string{seed.ID},
		Context:             map[string]string{"stimulus": "heat"},
		Observation:         map[string]string{"stimulus": "heat"},
		Action:              map[string]string{"operation": "predict"},
		ExpectedOutcome:     map[string]string{"result": "expand"},
		PredictionHash:      "prediction-heat",
		State:               memoryStructureValidatedState,
		ValidationCount:     1,
		SuccessCount:        1,
		Program:             []Op{{Code: "set", A: "result", B: "expand"}, {Code: "halt"}},
	}
	formation := NewMemoryStructureFormation()
	validation := NewMemoryStructureValidationLedger()
	validation.structures[parent.CandidateID] = cloneMemoryStructure(parent)
	validation.validated[parent.CandidateID] = cloneMemoryStructure(parent)
	executable, err := ExecutableMemory(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.upsertExplicitMemoryBounded(executable); err != nil {
		t.Fatal(err)
	}
	return parent, experiences, formation, validation
}

func recordContextReality(t *testing.T, e *Engine, experiences *experienceLedger, structure *MemoryStructure, id, structureKind, result string) *MemoryExperience {
	t.Helper()
	errorMap := calculatePredictionError(structure.ExpectedOutcome, map[string]string{"result": result})
	experience, err := experiences.Record(MemoryExperience{
		ID:      id,
		Context: map[string]string{"structure": structureKind, "color": "red"},
		Observation: map[string]string{
			"stimulus":            "heat",
			"memory_structure_id": structure.ID,
		},
		Action:          map[string]string{"operation": "predict", "memory_structure_id": structure.ID},
		Outcome:         map[string]string{"result": result},
		PredictionHash:  structure.PredictionHash,
		PredictionError: errorMap,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateMemoryBeliefsFromExperience(e, structure, experience); err != nil {
		t.Fatal(err)
	}
	return experience
}

func TestContextBranchingDiscoversDynamicContextAndRequiresIndependentWitness(t *testing.T) {
	e, path := newMemoryGrowthRuntimeTestEngine(t)
	parent, experiences, formation, validation := seedContextBranchParent(t, e)

	// P 和 Q 都是红色，因此 color 无法区分；只有动态 Context 键 structure 能分离现实结果。
	recordContextReality(t, e, experiences, parent, "p-1", "P", "expand")
	recordContextReality(t, e, experiences, parent, "p-2", "P", "expand")
	recordContextReality(t, e, experiences, parent, "q-1", "Q", "stable")
	recordContextReality(t, e, experiences, parent, "q-2", "Q", "stable")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		e.close()
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		e.close()
		t.Fatal(err)
	}

	formed, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if formed.SplitID == "" || formed.ContextKey != "structure" || len(formed.CandidateIDs) != 2 || !formed.Persisted {
		e.close()
		t.Fatalf("context split was not formed from the discriminating dynamic key: %#v", formed)
	}
	if formed.ValidatedStructureID != "" || formed.ParentRetired {
		e.close()
		t.Fatalf("branch promoted without independent witness: %#v", formed)
	}

	// 没有第三条独立 Experience 时，候选只能等待，不能自证。
	waiting, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if !waiting.Skipped {
		e.close()
		t.Fatalf("context branch advanced without an independent witness: %#v", waiting)
	}

	// 第三条 P Reality 验证第一个分支；父结构仍保留，直到所有分支都被独立验证。
	loadedExperiences, loadedFormation, loadedValidation, err := LoadMemoryGrowthState(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	recordContextReality(t, e, loadedExperiences, parent, "p-3", "P", "expand")
	if err := PersistMemoryGrowthState(e, loadedExperiences, loadedFormation, loadedValidation); err != nil {
		e.close()
		t.Fatal(err)
	}
	firstValidated, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if firstValidated.ValidatedStructureID == "" || firstValidated.ParentRetired {
		e.close()
		t.Fatalf("first branch validation did not preserve parent fallback: %#v", firstValidated)
	}

	loadedExperiences, loadedFormation, loadedValidation, err = LoadMemoryGrowthState(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	recordContextReality(t, e, loadedExperiences, parent, "q-3", "Q", "stable")
	if err := PersistMemoryGrowthState(e, loadedExperiences, loadedFormation, loadedValidation); err != nil {
		e.close()
		t.Fatal(err)
	}
	secondValidated, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if secondValidated.ValidatedStructureID == "" || !secondValidated.ParentRetired || !secondValidated.Persisted {
		e.close()
		t.Fatalf("all context branches did not retire the coarse parent: %#v", secondValidated)
	}
	_, _, finalValidation, err := LoadMemoryGrowthState(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if _, ok := finalValidation.GetValidated(parent.CandidateID); ok {
		e.close()
		t.Fatal("coarse parent remained validated after all contextual branches passed reality validation")
	}
	if _, _, err := e.resolveLocalFabricMemory(parent.ID); err == nil || !errorsIsEOF(err) {
		e.close()
		t.Fatalf("retired coarse parent remained executable in Fabric: %v", err)
	}
	e.close()

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	splits, err := listMemoryContextSplits(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(splits) != 1 || splits[0].Status != memoryContextSplitValidated || splits[0].ContextKey != "structure" {
		t.Fatalf("validated context split did not survive restart: %#v", splits)
	}
	_, _, restartedValidation, err := LoadMemoryGrowthState(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range splits[0].Branches {
		child, ok := restartedValidation.GetValidated(branch.CandidateID)
		if !ok || !containsString(child.ParentStructureIDs, parent.ID) || len(child.Program) == 0 {
			t.Fatalf("validated contextual child lost lineage/program across restart: %#v", child)
		}
	}
}

func errorsIsEOF(err error) bool {
	return err != nil && (err == io.EOF || errors.Is(err, io.EOF))
}

func TestContextBranchingDoesNotSplitWithoutSeparatingContext(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	parent, experiences, formation, validation := seedContextBranchParent(t, e)

	// 同一个 Context 值 P 同时出现 expand/stable；没有任何 Context 键能稳定分离结果。
	recordContextReality(t, e, experiences, parent, "same-p-1", "P", "expand")
	recordContextReality(t, e, experiences, parent, "same-p-2", "P", "expand")
	recordContextReality(t, e, experiences, parent, "same-p-3", "P", "stable")
	recordContextReality(t, e, experiences, parent, "same-p-4", "P", "stable")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}

	result, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped || result.SplitID != "" {
		t.Fatalf("non-separating context incorrectly created a split: %#v", result)
	}
}

func TestContextBranchingMarksExistingSplitContestedByNewReality(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	parent, experiences, formation, validation := seedContextBranchParent(t, e)

	recordContextReality(t, e, experiences, parent, "contest-p-1", "P", "expand")
	recordContextReality(t, e, experiences, parent, "contest-p-2", "P", "expand")
	recordContextReality(t, e, experiences, parent, "contest-q-1", "Q", "stable")
	recordContextReality(t, e, experiences, parent, "contest-q-2", "Q", "stable")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
	formed, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if formed.SplitID == "" {
		t.Fatal("expected initial context split proposal")
	}

	loadedExperiences, loadedFormation, loadedValidation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	// 新 Reality 证明 Q 并不稳定对应 stable，因此之前的 structure 分离被现实推翻。
	recordContextReality(t, e, loadedExperiences, parent, "contest-q-new", "Q", "expand")
	if err := PersistMemoryGrowthState(e, loadedExperiences, loadedFormation, loadedValidation); err != nil {
		t.Fatal(err)
	}
	result, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Contested || !result.Persisted {
		t.Fatalf("new contradictory Reality did not contest the existing split: %#v", result)
	}
	split, _, err := loadMemoryContextSplit(e, formed.SplitID)
	if err != nil {
		t.Fatal(err)
	}
	if split == nil || split.Status != memoryContextSplitContested {
		t.Fatalf("contested split state was not persisted: %#v", split)
	}
	_, _, currentValidation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := currentValidation.GetValidated(parent.CandidateID); !ok {
		t.Fatal("contested split incorrectly retired the coarse parent")
	}
}

func TestMemoryContextSplitStateLivesInsideMemoryMem(t *testing.T) {
	e, path := newMemoryGrowthRuntimeTestEngine(t)
	parent, experiences, formation, validation := seedContextBranchParent(t, e)
	recordContextReality(t, e, experiences, parent, "persist-p-1", "P", "expand")
	recordContextReality(t, e, experiences, parent, "persist-p-2", "P", "expand")
	recordContextReality(t, e, experiences, parent, "persist-q-1", "Q", "stable")
	recordContextReality(t, e, experiences, parent, "persist-q-2", "Q", "stable")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		e.close()
		t.Fatal(err)
	}
	if _, err := RunAutonomousContextBranchingCycle(e); err != nil {
		e.close()
		t.Fatal(err)
	}
	e.close()

	entries, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Base(entries[0]) != filepath.Base(path) {
		t.Fatalf("context branching created a runtime sidecar: %v", entries)
	}
}
