package main

import (
	"strings"
	"sync"
	"testing"
)

func TestLazySnapshotLoadsOnlyReadSetAndEnforcesLimit(t *testing.T) {
	memories := []*Memory{
		{ID: "memory.clean.a", Layer: "inherited", State: map[string]any{"v": "a"}},
		{ID: "memory.clean.b", Layer: "inherited", State: map[string]any{"v": "b"}},
	}
	zr, path, manifest := writeIndexedStoreForTest(t, memories)
	defer zr.Close()
	store, err := openIndexedStore(path, zr, manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	e := &Engine{
		store:      store,
		cache:      map[string]*Memory{},
		newIDs:     map[string]bool{},
		dirtyIDs:   map[string]bool{},
		deletedIDs: map[string]bool{},
		spaces:     map[string]*Engine{},
		dataMu:     &sync.RWMutex{},
		tagAdded:   map[string]map[string]bool{},
		tagRemoved: map[string]map[string]bool{},
	}

	ce, base, err := e.snapshotForSpeculationLazy(1)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLazySpeculation(ce)

	if len(base.memories) != 0 || len(ce.cache) != 0 {
		t.Fatalf("lazy snapshot preloaded clean persisted Memory: base=%d cache=%d", len(base.memories), len(ce.cache))
	}
	if len(e.cache) != 0 {
		t.Fatalf("parent cache changed before speculative read: %d", len(e.cache))
	}

	if _, err := ce.resolveIDLocal("memory.clean.a"); err != nil {
		t.Fatal(err)
	}
	if len(base.memories) != 1 || base.memories["memory.clean.a"] == nil {
		t.Fatalf("first read was not baselined exactly once: %#v", base.memories)
	}
	if len(e.cache) != 0 {
		t.Fatalf("speculative page-in leaked into parent cache: %d", len(e.cache))
	}

	if _, err := ce.resolveIDLocal("memory.clean.b"); err == nil || !strings.Contains(err.Error(), "snapshot working-set limit exceeded") {
		t.Fatalf("second clean read should hit read-set limit, got %v", err)
	}
	if len(base.memories) != 1 {
		t.Fatalf("overflow read mutated baseline: %d", len(base.memories))
	}
}
