package main

import (
	"runtime"
	"sync/atomic"
	"testing"
)

func TestEventHandlerFailureDoesNotSuppressIndependentHandler(t *testing.T) {
	failing := &Memory{
		ID: "a.fail.handler", Layer: "emergent", Tags: []string{"memory"}, Trigger: []string{"event:isolation-test"},
		State: map[string]any{}, Revision: 1, Program: []Op{{Code: "unsupported-test-op"}},
	}
	success := &Memory{
		ID: "z.success.handler", Layer: "emergent", Tags: []string{"memory"}, Trigger: []string{"event:isolation-test"},
		Capabilities: []string{"memory.write"}, State: map[string]any{"runs": "0"}, Revision: 1,
		Program: []Op{{Code: "state_num_add", A: "z.success.handler", B: "runs", C: "1"}, {Code: "halt"}},
	}
	e := loadFabricWriteTestEngine(t, []*Memory{failing, success})
	if err := e.fireEvent("isolation-test", "", newFrame()); err == nil {
		t.Fatal("expected aggregate event error from failing handler")
	}
	_, got, err := e.resolveLocalFabricMemory(success.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State["runs"] != "1" {
		t.Fatalf("independent handler was suppressed by sibling failure: runs=%v", got.State["runs"])
	}
}

func busyEventProgram(iterations string) []Op {
	return []Op{
		{Code: "set", A: "i", B: "0"},
		{Code: "label", A: "loop"},
		{Code: "num_add", A: "i", B: "{{i}}", C: "1"},
		{Code: "cmp_lt", A: "again", B: "{{i}}", C: iterations},
		{Code: "jump_if", A: "{{again}}", B: "loop"},
		{Code: "halt"},
	}
}

func TestEventDispatchRunsIndependentHandlersConcurrently(t *testing.T) {
	oldProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(oldProcs)
	oldConcurrency := physicalConcurrency(0)
	setPhysicalExecutionConcurrency(4)
	defer setPhysicalExecutionConcurrency(oldConcurrency)
	atomic.StoreInt64(&speculativePeak, 0)

	handlers := make([]*Memory, 0, 4)
	for _, id := range []string{"parallel.a", "parallel.b", "parallel.c", "parallel.d"} {
		handlers = append(handlers, &Memory{
			ID: id, Layer: "emergent", Tags: []string{"memory"}, Trigger: []string{"event:parallel-test"},
			State: map[string]any{}, Revision: 1, Program: busyEventProgram("100000"),
		})
	}
	e := loadFabricWriteTestEngine(t, handlers)
	if err := e.fireEvent("parallel-test", "", newFrame()); err != nil {
		t.Fatal(err)
	}
	if peak := atomic.LoadInt64(&speculativePeak); peak < 2 {
		t.Fatalf("event handlers remained serial: speculative peak=%d", peak)
	}
}

func TestNestedEventDoesNotLeakContextToSiblingHandler(t *testing.T) {
	oldConcurrency := physicalConcurrency(0)
	setPhysicalExecutionConcurrency(1)
	defer setPhysicalExecutionConcurrency(oldConcurrency)

	parent := &Memory{ID: "subject.parent", Layer: "emergent", Tags: []string{"memory", "test.parent"}, State: map[string]any{}, Revision: 1}
	child := &Memory{ID: "subject.child", Layer: "emergent", Tags: []string{"memory", "test.child"}, State: map[string]any{}, Revision: 1}
	nester := &Memory{
		ID: "a.nested.emitter", Layer: "emergent", Tags: []string{"memory"}, Trigger: []string{"event:parent-event", "subject_tag:test.parent"},
		Capabilities: []string{"event.emit"}, State: map[string]any{}, Revision: 1,
		Program: []Op{{Code: "emit_event", A: "child-event", B: "subject.child"}},
	}
	observer := &Memory{
		ID: "z.parent.observer", Layer: "emergent", Tags: []string{"memory"}, Trigger: []string{"event:parent-event", "subject_tag:test.parent"},
		State: map[string]any{}, Revision: 1,
		Program: []Op{{Code: "copy", A: "seen_subject", B: "__subject"}},
	}
	e := loadFabricWriteTestEngine(t, []*Memory{parent, child, nester, observer})
	f := newFrame()
	if err := e.fireEvent("parent-event", parent.ID, f); err != nil {
		t.Fatal(err)
	}
	if got := f.Vars["seen_subject"]; got != parent.ID {
		t.Fatalf("nested event context leaked to sibling: got=%q want=%q", got, parent.ID)
	}
}
