package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

var automaticShardMu sync.Mutex

const minimumAutomaticShardFreeBytes int64 = 5 << 30

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

func automaticShardMinFreeBytes() int64 {
	raw := strings.TrimSpace(os.Getenv("MEMORYAI_SHARD_MIN_FREE_BYTES"))
	if raw == "" {
		return minimumAutomaticShardFreeBytes
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < minimumAutomaticShardFreeBytes {
		return minimumAutomaticShardFreeBytes
	}
	return n
}

func physicalFreeBytes(path string) (int64, error) {
	path = filepath.Clean(path)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	} else if err != nil && os.IsNotExist(err) {
		path = filepath.Dir(path)
	} else if err != nil {
		return 0, err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	free := uint64(stat.Bavail) * uint64(stat.Bsize)
	if free > uint64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1), nil
	}
	return int64(free), nil
}

func ensureAutomaticShardDiskBudget(e *Engine) error {
	if e == nil || strings.TrimSpace(e.bodyPath) == "" {
		return fmt.Errorf("automatic shard expansion requires physical body path")
	}
	free, err := physicalFreeBytes(e.bodyPath)
	if err != nil {
		return fmt.Errorf("automatic shard expansion cannot read free disk: %w", err)
	}
	minimum := automaticShardMinFreeBytes()
	if free < minimum {
		return fmt.Errorf("automatic shard expansion requires at least %d free bytes on current Memory path; available=%d", minimum, free)
	}
	return nil
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
	preferred := e.spaces[e.writeSpace]
	e.spaceMu.RUnlock()
	if preferred != nil && preferred.manifest.Role == "storage" && shardHasCapacity(preferred, 1) {
		return preferred
	}
	if shardHasCapacity(e, 1) {
		return e
	}
	e.spaceMu.RLock()
	paths := make([]string, 0, len(e.spaces))
	for p := range e.spaces {
		paths = append(paths, p)
	}
	e.spaceMu.RUnlock()
	sort.Strings(paths)
	for _, p := range paths {
		e.spaceMu.RLock()
		sp := e.spaces[p]
		e.spaceMu.RUnlock()
		if sp != nil && sp.manifest.Role == "storage" && shardHasCapacity(sp, 1) {
			e.spaceMu.Lock()
			e.writeSpace = p
			e.spaceMu.Unlock()
			return sp
		}
	}
	return nil
}

func (e *Engine) placeRuntimeMemoryLocked(m *Memory) error {
	if e == nil || m == nil || strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("runtime Memory placement requires engine and id")
	}
	if target := e.mountedWritableShard(); target != nil {
		target.addRuntimeMemory(m)
		return nil
	}
	return fmt.Errorf("Memory Fabric capacity exhausted: Memory must create/select storage explicitly before creating another Memory")
}

func (e *Engine) placeRuntimeMemory(m *Memory) error {
	automaticShardMu.Lock()
	defer automaticShardMu.Unlock()
	return e.placeRuntimeMemoryLocked(m)
}
