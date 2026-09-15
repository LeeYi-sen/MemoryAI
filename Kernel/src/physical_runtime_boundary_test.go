package main

import (
	"runtime"
	"testing"
)

func TestPhysicalRuntimeInfoHasNoCognitionABIKeys(t *testing.T) {
	info := physicalRuntimeInfo()
	for _, key := range []string{"parallel", "speculative", "scheduler"} {
		if _, ok := info[key]; !ok {
			t.Fatalf("physical runtime info missing %q", key)
		}
	}
	parallel, ok := info["parallel"].(map[string]any)
	if !ok {
		t.Fatalf("parallel runtime info has unexpected type: %T", info["parallel"])
	}
	for _, forbidden := range []string{"cognitive_selection", "fixed_semantic_lanes", "score", "rank", "utility", "goal"} {
		if _, exists := parallel[forbidden]; exists {
			t.Fatalf("physical runtime info leaked cognition-shaped key %q", forbidden)
		}
	}
}

func TestPhysicalExecutionConcurrencyIsPhysicallyBounded(t *testing.T) {
	setPhysicalExecutionConcurrency(runtime.GOMAXPROCS(0) + 1000)
	got := int(globalParallelRuntime.Info()["physical_concurrency"].(int64))
	if got < 1 || got > runtime.GOMAXPROCS(0) {
		t.Fatalf("physical execution concurrency escaped host bound: %d", got)
	}
	setPhysicalExecutionConcurrency(1)
}

func TestSpeculativeBoundaryUsesPhysicalPrimitiveNames(t *testing.T) {
	for _, code := range []string{"mesh_route_execution", "physical_execution_concurrency_set"} {
		if !speculativeAdditionalForbiddenPrimitive(code) {
			t.Fatalf("physical side-effect primitive was not canonical-only: %s", code)
		}
	}
	for _, legacy := range []string{"mesh_route_cognition", "cognition_concurrency_set"} {
		if speculativeAdditionalForbiddenPrimitive(legacy) {
			t.Fatalf("legacy cognition-shaped primitive remained active: %s", legacy)
		}
	}
}
