package main

import (
	"testing"
)

func TestContextActivationRecursesAcrossMultipleValidatedContextLevels(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	parent, _, children := fullyValidateRootContextSplit(t, e)
	pChild := children["P"]

	experiences, formation, validation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	recordNestedContextReality(t, e, experiences, pChild, "nested-low-1", "low", "expand")
	recordNestedContextReality(t, e, experiences, pChild, "nested-low-2", "low", "expand")
	recordNestedContextReality(t, e, experiences, pChild, "nested-high-1", "high", "contract")
	recordNestedContextReality(t, e, experiences, pChild, "nested-high-2", "high", "contract")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	formed, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if formed.ParentStructureID != pChild.ID || formed.ContextKey != "temperature" {
		t.Fatalf("second-level split did not discover temperature: %#v", formed)
	}

	experiences, formation, validation, err = LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	recordNestedContextReality(t, e, experiences, pChild, "nested-low-3", "low", "expand")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	if _, err := RunAutonomousContextBranchingCycle(e); err != nil {
		t.Fatal(err)
	}

	experiences, formation, validation, err = LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	recordNestedContextReality(t, e, experiences, pChild, "nested-high-3", "high", "contract")
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	completed, err := RunAutonomousContextBranchingCycle(e)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.ParentRetired {
		t.Fatalf("second-level parent did not retire: %#v", completed)
	}

	resolution, err := ResolveContextualExecutionTarget(e, parent.ID, map[string]string{"structure": "P", "temperature": "high", "color": "red"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution.Conditions) != 2 || resolution.Conditions["structure"] != "P" || resolution.Conditions["temperature"] != "high" {
		t.Fatalf("recursive activation lost context chain: %#v", resolution)
	}
	if resolution.ResolvedID == pChild.ID {
		t.Fatalf("recursive activation stopped at retired intermediate structure: %#v", resolution)
	}
	if resolution.Structure.ExpectedOutcome["result"] != "contract" {
		t.Fatalf("recursive activation selected wrong leaf Reality: %#v", resolution.Structure)
	}
}
