package main

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireRemoteMutationACKRejectsApplicationFailure(t *testing.T) {
	if err := requireRemoteMutationACK("space_put", `{"ok":true,"id":"m1"}`); err != nil {
		t.Fatalf("positive ACK rejected: %v", err)
	}
	if err := requireRemoteMutationACK("space_put", `{"ok":false,"error":"disk full"}`); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("negative ACK was not surfaced: %v", err)
	}
	if err := requireRemoteMutationACK("space_put", `not-json`); err == nil {
		t.Fatal("malformed mutation ACK was accepted")
	}
}

func TestTransferMemoryDurableMovePersistsTargetBeforeSourceRemoval(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	target := filepath.Join(dir, "MoveTarget.mem")
	rootMemory := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	source := &Memory{ID: "move-me", Layer: "emergent", Tags: []string{"memory", "move"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{rootMemory, source})
	writeBodyForPersistenceTest(t, target, "storage", nil)

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := transferMemoryDurable(e, source.ID, target, true)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if newID != source.ID {
		e.close()
		t.Fatalf("unexpected move id: got %q want %q", newID, source.ID)
	}
	e.close()

	targetEngine, err := loadEngine(target)
	if err != nil {
		t.Fatal(err)
	}
	got, err := targetEngine.resolveIDLocal(source.ID)
	if err != nil || got == nil || got.State["v"] != "1" {
		targetEngine.close()
		t.Fatalf("destination was not durable after successful move: got=%v err=%v", got, err)
	}
	targetEngine.close()

	sourceEngine, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceEngine.close()
	if _, err := sourceEngine.resolveIDLocal(source.ID); !errors.Is(err, io.EOF) {
		t.Fatalf("source remained durable after successful move: %v", err)
	}
}

func TestTransferMemoryDurableTargetFailureRetainsSource(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	target := filepath.Join(dir, "MoveTarget.mem")
	rootMemory := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	source := &Memory{ID: "retain-me", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "safe"}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{rootMemory, source})
	writeBodyForPersistenceTest(t, target, "storage", nil)

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := e.mountSpace(target)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	e.spaceMu.RLock()
	dst := e.spaces[cp]
	e.spaceMu.RUnlock()
	if dst == nil {
		e.close()
		t.Fatal("target did not mount")
	}
	originalBodyPath := dst.bodyPath
	dst.bodyPath = filepath.Join(dir, "missing-parent", "cannot-persist.mem")

	_, err = transferMemoryDurable(e, source.ID, target, true)
	dst.bodyPath = originalBodyPath
	if err == nil {
		e.close()
		t.Fatal("target persistence failure was reported as move success")
	}
	owner, got, resolveErr := e.resolveLocalFabricMemoryCopy(source.ID)
	if resolveErr != nil || owner != e || got == nil || got.State["v"] != "safe" {
		e.close()
		t.Fatalf("source was not retained after destination failure: owner=%p got=%v err=%v", owner, got, resolveErr)
	}
	e.close()

	sourceEngine, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceEngine.close()
	if _, err := sourceEngine.resolveIDLocal(source.ID); err != nil {
		t.Fatalf("durable source disappeared after destination failure: %v", err)
	}
}

func TestTransferSourceChangePreventsDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Memory.mem")
	source := &Memory{ID: "changing", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{source})
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	snapshot, err := resolveSpecificOwnerMemoryCopy(e, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	live, err := e.resolveIDLocal(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.dataMu.Lock()
	live.State["v"] = "2"
	live.Revision = 2
	e.dirtyIDs[live.ID] = true
	e.dirty = true
	e.dataMu.Unlock()

	err = markTransferSourceDeletedIfUnchanged(e, source.ID, structuralMemoryDigest(snapshot))
	if err == nil || !strings.Contains(err.Error(), "changed after destination durability") {
		t.Fatalf("changed source was deleted or wrong error returned: %v", err)
	}
	got, err := resolveSpecificOwnerMemoryCopy(e, source.ID)
	if err != nil || got.State["v"] != "2" {
		t.Fatalf("changed source was not retained: got=%v err=%v", got, err)
	}
}

func TestTransferDeletePersistenceFailureRestoresSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Memory.mem")
	source := &Memory{ID: "restore-me", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"v": "1"}, Revision: 1}
	writeBodyForPersistenceTest(t, path, "storage", []*Memory{source})
	e, err := loadEngine(path)
	if err != nil {
		t.Fatal(err)
	}
	originalBodyPath := e.bodyPath
	e.bodyPath = filepath.Join(dir, "missing-parent", "cannot-persist.mem")

	snapshot, err := resolveSpecificOwnerMemoryCopy(e, source.ID)
	if err != nil {
		e.bodyPath = originalBodyPath
		e.close()
		t.Fatal(err)
	}
	err = durableDeleteTransferSource(e, snapshot)
	e.bodyPath = origiginalBodyPath
	if err == nil {
		e.close()
		t.Fatal("source delete persistence failure was reported as success")
	}
	got, resolveErr := resolveSpecificOwnerMemoryCopy(e, source.ID)
	if resolveErr != nil || got == nil || got.State["v"] != "1" {
		e.close()
		t.Fatalf("source was not restored after delete persistence failure: got=%v err=%v", got, resolveErr)
	}
	e.close()
}
