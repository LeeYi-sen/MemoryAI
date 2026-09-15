package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func seedValidatedGroundedStructure(t *testing.T, e *Engine, adapterURL, actionID, expectedStatus string) *MemoryStructure {
	t.Helper()
	adapterID := "source-adapter-grounded"
	if _, err := e.upsertSourceAdapter(sourceAdapter{ID: adapterID, Config: map[string]string{
		"url":    adapterURL,
		"method": http.MethodGet,
	}}); err != nil {
		t.Fatal(err)
	}

	experiences := NewMemoryExperienceLedger()
	source, err := experiences.Record(MemoryExperience{
		ID:             "experience-grounded-source",
		Observation:    map[string]string{"kind": "grounded"},
		Action:         map[string]string{"operation": "external-request"},
		Outcome:        map[string]string{"status_code": expectedStatus},
		PredictionHash: "prediction-grounded",
	})
	if err != nil {
		t.Fatal(err)
	}
	formation := NewMemoryStructureFormation()
	validation := NewMemoryStructureValidationLedger()
	structure := &MemoryStructure{
		ID:                  "memory-structure-grounded",
		CandidateID:         "candidate-grounded",
		PatternHash:         memoryStructurePatternHash(*source),
		SourceExperienceIDs: []string{source.ID},
		Observation:         cloneStringMap(source.Observation),
		Action: map[string]string{
			"operation":                 "external-request",
			groundedAdapterActionKey:    adapterID,
			groundedActionIDKey:         actionID,
			groundedAutonomousActionKey: "1",
			groundedTimeoutActionKey:    "5s",
		},
		ExpectedOutcome: map[string]string{"status_code": expectedStatus},
		PredictionHash:  source.PredictionHash,
		State:           memoryStructureValidatedState,
		ValidationCount: 1,
		SuccessCount:    1,
		Program:         []Op{{Code: "halt"}},
	}
	validation.structures[structure.CandidateID] = cloneMemoryStructure(structure)
	validation.validated[structure.CandidateID] = cloneMemoryStructure(structure)
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
	return structure
}

