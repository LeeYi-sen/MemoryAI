package main

import "testing"

func uint64InfoValue(t *testing.T, info map[string]any, key string) uint64 {
	t.Helper()
	value, ok := info[key].(uint64)
	if !ok {
		t.Fatalf("physical runtime stat %q missing or wrong type: %#v", key, info[key])
	}
	return value
}

func TestPhysicalRuntimeStatsExposeEventHandlerRunsAndFailures(t *testing.T) {
	before := physicalRuntimeInfo()
	beforeRuns := uint64InfoValue(t, before, "event_handler_runs")
	beforeFailures := uint64InfoValue(t, before, "event_handler_failures")

	handler := &Memory{
		ID: "event.stats.failure", Layer: "emergent", Tags: []string{"memory"},
		Trigger: []string{"event:stats-failure"}, State: map[string]any{}, Revision: 1,
		Program: []Op{{Code: "unsupported-test-op"}},
	}
	e := loadFabricWriteTestEngine(t, []*Memory{handler})
	if err := e.fireEvent("stats-failure", "", newFrame()); err == nil {
		t.Fatal("expected physical handler execution failure")
	}
	after := physicalRuntimeInfo()
	if got := uint64InfoValue(t, after, "event_handler_runs"); got != beforeRuns+1 {
		t.Fatalf("handler run counter = %d, want %d", got, beforeRuns+1)
	}
	if got := uint64InfoValue(t, after, "event_handler_failures"); got != beforeFailures+1 {
		t.Fatalf("handler failure counter = %d, want %d", got, beforeFailures+1)
	}
}
