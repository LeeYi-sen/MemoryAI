package main

import "testing"

func TestCalculatePredictionErrorExactMatch(t *testing.T) {
	predicted := map[string]string{"result": "ok", "count": "2"}
	actual := map[string]string{"result": "ok", "count": "2"}
	if got := calculatePredictionError(predicted, actual); len(got) != 0 {
		t.Fatalf("exact execution produced prediction error: %v", got)
	}
}

func TestCalculatePredictionErrorRecordsMismatchAndMissing(t *testing.T) {
	predicted := map[string]string{"result": "ok", "count": "2", "status": "ready"}
	actual := map[string]string{"result": "failed", "count": "2"}
	got := calculatePredictionError(predicted, actual)
	if got["result"] != "expected=ok;actual=failed" {
		t.Fatalf("unexpected mismatch record: %v", got)
	}
	if got["status"] != "expected=ready;actual=<missing>" {
		t.Fatalf("unexpected missing record: %v", got)
	}
	if _, ok := got["count"]; ok {
		t.Fatalf("matching field was incorrectly recorded as error: %v", got)
	}
}

func TestCaptureActualOutcomeOnlyUsesPredictedKeys(t *testing.T) {
	predicted := map[string]string{"result": "ok"}
	actual := captureActualOutcome(map[string]any{
		"result": "ok",
		"temporary_vm_value": "must-not-be-an-outcome",
	}, predicted)
	if len(actual) != 1 || actual["result"] != "ok" {
		t.Fatalf("unexpected actual outcome: %v", actual)
	}
}

func TestMemoryExperiencePredictionErrorIsPersistedInHash(t *testing.T) {
	ledger := NewMemoryExperienceLedger()
	a, err := ledger.Record(MemoryExperience{
		ID:              "experience-prediction-a",
		Observation:     map[string]string{"structure": "s"},
		Outcome:         map[string]string{"result": "bad"},
		PredictionError: map[string]string{"result": "expected=ok;actual=bad"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ledger.Record(MemoryExperience{
		ID:              "experience-prediction-b",
		Observation:     map[string]string{"structure": "s"},
		Outcome:         map[string]string{"result": "bad"},
		PredictionError: map[string]string{"result": "expected=ok;actual=other"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ContentHash == b.ContentHash {
		t.Fatal("prediction error did not affect experience content hash")
	}
}
