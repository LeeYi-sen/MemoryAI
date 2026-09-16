package main

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func TestPhysicalMemoryWithJournalSeesIncrementalUpsertAndDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "core", []*Memory{{
		ID: "journal.visible", Layer: "emergent", Tags: []string{"memory"}, Revision: 1,
		State: map[string]any{"value": "before"},
	}})
	e, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	if err := e.saveBody(e.bodyPath); err != nil {
		t.Fatal(err)
	}

	_, current, err := e.resolveLocalFabricMemory("journal.visible")
	if err != nil {
		t.Fatal(err)
	}
	updated := copyMemory(current)
	updated.State["value"] = "after"
	updated.Revision++
	if err := e.upsertExplicitMemoryBounded(updated); err != nil {
		t.Fatal(err)
	}
	if err := persistEngineIncremental(e); err != nil {
		t.Fatal(err)
	}
	physical, state, err := physicalMemoryWithJournal(path, "journal.visible")
	if err != nil {
		t.Fatal(err)
	}
	if state != logicalDiskPresent || physical == nil || physical.State["value"] != "after" || physical.Revision != 2 {
		t.Fatalf("journal-aware physical read missed incremental upsert: %+v", physical)
	}

	if err := e.deleteExplicitMemoryBounded("journal.visible"); err != nil {
		t.Fatal(err)
	}
	if err := persistEngineIncremental(e); err != nil {
		t.Fatal(err)
	}
	if physical, state, err := physicalMemoryWithJournal(path, "journal.visible"); err != nil || state != logicalDiskAbsent || physical != nil {
		t.Fatalf("journal-aware physical read missed incremental delete: memory=%v state=%v err=%v", physical, state, err)
	}
}

func TestTransferProbeTreatsJournalCandidateAsDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{{
		ID: "seed", Layer: "emergent", Tags: []string{"memory"}, Revision: 1,
	}})
	dst, err := loadEngineCanonical(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.close()
	if err := dst.saveBody(dst.bodyPath); err != nil {
		t.Fatal(err)
	}
	candidate := &Memory{ID: "transfer.candidate", Layer: "emergent", Tags: []string{"memory"}, Revision: 1, State: map[string]any{"x": "1"}}
	if err := dst.upsertExplicitMemoryBounded(candidate); err != nil {
		t.Fatal(err)
	}
	if err := persistEngineIncremental(dst); err != nil {
		t.Fatal(err)
	}
	if got := probeTransferCandidateOnDisk(dst, candidate); got != transferCandidateDiskMatching {
		t.Fatalf("journal-backed durable candidate probed as %v", got)
	}
}
