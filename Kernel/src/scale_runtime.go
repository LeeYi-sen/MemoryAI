package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
)

// localMemoryCountFast returns the local live-record count without enumerating
// the persisted Memory body. Cost is O(number of deletion deltas), never O(N).
func localMemoryCountFast(e *Engine) int {
	if e == nil {
		return 0
	}
	base := 0
	if e.store != nil {
		base = e.store.memoryCount
	}

	e.dataMu.RLock()
	newCount := 0
	for id := range e.newIDs {
		if !e.deletedIDs[id] {
			newCount++
		}
	}
	deleted := make([]string, 0, len(e.deletedIDs))
	for id, yes := range e.deletedIDs {
		if yes {
			deleted = append(deleted, id)
		}
	}
	e.dataMu.RUnlock()

	deletedPersisted := 0
	if e.store != nil {
		for _, id := range deleted {
			if _, err := e.store.GetID(id); err == nil {
				deletedPersisted++
			} else if err != io.EOF {
				// Counting is diagnostic/physical telemetry. On index read failure,
				// preserve a conservative non-negative estimate instead of scanning.
				continue
			}
		}
	}
	count := base + newCount - deletedPersisted
	if count < 0 {
		return 0
	}
	return count
}

func legacyBodyListMax() int {
	const defaultMax = 4096
	raw := os.Getenv("MEMORYAI_BODY_LIST_MAX")
	if raw == "" {
		return defaultMax
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultMax
	}
	return n
}

// boundedLegacyBodyIDs preserves compatibility for small historical Memory
// bodies while preventing executable Memory from turning body_list into an
// unbounded whole-body cognition primitive. Large bodies must use exact/tag/
// activation indexes. A configured max of zero disables legacy body_list.
func boundedLegacyBodyIDs(e *Engine) ([]string, error) {
	max := legacyBodyListMax()
	if max == 0 {
		return nil, fmt.Errorf("body_list disabled; use exact/tag/activation indexing")
	}
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("body_list requires local indexed store")
	}

	// Check persisted cardinality before AllIDs so a large Store is rejected
	// without touching its record index sequentially.
	if e.store.memoryCount > max {
		return nil, fmt.Errorf(
			"body_list bounded at %d persisted memories; use exact/tag/activation indexing",
			max,
		)
	}
	e.dataMu.RLock()
	newCount := len(e.newIDs)
	e.dataMu.RUnlock()
	if e.store.memoryCount+newCount > max {
		return nil, fmt.Errorf(
			"body_list bounded at %d memories; use exact/tag/activation indexing",
			max,
		)
	}
	return e.localIDs()
}
