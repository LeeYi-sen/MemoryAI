package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// automaticShardMu serializes physical placement so concurrent memory_new
// operations cannot race past the per-body bound or create duplicate shards.
// This is storage coordination only; Memory decides that a new Memory should
// exist, Kernel only chooses a bounded physical container for its bytes.
var automaticShardMu sync.Mutex

func memoryShardMax() int {
	const defaultMax = 4096
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_SHARD_MAX_MEMORIES"))
	if raw == "" {
		return defaultMax
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultMax
	}
	return n
}

func shardHasCapacity(e *Engine, additional int) bool {
	if e == nil {
		return false
	}
	if additional < 0 {
		additional = 0
	}
	return localMemoryCountFast(e)+additional <= memoryShardMax()
}

func automaticShardPaths(e *Engine) ([]string, error) {
	if e == nil {
		return nil, fmt.Errorf("automatic shard discovery requires engine")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(e.bodyPath), "Memory.*.mem"))
	if err != nil {
		return nil, err
	}
	primary, _ := filepath.Abs(e.bodyPath)
	out := make([]string, 0, len(matches))
	for _, p := range matches {
		cp, err := filepath.Abs(p)
		if err != nil || filepath.Clean(cp) == filepath.Clean(primary) {
			continue
		}
		out = append(out, cp)
	}
	sort.Strings(out)
	return out, nil
}

// mountAutomaticStorageShards recovers the physical shard set after restart.
// It never invents semantic links and ignores non-storage bodies.
func (e *Engine) mountAutomaticStorageShards() {
	if e == nil || e.manifest.Role != "core" {
		return
	}
	paths, err := automaticShardPaths(e)
	if err != nil {
		return
	}
	for _, p := range paths {
		if _, err := e.mountSpace(p); err != nil {
			continue
		}
		e.spaceMu.RLock()
		sp := e.spaces[p]
		e.spaceMu.RUnlock()
		if sp != nil && sp.manifest.Role != "storage" {
			_ = e.unmountSpace(p)
		}
	}
}

func (e *Engine) mountedWritableShard() *Engine {
	if e == nil {
		return nil
	}
	e.spaceMu.RLock()
	preferredPath := e.writeSpace
	preferred := e.spaces[preferredPath]
	paths := make([]string, 0, len(e.spaces))
	for p := range e.spaces {
		paths = append(paths, p)
	}
	e.spaceMu.RUnlock()

	if preferred != nil && preferred.manifest.Role == "storage" && shardHasCapacity(preferred, 1) {
		return preferred
	}
	if shardHasCapacity(e, 1) {
		return e
	}
	sort.Strings(paths)
	for _, p := range paths {
		e.spaceMu.RLock()
		sp := e.spaces[p]
		e.spaceMu.RUnlock()
		if sp != nil && sp.manifest.Role == "storage" && shardHasCapacity(sp, 1) {
			return sp
		}
	}
	return nil
}

func (e *Engine) createAutomaticWritableShard() (*Engine, error) {
	locator, err := e.createAndMountSpace("")
	if err != nil {
		return nil, err
	}
	cp, err := e.localLocatorPath(locator)
	if err != nil {
		return nil, err
	}
	e.spaceMu.Lock()
	sp := e.spaces[cp]
	if sp != nil {
		e.writeSpace = cp
	}
	e.spaceMu.Unlock()
	if sp == nil {
		return nil, fmt.Errorf("automatic shard %s mounted without engine", locator)
	}
	if sp.manifest.Role != "storage" {
		return nil, fmt.Errorf("automatic shard %s is not passive storage", locator)
	}
	if !shardHasCapacity(sp, 1) {
		return nil, fmt.Errorf("new automatic shard %s has no physical capacity", locator)
	}
	return sp, nil
}

// placeRuntimeMemory atomically chooses/creates one bounded physical shard and
// inserts a newly-created Memory. A failed expansion never falls back to an
// already-full body, preserving the bounded-write invariant.
func (e *Engine) placeRuntimeMemory(m *Memory) error {
	if e == nil || m == nil || strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("runtime Memory placement requires engine and id")
	}
	automaticShardMu.Lock()
	defer automaticShardMu.Unlock()

	// Pick up shards created by an earlier process/restart before allocating a
	// new file. Number of shards grows with physical containers, not Memories.
	e.mountAutomaticStorageShards()
	if target := e.mountedWritableShard(); target != nil {
		target.addRuntimeMemory(m)
		return nil
	}
	target, err := e.createAutomaticWritableShard()
	if err != nil {
		return fmt.Errorf("Memory shard expansion failed: %w", err)
	}
	target.addRuntimeMemory(m)
	return nil
}
