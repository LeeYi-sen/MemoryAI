package main

import (
	"strings"
	"sync"
	"testing"
)

func TestLocalMemoryCountFastUsesStoreBaseAndDeltas(t *testing.T) {
	memories := []*Memory{
		{ID: "persisted.a", Layer: "inherited", State: map[string]any{}},
		{ID: "persisted.b", Layer: "inherited", State: map[string]any{}},
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
		cache:      map[string]*Memory{"new.c": {ID: "new.c"}},
		newIDs:     map[string]bool{"new.c": true},
		dirtyIDs:   map[string]bool{},
		deletedIDs: map[string]bool{"persisted.a": true},
		spaces:     map[string]*Engine{},
		dataMu:     &sync.RWMutex{},
		tagAdded:   map[string]map[string]bool{},
		tagRemoved: map[string]map[string]bool{},
	}
	if got := localMemoryCountFast(e); got != 2 {
		t.Fatalf("fast count=%d want=2 (base 2 + new 1 - deleted persisted 1)", got)
	}
}

func TestBoundedLegacyBodyListRejectsBeforeWholeStoreEnumeration(t *testing.T) {
	t.Setenv("MEMORYAI_BODY_LIST_MAX", "16")
	// ids.size deliberately claims an index entry while file is nil. Any call
	// to AllIDs/readIndex would dereference the nil file. The large cardinality
	// guard must reject before that can happen.
	store := &IndexedStore{memoryCount: 1000000, ids: section{size: indexEntrySize}}
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
	ids, err := boundedLegacyBodyIDs(e)
	if err == nil || !strings.Contains(err.Error(), "body_list bounded") {
		t.Fatalf("large body_list should be rejected before enumeration: ids=%v err=%v", ids, err)
	}
	if got := localMemoryCountFast(e); got != 1000000 {
		t.Fatalf("fast count should use cardinality metadata without enumeration: %d", got)
	}
}
