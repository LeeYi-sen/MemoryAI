package main

import "testing"

func TestMemoryCopyClearsStaleCapabilitySignature(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	parent := &Memory{
		ID: "signed.parent", Layer: "emergent", Revision: 2,
		Tags: []string{"memory"}, Program: []Op{{Code: "halt"}},
		Capabilities: []string{"memory.write"}, CapabilitySig: "stale-parent-signature",
	}
	e.addRuntimeMemory(parent)
	f := newFrame()
	if _, err := e.execPrimitive(&Memory{ID: "driver"}, Op{Code: "memory_copy", A: "child", B: parent.ID}, f, 0, nil); err != nil {
		t.Fatal(err)
	}
	child, err := e.resolve(f.Vars["child"])
	if err != nil {
		t.Fatal(err)
	}
	if child.CapabilitySig != "" {
		t.Fatalf("copied structural identity retained stale signature: %q", child.CapabilitySig)
	}
}

func TestProgramMutationClearsStaleCapabilitySignature(t *testing.T) {
	e := loadCurrentBodyForGrowthTest(t)
	target := &Memory{
		ID: "signed.mutable", Layer: "emergent", Revision: 2,
		Tags: []string{"memory"}, Program: []Op{{Code: "cmp_gt", A: "x", B: "1", C: "0"}},
		Capabilities: []string{"program.mutate"}, CapabilitySig: "stale-program-signature",
	}
	e.addRuntimeMemory(target)
	f := newFrame()
	op := Op{Code: "program_set_field", A: target.ID, B: "0", C: "cmp_ge", Args: map[string]string{"field": "code"}}
	if _, err := e.execPrimitive(&Memory{ID: "driver"}, op, f, 0, nil); err != nil {
		t.Fatal(err)
	}
	got, err := e.resolve(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CapabilitySig != "" {
		t.Fatalf("program mutation retained stale signature: %q", got.CapabilitySig)
	}
}
