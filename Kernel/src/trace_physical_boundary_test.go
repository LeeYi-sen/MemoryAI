package main

import "testing"

func TestExecutionTraceDoesNotInterpretCognitiveTags(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	tagged := &Memory{
		ID: "trace.surface.tagged", Layer: "emergent", Revision: 1,
		Tags: []string{"memory", "cog.surface.synthetic"}, Program: []Op{{Code: "halt"}},
	}
	plain := &Memory{
		ID: "trace.plain", Layer: "emergent", Revision: 1,
		Tags: []string{"memory", "physical.synthetic"}, Program: []Op{{Code: "halt"}},
	}
	e.addRuntimeMemory(tagged)
	e.addRuntimeMemory(plain)
	if err := e.run(tagged.ID, newFrame()); err != nil {
		t.Fatal(err)
	}
	if err := e.run(plain.ID, newFrame()); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, id := range e.trace {
		seen[id] = true
	}
	if !seen[tagged.ID] || !seen[plain.ID] {
		t.Fatalf("trace interpreted Memory semantics: trace=%v", e.trace)
	}
}
