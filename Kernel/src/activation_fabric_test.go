package main

import (
	"path/filepath"
	"testing"
)

func newActivationFabricTestEngine(t *testing.T) (*Engine, *Engine) {
	t.Helper()
	dir := t.TempDir()
	primary := filepath.Join(dir, "Memory.mem")
	shard := filepath.Join(dir, "Memory.1.mem")
	root := &Memory{
		ID: "root", Layer: "inherited", Tags: []string{"memory", "primary-only"},
		State: map[string]any{"zone": "primary"}, Revision: 1,
	}
	target := &Memory{
		ID: "activation.shard.target", Layer: "emergent", Tags: []string{"memory", "shard-visible"},
		State: map[string]any{"zone": "shard", "phase": "old"}, Revision: 1,
	}
	writeBodyForPersistenceTest(t, primary, "core", []*Memory{root})
	writeBodyForPersistenceTest(t, shard, "storage", []*Memory{target})

	e, err := loadEngine(primary)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.close() })
	e.mountAutomaticStorageShards()
	owner, _, err := e.resolveLocalFabricMemory(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if owner == nil || owner == e {
		t.Fatalf("target not mounted in passive shard: %v", owner)
	}
	return e, owner
}

func activationHasID(result ActivationResult, id string) bool {
	for _, candidate := range result.Candidates {
		if candidate.ID == id {
			return true
		}
	}
	return false
}

func TestActivationSeesPersistedPassiveShard(t *testing.T) {
	e, _ := newActivationFabricTestEngine(t)
	r := newSparseActivationRuntime()
	if err := r.Build(e); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{
		"tag:shard-visible",
		"state-kv:zone=shard",
		"id:activation.shard.target",
	} {
		result, err := r.Activate(e, query, 64)
		if err != nil {
			t.Fatal(err)
		}
		if !activationHasID(result, "activation.shard.target") {
			t.Fatalf("Fabric activation missed passive shard for %q: %#v", query, result.Candidates)
		}
	}
}

func TestActivationShardDirtyOverlayShadowsPersistedPosting(t *testing.T) {
	e, owner := newActivationFabricTestEngine(t)
	r := newSparseActivationRuntime()
	if err := r.Build(e); err != nil {
		t.Fatal(err)
	}

	current, err := owner.resolveIDLocal("activation.shard.target")
	if err != nil {
		t.Fatal(err)
	}
	q := copyMemory(current)
	q.Tags = []string{"memory", "shard-updated"}
	q.State["phase"] = "new"
	q.Revision++
	if err := upsertExplicitMemoryOnOwner(owner, q); err != nil {
		t.Fatal(err)
	}

	oldResult, err := r.Activate(e, "tag:shard-visible", 64)
	if err != nil {
		t.Fatal(err)
	}
	if activationHasID(oldResult, q.ID) {
		t.Fatal("stale persisted shard posting remained visible after dirty overlay")
	}

	for _, query := range []string{"tag:shard-updated", "state-kv:phase=new"} {
		result, err := r.Activate(e, query, 64)
		if err != nil {
			t.Fatal(err)
		}
		if !activationHasID(result, q.ID) {
			t.Fatalf("dirty shard overlay missing for %q: %#v", query, result.Candidates)
		}
	}
}
