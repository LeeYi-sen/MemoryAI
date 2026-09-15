package main

import (
	"testing"
	"time"
)

func TestMeshServeLocalMemoryReadsPassiveShard(t *testing.T) {
	memory := &Memory{
		ID: "mesh.shard.read", Layer: "emergent", Tags: []string{"memory", "mesh-test"},
		State: map[string]any{"where": "shard"}, Revision: 1,
	}
	e := loadFabricWriteTestEngine(t, []*Memory{memory})
	m := &meshRuntime{role: "sovereign", nodeID: "local", engine: e}

	res := m.serveLocalMemory(memory.ID)
	if !res.OK || res.Memory == nil {
		t.Fatalf("Mesh failed to read passive shard Memory: %#v", res)
	}
	if got := res.Memory.State["where"]; got != "shard" {
		t.Fatalf("Mesh returned wrong shard Memory state: %v", got)
	}
	if _, err := e.resolveIDLocal(memory.ID); err == nil {
		t.Fatal("test Memory unexpectedly exists in primary body")
	}
}

func TestMeshServeLocalExecutionRunsPassiveShardProgram(t *testing.T) {
	executable := &Memory{
		ID: "mesh.shard.exec", Layer: "emergent", Tags: []string{"memory", "mesh-test"},
		State: map[string]any{}, Revision: 1,
		Program: []Op{
			{Code: "set", A: "mesh_result", B: "executed-from-shard"},
			{Code: "halt"},
		},
	}
	e := loadFabricWriteTestEngine(t, []*Memory{executable})
	m := &meshRuntime{role: "sovereign", nodeID: "local", engine: e}

	res := m.serveLocalExecution(executable.ID, nil)
	if !res.OK || res.Frame == nil {
		t.Fatalf("Mesh failed to execute passive shard Memory: %#v", res)
	}
	if got := res.Frame.Vars["mesh_result"]; got != "executed-from-shard" {
		t.Fatalf("passive shard program did not execute: %q", got)
	}
	if _, err := e.resolveIDLocal(executable.ID); err == nil {
		t.Fatal("executed shard Memory was copied into primary body")
	}
}

func TestEventDispatchRunsHandlerFromPassiveShard(t *testing.T) {
	handler := &Memory{
		ID: "event.shard.handler", Layer: "emergent", Tags: []string{"memory", "event-handler"},
		Trigger: []string{"event:shard-event"}, State: map[string]any{}, Revision: 1,
		Program: []Op{
			{Code: "set", A: "event_result", B: "handled-from-shard"},
			{Code: "halt"},
		},
	}
	e := loadFabricWriteTestEngine(t, []*Memory{handler})
	f := newFrame()
	if err := e.fireEvent("shard-event", "", f); err != nil {
		t.Fatal(err)
	}
	if got := f.Vars["event_result"]; got != "handled-from-shard" {
		t.Fatalf("passive shard event handler did not execute: %q", got)
	}
}

func TestEventSubjectTagResolvesSubjectFromPassiveShard(t *testing.T) {
	subject := &Memory{
		ID: "event.shard.subject", Layer: "emergent", Tags: []string{"memory", "subject-blue"},
		State: map[string]any{}, Revision: 1,
	}
	handler := &Memory{
		ID: "event.shard.subject-handler", Layer: "emergent", Tags: []string{"memory", "event-handler"},
		Trigger: []string{"event:subject-event", "subject_tag:subject-blue"}, State: map[string]any{}, Revision: 1,
		Program: []Op{
			{Code: "set", A: "subject_result", B: "subject-found-in-shard"},
			{Code: "halt"},
		},
	}
	e := loadFabricWriteTestEngine(t, []*Memory{subject, handler})
	f := newFrame()
	if err := e.fireEvent("subject-event", subject.ID, f); err != nil {
		t.Fatal(err)
	}
	if got := f.Vars["subject_result"]; got != "subject-found-in-shard" {
		t.Fatalf("subject_tag did not resolve passive shard subject: %q", got)
	}
}

func TestCanonicalEventDispatchDoesNotReenterSchedulerLane(t *testing.T) {
	handler := &Memory{
		ID: "event.shard.canonical-handler", Layer: "emergent", Tags: []string{"memory", "event-handler"},
		Trigger: []string{"event:canonical-event"}, State: map[string]any{}, Revision: 1,
		Program: []Op{
			{Code: "set", A: "canonical_result", B: "single-lane"},
			{Code: "halt"},
		},
	}
	e := loadFabricWriteTestEngine(t, []*Memory{handler})
	f := newFrame()
	f.Vars["__txn_canonical"] = "1"

	globalTxnScheduler.commitMu.Lock()
	done := make(chan error, 1)
	go func() { done <- e.fireEvent("canonical-event", "", f) }()

	select {
	case err := <-done:
		globalTxnScheduler.commitMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		globalTxnScheduler.commitMu.Unlock()
		t.Fatal("canonical Event re-entered scheduler commit lane")
	}
	if got := f.Vars["canonical_result"]; got != "single-lane" {
		t.Fatalf("canonical shard handler did not execute: %q", got)
	}
}

func TestCanonicalEventHandlerKeepsSiblingShardVisibility(t *testing.T) {
	handler := &Memory{
		ID: "vm.shard.a", Layer: "emergent", Tags: []string{"memory", "event-handler"},
		Trigger: []string{"event:canonical-cross-shard"}, State: map[string]any{}, Revision: 1,
		Program: []Op{
			{Code: "state_get", A: "vm.shard.b", B: "value", C: "event_before"},
			{Code: "state_set", A: "vm.shard.b", B: "value", C: "event-updated"},
			{Code: "call", A: "vm.shard.child"},
			{Code: "halt"},
		},
	}
	target := &Memory{
		ID: "vm.shard.b", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"value": "initial"}, Revision: 1,
	}
	child := &Memory{
		ID: "vm.shard.child", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{}, Revision: 1,
		Program: []Op{{Code: "set", A: "event_child", B: "sibling-visible"}, {Code: "halt"}},
	}
	e, ownerA, ownerB := loadTwoShardVMTestEngine(t, []*Memory{handler}, []*Memory{target, child})
	f := newFrame()
	f.Vars["__txn_canonical"] = "1"

	globalTxnScheduler.commitMu.Lock()
	done := make(chan error, 1)
	go func() { done <- e.fireEvent("canonical-cross-shard", "", f) }()
	select {
	case err := <-done:
		globalTxnScheduler.commitMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		globalTxnScheduler.commitMu.Unlock()
		t.Fatal("canonical cross-shard Event re-entered scheduler or lost Fabric context")
	}

	if got := f.Vars["event_before"]; got != "initial" {
		t.Fatalf("canonical Event state_get lost sibling shard: %q", got)
	}
	if got := f.Vars["event_child"]; got != "sibling-visible" {
		t.Fatalf("canonical Event call lost sibling shard: %q", got)
	}
	actualOwner, got, err := e.resolveLocalFabricMemory(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if actualOwner != ownerB || got.State["value"] != "event-updated" || got.Revision != 2 {
		t.Fatalf("canonical Event state_set did not remain on sibling owner: owner=%p want=%p state=%v revision=%d", actualOwner, ownerB, got.State, got.Revision)
	}
	if _, err := e.resolveIDLocal(target.ID); !errorsIsVMEOF(err) {
		t.Fatalf("canonical Event copied target into primary: %v", err)
	}
	if _, err := ownerA.resolveIDLocal(target.ID); !errorsIsVMEOF(err) {
		t.Fatalf("canonical Event copied target into handler shard: %v", err)
	}
}
