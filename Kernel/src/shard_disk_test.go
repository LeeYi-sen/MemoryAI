package main

import (
	"fmt"
	"os"
	"testing"
)

func TestAutomaticShardMinimumCannotDropBelowFiveGiB(t *testing.T) {
	t.Setenv("MEMORYAI_SHARD_MIN_FREE_BYTES", "1")
	if got := automaticShardMinFreeBytes(); got != minimumAutomaticShardFreeBytes {
		t.Fatalf("minimum free-space floor drifted: got=%d want=%d", got, minimumAutomaticShardFreeBytes)
	}
}

func TestAutomaticShardDiskBudgetCanFailClosed(t *testing.T) {
	dir := t.TempDir()
	var statTarget = dir
	free, err := physicalFreeBytes(statTarget)
	if err != nil {
		t.Fatal(err)
	}
	if free <= 0 {
		t.Fatalf("unexpected free bytes: %d", free)
	}
	// Set a floor above the observed disk capacity while staying inside int64.
	floor := free + 1
	if floor < minimumAutomaticShardFreeBytes {
		floor = minimumAutomaticShardFreeBytes
	}
	if floor <= free {
		t.Skip("filesystem free-space value cannot be exceeded safely on this runner")
	}
	t.Setenv("MEMORYAI_SHARD_MIN_FREE_BYTES", fmt.Sprint(floor))
	e := &Engine{bodyPath: dir + string(os.PathSeparator) + "Memory.mem"}
	if err := ensureAutomaticShardDiskBudget(e); err == nil {
		t.Fatal("expected automatic shard expansion to fail closed below configured free-space floor")
	}
}
