package main

import (
	"testing"
	"time"
)

func TestMountedShardDataLocksAreIndependent(t *testing.T) {
	a := &Memory{ID: "vm.shard.a", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"value": "a"}, Revision: 1}
	b := &Memory{ID: "vm.shard.b", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"value": "b"}, Revision: 1}
	e, ownerA, ownerB := loadTwoShardVMTestEngine(t, []*Memory{a}, []*Memory{b})

	if e.dataMu == ownerA.dataMu || e.dataMu == ownerB.dataMu || ownerA.dataMu == ownerB.dataMu {
		t.Fatalf("Fabric bodies unexpectedly share one dataMu: root=%p A=%p B=%p", e.dataMu, ownerA.dataMu, ownerB.dataMu)
	}
}

func TestSpeculativeCommitAcrossTwoShardOwnersDoesNotSelfDeadlock(t *testing.T) {
	a := &Memory{ID: "vm.shard.a", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"value": "a"}, Revision: 1}
	b := &Memory{ID: "vm.shard.b", Layer: "emergent", Tags: []string{"memory"}, State: map[string]any{"value": "b"}, Revision: 1}
	e, ownerA, ownerB := loadTwoShardVMTestEngine(t, []*Memory{a}, []*Memory{b})

	ce, base, err := e.snapshotForSpeculationLazy(8)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLazySpeculation(ce)

	ma, err := ce.resolveIDLocal(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := ce.resolveIDLocal(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	ma.State["value"] = "a2"
	mb.State["value"] = "b2"
	ma.Revision++
	mb.Revision++
	ce.dataMu.Lock()
	ce.dirtyIDs[a.ID] = true
	ce.dirtyIDs[b.ID] = true
	ce.dirty = true
	ce.dataMu.Unlock()

	diff := diffSnapshot(base, ce)
	releaseSpeculativeStoreLease(ce)
	done := make(chan bool, 1)
	go func() { done <- e.commitFabricSnapshotDiff(base, diff, ce) }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("cross-shard speculative commit unexpectedly conflicted")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cross-shard commit deadlocked while acquiring physical owner locks")
	}

	gotA, err := ownerA.resolveIDLocal(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := ownerB.resolveIDLocal(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotA.State["value"] != "a2" || gotB.State["value"] != "b2" {
		t.Fatalf("cross-shard commit did not update both owners: A=%v B=%v", gotA.State, gotB.State)
	}
}
