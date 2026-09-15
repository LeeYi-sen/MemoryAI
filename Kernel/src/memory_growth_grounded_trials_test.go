package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func enableGroundedTrialSeries(t *testing.T, e *Engine, candidateID, seriesID string) *MemoryStructure {
	t.Helper()
	experiences, formation, validation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	structure, ok := validation.GetValidated(candidateID)
	if !ok || structure == nil {
		t.Fatalf("validated grounded structure unavailable: %s", candidateID)
	}
	if structure.Action == nil {
		structure.Action = map[string]string{}
	}
	structure.Action[groundedTrialSeriesActionKey] = seriesID
	upsertGroundedTrialStructure(validation, structure)
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
	return structure
}

func TestGroundedTrialSeriesAuthorsRepeatedInstancesWithExperienceLineage(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e, path := newMemoryGrowthRuntimeTestEngine(t)
	seed := seedValidatedGroundedStructure(t, e, server.URL, "trial-template-root", "200")
	enableGroundedTrialSeries(t, e, seed.CandidateID, "series-reality-probe")

	results := make([]*GroundedActionCycleResult, 0, 3)
	for generation := uint64(1); generation <= 3; generation++ {
		result, err := RunAutonomousGroundedActionCycle(e)
		if err != nil {
			e.close()
			t.Fatal(err)
		}
		if result.Skipped || !result.Persisted || result.ActionInstanceID == "" || result.TrialSeriesID != "series-reality-probe" || result.TrialGeneration != generation {
			e.close()
			t.Fatalf("grounded trial generation %d did not complete: %#v", generation, result)
		}
		if generation > 1 && result.ParentExperienceID != results[generation-2].ExperienceID {
			e.close()
			t.Fatalf("trial generation %d lost Experience lineage: parent=%s previous=%s", generation, result.ParentExperienceID, results[generation-2].ExperienceID)
		}
		results = append(results, result)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		e.close()
		t.Fatalf("grounded trial physical executions=%d, want 3", got)
	}
	if results[0].ActionID == results[1].ActionID || results[1].ActionID == results[2].ActionID || results[0].ActionID == results[2].ActionID {
		e.close()
		t.Fatalf("trial action identities were reused: %#v", results)
	}

	experiences, _, validation, err := LoadMemoryGrowthState(e)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	for i := 1; i < len(results); i++ {
		experience, ok := experiences.Get(results[i].ExperienceID)
		if !ok || len(experience.ParentIDs) != 1 || experience.ParentIDs[0] != results[i-1].ExperienceID {
			e.close()
			t.Fatalf("grounded Experience lineage mismatch at generation %d: %#v", i+1, experience)
		}
	}
	instances, err := listGroundedActionInstancesForStructure(e, seed.ID)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if len(instances) != 3 {
		e.close()
		t.Fatalf("grounded action instance count=%d, want 3", len(instances))
	}
	for i, instance := range instances {
		wantGeneration := uint64(i + 1)
		if instance.Generation != wantGeneration {
			e.close()
			t.Fatalf("instance generation=%d, want %d", instance.Generation, wantGeneration)
		}
		if i > 0 && instance.ParentInstanceID != instances[i-1].ID {
			e.close()
			t.Fatalf("instance lineage broke at generation %d: %#v", wantGeneration, instance)
		}
	}
	current, ok := validation.GetValidated(seed.CandidateID)
	if !ok || current.Action[groundedTrialGenerationActionKey] != "3" || current.Action[groundedTrialInstanceActionKey] != instances[2].ID {
		e.close()
		t.Fatalf("validated Structure did not retain latest authored action instance: %#v", current)
	}
	e.close()

	reloaded, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	fourth, err := RunAutonomousGroundedActionCycle(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.TrialGeneration != 4 || fourth.ParentExperienceID != results[2].ExperienceID || atomic.LoadInt32(&hits) != 4 {
		t.Fatalf("restart did not continue trial lineage exactly once: result=%#v hits=%d", fourth, atomic.LoadInt32(&hits))
	}
}

func TestGroundedTrialPreparedFenceBlocksNextInstanceAndReplay(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e, path := newMemoryGrowthRuntimeTestEngine(t)
	seed := seedValidatedGroundedStructure(t, e, server.URL, "trial-prepared-root", "200")
	enableGroundedTrialSeries(t, e, seed.CandidateID, "series-prepared-fence")
	authored, err := RunGroundedTrialAuthoringCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if authored.Skipped || authored.Generation != 1 {
		t.Fatalf("first trial instance was not authored: %#v", authored)
	}
	receipt := &groundedActionReceiptState{
		ReceiptID:    groundedActionReceiptID(authored.ActionID),
		ActionID:     authored.ActionID,
		StructureID:  seed.ID,
		AdapterID:    "source-adapter-grounded",
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
		t.Fatalf("prepared trial was replayed or advanced: result=%#v hits=%d", result, atomic.LoadInt32(&hits))
	}
	instances, err := listGroundedActionInstancesForStructure(reloaded, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("prepared ambiguity authored a successor instance: %#v", instances)
	}
}

func TestGroundedTrialRecoversPersistedResultBeforeAuthoringSuccessor(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	seed := seedValidatedGroundedStructure(t, e, server.URL, "trial-recovery-root", "200")
	enableGroundedTrialSeries(t, e, seed.CandidateID, "series-recovery")
	authored, err := RunGroundedTrialAuthoringCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &groundedActionReceiptState{
		ReceiptID:      groundedActionReceiptID(authored.ActionID),
		ActionID:       authored.ActionID,
		StructureID:    seed.ID,
		AdapterID:      "source-adapter-grounded",
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

	recovered, err := RunAutonomousGroundedActionCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.RecoveredFeedback || recovered.TrialGeneration != 1 || atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("persisted result was not recovered without I/O: result=%#v hits=%d", recovered, atomic.LoadInt32(&hits))
	}
	instances, err := listGroundedActionInstancesForStructure(e, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("successor was authored before feedback recovery: %#v", instances)
	}

	next, err := RunAutonomousGroundedActionCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if next.TrialGeneration != 2 || next.ParentExperienceID != recovered.ExperienceID || atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("trial did not continue after recovered feedback: result=%#v hits=%d", next, atomic.LoadInt32(&hits))
	}
}

func TestGroundedTrialSeriesDoesNotUseOutcomeAsKernelRetryPolicy(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	seed := seedValidatedGroundedStructure(t, e, server.URL, "trial-conflict-root", "200")
	enableGroundedTrialSeries(t, e, seed.CandidateID, "series-conflicting-reality")

	first, err := RunAutonomousGroundedActionCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunAutonomousGroundedActionCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if first.PredictionMatched || second.PredictionMatched || first.TrialGeneration != 1 || second.TrialGeneration != 2 || atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("Kernel outcome policy incorrectly stopped Memory-authored series: first=%#v second=%#v hits=%d", first, second, atomic.LoadInt32(&hits))
	}
}
