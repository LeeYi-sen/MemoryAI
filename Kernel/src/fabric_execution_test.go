package main

import "testing"

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