func TestAutonomousGroundedActionExecutesOnceAndPersistsBelief(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	e, path := newMemoryGrowthRuntimeTestEngine(t)
	structure := seedValidatedGroundedStructure(t, e, server.URL, "grounded-action-once", "200")

	first, err := RunAutonomousGroundedActionCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if first.Skipped || !first.Persisted || !first.PredictionMatched || first.ExperienceID == "" || len(first.BeliefIDs) != 1 {
		e.close()
		t.Fatalf("grounded action did not complete feedback loop: %#v", first)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		e.close()
		t.Fatalf("external action count=%d, want 1", got)
	}

	second, err := RunAutonomousGroundedActionCycle(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if !second.Skipped {
		e.close()
		t.Fatalf("completed action was scheduled again: %#v", second)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		e.close()
		t.Fatalf("idempotency receipt allowed duplicate external action: %d", got)
	}

	beliefs, err := MemoryBeliefsForStructure(e, structure.ID)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if len(beliefs) != 1 || len(beliefs[0].SupportingExperienceIDs) != 1 || len(beliefs[0].ConflictingExperienceIDs) != 0 {
		e.close()
		t.Fatalf("unexpected belief evidence: %#v", beliefs)
	}
	_, beliefMemory, err := e.resolveLocalFabricMemory(beliefs[0].ID)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if _, exists := beliefMemory.State["confidence"]; exists {
		e.close()
		t.Fatal("Kernel confidence score leaked into Memory-native Belief")
	}
	e.close()

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	reloadedBeliefs, err := MemoryBeliefsForStructure(reloaded, structure.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedBeliefs) != 1 || len(reloadedBeliefs[0].EvidenceExperienceIDs) != 1 {
		t.Fatalf("belief did not survive Memory.mem restart: %#v", reloadedBeliefs)
	}
	afterRestart, err := RunAutonomousGroundedActionCycle(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if !afterRestart.Skipped || atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("restart replayed completed external action: result=%#v hits=%d", afterRestart, atomic.LoadInt32(&hits))
	}
}

func TestGroundedRealityConflictBecomesBeliefEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	structure := seedValidatedGroundedStructure(t, e, server.URL, "grounded-action-conflict", "200")
	result, err := RunAutonomousGroundedActionCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if result.PredictionMatched || result.PhysicalError == "" || !result.Persisted {
		t.Fatalf("HTTP failure was not retained as reality evidence: %#v", result)
	}

	experiences, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	experience, ok := experiences.Get(result.ExperienceID)
	if !ok || experience.Outcome["status_code"] != "500" || len(experience.PredictionError) == 0 {
		t.Fatalf("grounded prediction error missing: %#v", experience)
	}
	beliefs, err := MemoryBeliefsForStructure(e, structure.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(beliefs) != 1 || len(beliefs[0].ConflictingExperienceIDs) != 1 || beliefs[0].ObservedValues["500"] != 1 {
		t.Fatalf("conflicting grounded evidence missing from belief: %#v", beliefs)
	}
}

func TestPreparedGroundedReceiptIsNeverAutomaticallyReplayed(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e, path := newMemoryGrowthRuntimeTestEngine(t)
	structure := seedValidatedGroundedStructure(t, e, server.URL, "grounded-action-ambiguous", "200")
	actionID := structure.Action[groundedActionIDKey]
	receipt := &groundedActionReceiptState{
		ReceiptID:    groundedActionReceiptID(actionID),
		ActionID:     actionID,
		StructureID:  structure.ID,
		AdapterID:    structure.Action[groundedAdapterActionKey],
		Status:       groundedReceiptPrepared,
		PreparedNano: 1,
	}
	if err := upsertGroundedActionReceipt(e, receipt, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
	e.close()

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	result, err := RunAutonomousGroundedActionCycle(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped || atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("ambiguous prepared receipt was automatically replayed: result=%#v hits=%d", result, atomic.LoadInt32(&hits))
	}
}

func TestResultPersistedReceiptRecoversFeedbackWithoutRepeatingIO(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e, path := newMemoryGrowthRuntimeTestEngine(t)
	structure := seedValidatedGroundedStructure(t, e, server.URL, "grounded-action-recover", "200")
	actionID := structure.Action[groundedActionIDKey]
	receipt := &groundedActionReceiptState{
		ReceiptID:      groundedActionReceiptID(actionID),
		ActionID:       actionID,
		StructureID:    structure.ID,
		AdapterID:      structure.Action[groundedAdapterActionKey],
		Status:         groundedReceiptResultPersisted,
		PreparedNano:   1,
		CompletedNano:  2,
		PhysicalResult: map[string]any{"status_code": 200, "ok": true},
	}
	if err := upsertGroundedActionReceipt(e, receipt, 2); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
	e.close()

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	result, err := RunAutonomousGroundedActionCycle(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RecoveredFeedback || !result.Persisted || !result.PredictionMatched || result.ExperienceID == "" {
		t.Fatalf("persisted result was not reconciled into Memory feedback: %#v", result)
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("feedback recovery repeated external I/O: hits=%d", atomic.LoadInt32(&hits))
	}
	loaded, _, err := loadGroundedActionReceipt(reloaded, actionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.Status != groundedReceiptFeedbackCommitted || loaded.ExperienceID != result.ExperienceID {
		t.Fatalf("receipt did not reach feedback_committed: %#v", loaded)
	}
	if got := fmt.Sprint(loaded.PhysicalResult["status_code"]); got != "200" {
		t.Fatalf("physical result changed during recovery: %s", got)
	}
}

func TestBeliefTracksSupportConflictAndMissingWithoutFixedScore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "core", []*Memory{{ID: "seed", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}})
	e, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	structure := &MemoryStructure{ID: "belief-structure", State: memoryStructureValidatedState, ExpectedOutcome: map[string]string{"answer": "yes"}}

	for i, outcome := range []map[string]string{{"answer": "yes"}, {"answer": "no"}, {}} {
		experience := &MemoryExperience{
			ID:          fmt.Sprintf("belief-evidence-%d", i),
			CreatedNano: int64(i + 1),
			Observation: map[string]string{"memory_structure_id": structure.ID},
			Outcome:     outcome,
		}
		if _, err := UpdateMemoryBeliefsFromExperience(e, structure, experience); err != nil {
			t.Fatal(err)
		}
	}
	beliefs, err := MemoryBeliefsForStructure(e, structure.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(beliefs) != 1 {
		t.Fatalf("belief count=%d, want 1", len(beliefs))
	}
	belief := beliefs[0]
	if len(belief.SupportingExperienceIDs) != 1 || len(belief.ConflictingExperienceIDs) != 1 || len(belief.MissingExperienceIDs) != 1 {
		t.Fatalf("dynamic evidence sets incorrect: %#v", belief)
	}
	if belief.ObservedValues["yes"] != 1 || belief.ObservedValues["no"] != 1 {
		t.Fatalf("observed alternatives incorrect: %#v", belief.ObservedValues)
	}
}

func TestConcurrentGroundedCyclesDoNotCrossAtMostOnceFence(t *testing.T) {
	var hits int32
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	seedValidatedGroundedStructure(t, e, server.URL, "grounded-action-concurrent", "200")

	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan *GroundedActionCycleResult, 2)
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		result, err := RunAutonomousGroundedActionCycle(e)
		results <- result
		errs <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first grounded action did not reach external I/O")
	}
	go func() {
		defer wg.Done()
		result, err := RunAutonomousGroundedActionCycle(e)
		results <- result
		errs <- err
	}()
	// Give the concurrent call a chance to observe the in-process fence before releasing I/O.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	completed := 0
	skipped := 0
	for result := range results {
		if result == nil {
			t.Fatal("grounded cycle returned nil result")
		}
		if result.Skipped {
			skipped++
		} else if result.Persisted {
			completed++
		}
	}
	if completed != 1 || skipped != 1 {
		t.Fatalf("concurrent grounded cycle fence mismatch: completed=%d skipped=%d", completed, skipped)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("concurrent grounded actions crossed at-most-once fence: hits=%d", got)
	}
}
