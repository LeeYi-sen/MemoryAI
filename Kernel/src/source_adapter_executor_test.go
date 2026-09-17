package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestSourceAdapterOperationalTagsMatchMemorySelectors(t *testing.T) {
	m := sourceAdapterMemory(sourceAdapter{ID: "evidence", Config: map[string]string{"enabled": "1"}}, 1)
	if !memoryHasTag(m, "source.adapter.enabled") {
		t.Fatal("enabled source adapter is invisible to Memory selector")
	}
	a := sourceAdapterMemory(sourceAdapter{ID: "action", Config: map[string]string{"enabled": "1", "role": "action"}}, 1)
	if !memoryHasTag(a, "source.adapter.enabled") || !memoryHasTag(a, "source.adapter.action") {
		t.Fatalf("action adapter tags do not match Memory selectors: %v", a.Tags)
	}
}

func TestPhysicalEventExposesNamespacedVars(t *testing.T) {
	f := newFrame()
	applyPhysicalEventFrame(f, PhysicalEvent{Name: "response", Vars: map[string]string{"payload": "ok"}})
	if f.Vars["payload"] != "ok" || f.Vars["__event.payload"] != "ok" {
		t.Fatalf("event variable namespace missing: %#v", f.Vars)
	}
}

func TestExternalRequestExecutorPersistsDoneAndEmitsResponse(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"observations":["external fact"]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	body := filepath.Join(dir, "Memory.mem")
	root := &Memory{ID: "root", Layer: "inherited", Tags: []string{"memory"}, State: map[string]any{}, Revision: 1}
	request := &Memory{
		ID: "request-1", Layer: "acquired", Tags: []string{"memory", "io.external.request.pending"}, Revision: 1,
		State: map[string]any{
			"source_adapter_id": "source-1",
			"source_id":         "source-1",
			"goal_id":           "goal-1",
			"response_event":    "external.response",
			"io.status":         "pending",
		},
	}
	handler := &Memory{
		ID: "response-handler", Layer: "inherited", Tags: []string{"memory"}, Trigger: []string{"event:external.response", "subject_tag:io.external.request.done"},
		Capabilities: []string{"memory.write"}, State: map[string]any{}, Revision: 1,
		Program: []Op{
			{Code: "copy", A: "payload", B: "__event.normalized_payload"},
			{Code: "state_set", A: "response-handler", B: "payload", C: "{{payload}}"},
			{Code: "halt"},
		},
	}
	writeBodyForPersistenceTest(t, body, "core", []*Memory{root, request, handler})
	e, err := loadEngineWithMutationJournal(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.upsertSourceAdapter(sourceAdapter{ID: "source-1", Config: map[string]string{"enabled": "1", "url": server.URL}}); err != nil {
		e.close()
		t.Fatal(err)
	}
	if err := e.processExternalIORequests(); err != nil {
		e.close()
		t.Fatal(err)
	}
	_, done, err := e.resolveLocalFabricMemory(request.ID)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if done.State["io.status"] != "done" || memoryHasTag(done, "io.external.request.pending") || !memoryHasTag(done, "io.external.request.done") {
		e.close()
		t.Fatalf("request lifecycle not completed: tags=%v state=%v", done.Tags, done.State)
	}
	_, gotHandler, err := e.resolveLocalFabricMemory(handler.ID)
	if err != nil {
		e.close()
		t.Fatal(err)
	}
	if gotHandler.State["payload"] != `{"observations":["external fact"]}` {
		e.close()
		t.Fatalf("response event payload not delivered: %v", gotHandler.State["payload"])
	}
	if atomic.LoadInt32(&calls) != 1 {
		e.close()
		t.Fatalf("source executed %d times", calls)
	}
	e.close()

	reloaded, err := loadEngineWithMutationJournal(body)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.close()
	if err := reloaded.processExternalIORequests(); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("done request replayed external side effect after restart: calls=%d", calls)
	}
}
