package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitialChineseInputRendersMemoryOwnedReply(t *testing.T) {
	source := filepath.Join("..", "..", "data", "Memory.mem")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	body := filepath.Join(t.TempDir(), "Memory.mem")
	if err := os.WriteFile(body, raw, 0600); err != nil {
		t.Fatal(err)
	}
	e, err := loadEngineWithMutationJournal(body)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()

	res := e.handleDaemonRequest(daemonRequest{Args: []string{"input", "你是谁"}})
	if !res.OK || res.Frame == nil {
		t.Fatalf("Chinese input failed: %#v", res)
	}
	if got := res.Frame.Vars["reply_text"]; got != "我是MemoryAI" {
		t.Fatalf("Memory-owned Chinese rendering missing: got=%q frame=%#v", got, res.Frame.Vars)
	}
	wantEvents := map[string]bool{"experience.raw": false, "semantic.relation.observed": false, "semantic.answer.request": false, "expression.frame.ready": false, "reply.ready": false}
	for _, ev := range res.Frame.Events {
		if _, ok := wantEvents[ev.Name]; ok {
			wantEvents[ev.Name] = true
		}
	}
	for name, seen := range wantEvents {
		if !seen {
			t.Fatalf("live semantic event missing: %s", name)
		}
	}
}
