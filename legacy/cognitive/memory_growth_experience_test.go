package main

import "testing"

func TestMemoryExperienceLedgerLineageAndValidation(t *testing.T) {
	ledger := NewMemoryExperienceLedger()
	root, err := ledger.Record(MemoryExperience{Observation: map[string]string{"state": "cold"}})
	if err != nil {
		t.Fatal(err)
	}
	child, err := ledger.Record(MemoryExperience{ParentIDs: []string{root.ID}, Action: map[string]string{"action": "heat"}, Outcome: map[string]string{"state": "warm"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ledger.Children(root.ID)); got != 1 {
		t.Fatalf("children=%d", got)
	}
	if _, err := ledger.Validate(child.ID, true, "reproduced"); err != nil {
		t.Fatal(err)
	}
	got, ok := ledger.Get(child.ID)
	if !ok {
		t.Fatal("child missing")
	}
	if got.ValidationCount != 1 || got.SuccessCount != 1 || got.FailureCount != 0 {
		t.Fatalf("validation counters=%+v", got)
	}
	if len(ledger.ValidationHistory(child.ID)) != 1 {
		t.Fatal("validation history missing")
	}
}

func TestMemoryExperienceLedgerRejectsMissingParent(t *testing.T) {
	ledger := NewMemoryExperienceLedger()
	if _, err := ledger.Record(MemoryExperience{ParentIDs: []string{"missing"}, Observation: map[string]string{"x": "1"}}); err == nil {
		t.Fatal("expected missing parent rejection")
	}
}

func TestMemoryExperienceLedgerCanonicalContentHash(t *testing.T) {
	ledger := NewMemoryExperienceLedger()
	a, err := ledger.Record(MemoryExperience{Observation: map[string]string{"b": "2", "a": "1"}, Action: map[string]string{"x": "y"}})
	if err != nil {
		t.Fatal(err)
	}
	b := MemoryExperience{Observation: map[string]string{"a": "1", "b": "2"}, Action: map[string]string{"x": "y"}}
	if a.ContentHash != experienceContentHash(b) {
		t.Fatalf("hash not canonical: %s != %s", a.ContentHash, experienceContentHash(b))
	}
}
