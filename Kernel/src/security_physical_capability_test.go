package main

import "testing"

func TestCurrentPhysicalPrimitivesKeepCapabilityBoundary(t *testing.T) {
	current := map[string]string{
		"mesh_route_execution":               "mesh.run",
		"physical_execution_concurrency_set": "scheduler.control",
	}
	for code, want := range current {
		if got := requiredCapability(code); got != want {
			t.Fatalf("current physical primitive %s capability=%q want=%q", code, got, want)
		}
	}
	for _, legacy := range []string{"mesh_route_cognition", "cognition_concurrency_set"} {
		if got := requiredCapability(legacy); got != "" {
			t.Fatalf("legacy cognition-shaped primitive %s still has active capability mapping %q", legacy, got)
		}
	}
}
