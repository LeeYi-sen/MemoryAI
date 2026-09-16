package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActivationCandidateWireShapeIsPhysicalOnly(t *testing.T) {
	payload, err := json.Marshal(ActivationCandidate{ID: "memory.example", FeatureHit: 2})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, "score") {
		t.Fatalf("activation candidate leaked cognitive score field: %s", text)
	}
	if !strings.Contains(text, "feature_hits") {
		t.Fatalf("activation candidate lost physical hit metadata: %s", text)
	}
}

func TestActivationRuntimeInfoUsesPhysicalPageCapVocabulary(t *testing.T) {
	t.Setenv("MEMORYAI_ACTIVATION_PAGE_CAP", "17")
	t.Setenv("MEMORYAI_ACTIVATION_TOPK", "5")
	r := newSparseActivationRuntime()
	if r.pageCap != 17 {
		t.Fatalf("physical page cap mismatch: got=%d want=17", r.pageCap)
	}
	info := r.Info()
	if _, exists := info["default_top_k"]; exists {
		t.Fatal("legacy cognitive Top-K key remains in activation info")
	}
	if _, exists := info["cognitive_ranking"]; exists {
		t.Fatal("activation info retains cognitive-ranking shaped key")
	}
	if got := info["default_page_cap"]; got != 17 {
		t.Fatalf("default_page_cap mismatch: got=%v want=17", got)
	}
	if got := info["physical_page_cap_only"]; got != true {
		t.Fatalf("physical_page_cap_only mismatch: got=%v", got)
	}
}

func TestLegacyActivationTopKEnvironmentDoesNotControlRuntime(t *testing.T) {
	t.Setenv("MEMORYAI_ACTIVATION_PAGE_CAP", "")
	t.Setenv("MEMORYAI_ACTIVATION_TOPK", "5")
	r := newSparseActivationRuntime()
	if r.pageCap != 64 {
		t.Fatalf("legacy Top-K environment still controls Kernel: got=%d want=64", r.pageCap)
	}
}
