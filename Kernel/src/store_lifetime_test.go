package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestUpsertExplicitMemoryDoesNotReenterDataLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", nil)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	done := make(chan error, 1)
	go func() {
		done <- e.upsertExplicitMemoryBounded(&Memory{
			ID: "upsert-no-reentry", Layer: "emergent", Tags: []string{"memory"},
			State: map[string]any{"v": "1"}, Revision: 1,
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("upsertExplicitMemoryBounded deadlocked by re-entering dataMu through store access")
	}
	if _, err := e.resolveIDLocal("upsert-no-reentry"); err != nil {
		t.Fatalf("upserted Memory not locally visible: %v", err)
	}
}

func TestRemoteJSONImportIsDirectReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	writeBodyForPersistenceTest(t, path, "storage", nil)
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	payload, _ := json.Marshal(&Memory{
		ID: "must-not-import-remote", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"remote": true}, Revision: 1,
	})
	id, status, err := e.importMemoryJSON(string(payload), true)
	if err == nil || status != "denied" || id != "" {
		t.Fatalf("remote import was not denied: id=%q status=%q err=%v", id, status, err)
	}
	if _, err := e.resolveIDLocal("must-not-import-remote"); err == nil {
		t.Fatal("remote Memory was copied into local body")
	}
}

func TestSpeculativePinnedStoreAvoidsNestedReadDeadlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{root})
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	ce, _, err := e.snapshotForSpeculationLazy(32)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLazySpeculation(ce)

	guard := indexedStoreGuard(e.store)
	writerStarted := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		close(writerStarted)
		guard.Lock()
		guard.Unlock()
		close(writerDone)
	}()
	<-writerStarted
	// Give the writer an opportunity to queue. If speculative reads attempted a
	// second RLock, Go RWMutex writer preference would block that read behind the
	// writer while the snapshot itself still held the first RLock.
	time.Sleep(20 * time.Millisecond)

	readDone := make(chan error, 1)
	go func() {
		_, er := ce.storeGetID("root")
		readDone <- er
	}()
	select {
	case er := <-readDone:
		if er != nil {
			t.Fatalf("pinned speculative store read failed: %v", er)
		}
	case <-time.After(500 * time.Millisecond):
		releaseLazySpeculation(ce)
		t.Fatal("speculative store read deadlocked behind waiting writer")
	}

	releaseLazySpeculation(ce)
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("store writer did not proceed after speculative lease release")
	}
}

func TestPersistWaitsForSpeculativeStoreLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{root})
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	ce, _, err := e.snapshotForSpeculationLazy(32)
	if err != nil {
		t.Fatal(err)
	}
	e.addRuntimeMemory(&Memory{ID: "new-during-test", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1})

	persistDone := make(chan error, 1)
	go func() { persistDone <- e.saveBody(path) }()
	select {
	case er := <-persistDone:
		releaseLazySpeculation(ce)
		t.Fatalf("persist completed while speculative store lease was active: %v", er)
	case <-time.After(75 * time.Millisecond):
		// Expected: finalize waits for the old IndexedStore write guard.
	}

	releaseLazySpeculation(ce)
	select {
	case er := <-persistDone:
		if er != nil {
			t.Fatal(er)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("persist did not resume after speculative store lease release")
	}
}
