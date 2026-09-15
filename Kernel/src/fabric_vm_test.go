package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func loadTwoShardVMTestEngine(t *testing.T, shardA, shardB []*Memory) (*Engine, *Engine, *Engine) {
	t.Helper()
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{{
		ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1,
	}})
	writeBodyForPersistenceTest(t, filepath.Join(dir, "Memory.1.mem"), "storage", shardA)
	writeBodyForPersistenceTest(t, filepath.Join(dir, "Memory.2.mem"), "storage", shardB)

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	e.mountAutomaticStorageShards()
	t.Cleanup(e.close)
	if got := len(e.mountedSpacePaths()); got != 2 {
		t.Fatalf("expected two mounted storage shards, got %d: %v", got, e.mountedSpacePaths())
	}

	ownerA, _, err := e.resolveLocalFabricMemory("vm.shard.a")
	if err != nil {
		t.Fatal(err)
	}
	ownerB, _, err := e.resolveLocalFabricMemory("vm.shard.b")
	if err != nil {
		t.Fatal(err)
	}
	if ownerA == e || ownerB == e || ownerA == ownerB {
		t.Fatalf("test topology did not produce distinct passive owners: A=%p B=%p root=%p", ownerA, ownerB, e)
	}
	return e, ownerA, ownerB
}

func TestCanonicalVMKeepsRootFabricAcrossSiblingShards(t *testing.T) {
	exec := &Memory{
		ID: "vm.shard.a", Layer: "emergent", Tags: []string{"memory", "vm-test"},
		State: map[string]any{}, Revision: 1,
		Program: []Op{
			{Code: "state_get", A: "vm.shard.b", B: "value", C: "before"},
			{Code: "state_set", A: "vm.shard.b", B: "value", C: "updated"},
			{Code: "call", A: "vm.shard.child"},
			{Code: "halt"},
		},
	}
	target := &Memory{
		ID: "vm.shard.b", Layer: "emergent", Tags: []string{"memory", "vm-test"},
		State: map[string]any{"value": "initial"}, Revision: 1,
	}
	child := &Memory{
		ID: "vm.shard.child", Layer: "emergent", Tags: []string{"memory", "vm-test"},
		State: map[string]any{}, Revision: 1,
		Program: []Op{
			{Code: "set", A: "child_result", B: "called-from-sibling-shard"},
			{Code: "halt"},
		},
	}

	e, ownerA, ownerB := loadTwoShardVMTestEngine(t, []*Memory{exec}, []*Memory{target, child})
	f := newFrame()
	if err := globalTxnScheduler.canonical(e, exec.ID, f); err != nil {
		t.Fatal(err)
	}

	if got := f.Vars["before"]; got != "initial" {
		t.Fatalf("state_get lost sibling shard visibility: got=%q", got)
	}
	if got := f.Vars["child_result"]; got != "called-from-sibling-shard" {
		t.Fatalf("call lost sibling shard visibility: got=%q", got)
	}

	actualOwner, got, err := e.resolveLocalFabricMemory(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if actualOwner != ownerB {
		t.Fatalf("state_set moved target owner: got=%p want=%p", actualOwner, ownerB)
	}
	if got.State["value"] != "updated" || got.Revision != 2 {
		t.Fatalf("state_set did not mutate sibling owner in place: state=%v revision=%d", got.State, got.Revision)
	}

	if _, err := e.resolveIDLocal(target.ID); !errorsIsVMEOF(err) {
		t.Fatalf("target was copied into primary: %v", err)
	}
	if _, err := ownerA.resolveIDLocal(target.ID); !errorsIsVMEOF(err) {
		t.Fatalf("target was copied into executing shard: %v", err)
	}
	if _, err := e.resolveIDLocal(child.ID); !errorsIsVMEOF(err) {
		t.Fatalf("child was copied into primary: %v", err)
	}
	if _, err := ownerA.resolveIDLocal(child.ID); !errorsIsVMEOF(err) {
		t.Fatalf("child was copied into executing shard: %v", err)
	}

	_, ranExec, err := e.resolveLocalFabricMemory(exec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ranExec.RuntimeExecCount == 0 {
		t.Fatal("canonical execution telemetry did not update executing shard Memory")
	}
	_, ranChild, err := e.resolveLocalFabricMemory(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ranChild.RuntimeExecCount == 0 {
		t.Fatal("nested call execution telemetry did not update sibling shard Memory")
	}
}

func loadDuplicateTargetVMTestEngine(t *testing.T, program []Op) (*Engine, []*Engine) {
	t.Helper()
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	exec := &Memory{
		ID: "vm.duplicate.exec", Layer: "emergent", Tags: []string{"memory", "vm-test"},
		State: map[string]any{}, Revision: 1, Program: program,
	}
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	left := &Memory{ID: "vm.duplicate.target", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"value": "left"}, Revision: 1}
	right := &Memory{ID: "vm.duplicate.target", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"value": "right"}, Revision: 1}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{root, exec})
	writeBodyForPersistenceTest(t, filepath.Join(dir, "Memory.1.mem"), "storage", []*Memory{left})
	writeBodyForPersistenceTest(t, filepath.Join(dir, "Memory.2.mem"), "storage", []*Memory{right})

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	e.mountAutomaticStorageShards()
	t.Cleanup(e.close)
	paths := e.mountedSpacePaths()
	if len(paths) != 2 {
		t.Fatalf("expected duplicate target in two shards, got paths=%v", paths)
	}
	owners := make([]*Engine, 0, 2)
	for _, path := range paths {
		e.spaceMu.RLock()
		owner := e.spaces[path]
		e.spaceMu.RUnlock()
		if owner == nil {
			t.Fatalf("mounted shard missing engine: %s", path)
		}
		owners = append(owners, owner)
	}
	return e, owners
}

