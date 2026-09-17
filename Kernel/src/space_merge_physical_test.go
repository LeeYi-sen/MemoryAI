package main

import (
	"path/filepath"
	"testing"
)

func TestSpaceMergePreservesDivergentIdentityForMemoryResolution(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	source := filepath.Join(dir, "source.mem")
	original := &Memory{ID: "same", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"value": "local"}, Revision: 1}
	unique := &Memory{ID: "unique", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"value": "remote"}, Revision: 1}
	divergent := copyMemory(original)
	divergent.State = map[string]any{"value": "remote"}
	divergent.Revision = 2
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{original})
	writeBodyForPersistenceTest(t, source, "storage", []*Memory{divergent, unique})
	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	stat, err := e.mergeSpace(source, "primary")
	if err != nil {
		t.Fatal(err)
	}
	if stat["conflicts"] != 1 || stat["copied"] != 1 {
		t.Fatalf("unexpected physical merge report: %#v", stat)
	}
	got, err := e.resolveIDLocal("same")
	if err != nil {
		t.Fatal(err)
	}
	if got.State["value"] != "local" || got.Revision != 1 {
		t.Fatalf("Kernel reconciled divergent identity instead of preserving conflict: %#v", got)
	}
	if _, err := e.resolveIDLocal("unique"); err != nil {
		t.Fatalf("non-conflicting physical copy missing: %v", err)
	}
	ids, _ := e.localIDs()
	for _, id := range ids {
		m, _ := e.resolveIDLocal(id)
		if m != nil && m.State["merge_conflict"] == "1" {
			t.Fatalf("Kernel invented a conflict branch: %s", id)
		}
	}
}
