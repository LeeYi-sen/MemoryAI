package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSpeculativePrimaryHitRejectsShardDuplicateIdentity(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	duplicateID := "txn.duplicate.primary-shard"
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{
		{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1},
		{ID: duplicateID, Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"owner": "primary"}, Revision: 1},
	})
	writeBodyForPersistenceTest(t, filepath.Join(dir, "Memory.1.mem"), "storage", []*Memory{
		{ID: duplicateID, Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"owner": "shard"}, Revision: 1},
	})

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.close)
	e.mountAutomaticStorageShards()

	ce, _, err := e.snapshotForSpeculationLazy(8)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLazySpeculation(ce)

	m, err := ce.resolveIDLocal(duplicateID)
	if err == nil || !strings.Contains(err.Error(), "duplicate local Fabric Memory id") {
		t.Fatalf("speculative primary hit accepted duplicate Fabric identity: memory=%#v err=%v", m, err)
	}
	ce.dataMu.RLock()
	_, baselined := ce.cache[duplicateID]
	ce.dataMu.RUnlock()
	if baselined {
		t.Fatal("duplicate primary/shard identity entered speculative private cache")
	}
}