func assertDuplicateTargetsUnchanged(t *testing.T, owners []*Engine) {
	t.Helper()
	seen := map[string]bool{}
	for _, owner := range owners {
		m, err := owner.resolveIDLocal("vm.duplicate.target")
		if err != nil {
			t.Fatal(err)
		}
		value := m.State["value"].(string)
		seen[value] = true
		if m.Revision != 1 {
			t.Fatalf("duplicate target was mutated despite identity conflict: value=%q revision=%d", value, m.Revision)
		}
	}
	if !seen["left"] || !seen["right"] {
		t.Fatalf("duplicate target fixtures changed unexpectedly: %v", seen)
	}
}

func TestCanonicalVMRejectsDuplicateFabricStateGet(t *testing.T) {
	e, owners := loadDuplicateTargetVMTestEngine(t, []Op{
		{Code: "state_get", A: "vm.duplicate.target", B: "value", C: "got"},
		{Code: "halt"},
	})
	f := newFrame()
	err := globalTxnScheduler.canonical(e, "vm.duplicate.exec", f)
	if err == nil || !strings.Contains(err.Error(), "duplicate local Fabric Memory id") {
		t.Fatalf("duplicate state_get did not fail closed: err=%v frame=%v", err, f.Vars)
	}
	if f.Vars["got"] != "" {
		t.Fatalf("duplicate state_get leaked a randomly selected value: %q", f.Vars["got"])
	}
	assertDuplicateTargetsUnchanged(t, owners)
}

func TestCanonicalVMRejectsDuplicateFabricStateSetWithoutWrite(t *testing.T) {
	e, owners := loadDuplicateTargetVMTestEngine(t, []Op{
		{Code: "state_set", A: "vm.duplicate.target", B: "value", C: "corrupt"},
		{Code: "halt"},
	})
	f := newFrame()
	err := globalTxnScheduler.canonical(e, "vm.duplicate.exec", f)
	if err == nil || !strings.Contains(err.Error(), "duplicate local Fabric Memory id") {
		t.Fatalf("duplicate state_set did not fail closed: err=%v", err)
	}
	assertDuplicateTargetsUnchanged(t, owners)
}

func errorsIsVMEOF(err error) bool {
	return err == io.EOF
}
