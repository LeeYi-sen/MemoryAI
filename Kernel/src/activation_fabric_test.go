package main

import (
	"path/filepath"
	"sync/atomic"
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

func containsActivationID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
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

func TestActivationBuildCountsCompleteFabric(t *testing.T) {
	e, _ := newActivationFabricTestEngine(t)
	r := newSparseActivationRuntime()
	if err := r.Build(e); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&r.nodes); got != 2 {
		t.Fatalf("Activation Build counted only part of Fabric: got=%d want=2", got)
	}
	if got := fabricMemoryCountFast(e); got != 2 {
		t.Fatalf("Fabric fast count mismatch: got=%d want=2", got)
	}
}

func TestActivationPrimaryLiveOverlayRemainsVisible(t *testing.T) {
	e, _ := newActivationFabricTestEngine(t)
	r := newSparseActivationRuntime()
	if err := r.Build(e); err != nil {
		t.Fatal(err)
	}

	live := &Memory{
		ID: "activation.primary.live", Layer: "emergent", Tags: []string{"memory", "primary-live"},
		State: map[string]any{"phase": "live"}, Revision: 1,
	}
	if err := upsertExplicitMemoryOnOwner(e, live); err != nil {
		t.Fatal(err)
	}

	result, err := r.Activate(e, "tag:primary-live", 64)
	if err != nil {
		t.Fatal(err)
	}
	if !activationHasID(result, live.ID) {
		t.Fatalf("Activate filtered valid primary live overlay: %#v", result.Candidates)
	}
	exact, err := r.ExactFeatureIDs(e, "tag:primary-live")
	if err != nil {
		t.Fatal(err)
	}
	if !containsActivationID(exact, live.ID) {
		t.Fatalf("ExactFeatureIDs filtered valid primary live overlay: %#v", exact)
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

func TestActivationQualificationUsesFullFabricReference(t *testing.T) {
	e, _ := newActivationFabricTestEngine(t)
	report, err := qualifySparseActivation(e)
	if err != nil {
		t.Fatalf("Fabric activation qualification failed: report=%+v err=%v", report, err)
	}
	if !report.OK || report.IndexedNodes != 2 || report.ReferenceNodes != 2 {
		t.Fatalf("qualification used incomplete Fabric corpus: %+v", report)
	}
	if report.ExactCandidate != report.Queries || report.ExactTopK != report.Queries {
		t.Fatalf("qualification exactness incomplete: %+v", report)
	}
}
