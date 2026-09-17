package main

import (
	"encoding/json"
	"testing"
)

func TestExecutableMemoryContractFieldsRoundTrip(t *testing.T) {
	raw := []byte(`{"id":"contract.memory","layer":"emergent","generation":1,"revision":1,"executable":true,"input_pattern":{"policy":"policy.drive"},"output_effect":{"weights":"clamped"}}`)
	var memory Memory
	if err := json.Unmarshal(raw, &memory); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(&memory)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got["executable"] != true {
		t.Fatalf("executable contract field lost: %s", encoded)
	}
	input, ok := got["input_pattern"].(map[string]any)
	if !ok || input["policy"] != "policy.drive" {
		t.Fatalf("input_pattern contract field lost: %s", encoded)
	}
	output, ok := got["output_effect"].(map[string]any)
	if !ok || output["weights"] != "clamped" {
		t.Fatalf("output_effect contract field lost: %s", encoded)
	}
}
