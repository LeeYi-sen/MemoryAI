package main

import "testing"

func TestMemoryStructuralDigestIgnoresRuntimeExecCount(t *testing.T) {
	a := &Memory{
		ID: "digest-test", Layer: "emergent", Tags: []string{"memory"},
		State: map[string]any{"belief": "stable"}, Revision: 7, RuntimeExecCount: 1,
	}
	b := copyMemory(a)
	b.RuntimeExecCount = 999999
	if memoryJSONDigest(a) != memoryJSONDigest(b) {
		t.Fatal("physical RuntimeExecCount changed Memory structural identity")
	}
	b.State["belief"] = "changed"
	if memoryJSONDigest(a) == memoryJSONDigest(b) {
		t.Fatal("semantic Memory state change did not change structural identity")
	}
}
