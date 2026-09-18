package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	legacyGrowthCoreID       = "__memoryai.growth.core"
	legacyGrowthMigrationTag = "memory-legacy-growth-migrated"
	legacyGrowthMigrationID  = "__memoryai.legacy-growth.migration"
)

type legacyMeshJournalDisk struct {
	Version int           `json:"version"`
	NodeID  string        `json:"node_id"`
	Entries []MeshRequest `json:"entries"`
}

type legacyMeshProposalReplayDisk struct {
	Version int                       `json:"version"`
	Entries []meshProposalReplayEntry `json:"entries"`
}

func legacyPayloadID(kind, identity string, raw []byte) string {
	sum := sha256.Sum256(append([]byte(kind+"\x00"+identity+"\x00"), raw...))
	return "legacy-growth-" + kind + "-" + hex.EncodeToString(sum[:16])
}

func legacyPayloadState(kind, identity string, value any) (map[string]any, []byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	state := map[string]any{
		"legacy_kind": kind,
		"legacy_id":   identity,
		"payload":     string(raw),
	}
	if obj, ok := value.(map[string]any); ok {
		for key, item := range obj {
			switch item.(type) {
			case string, float64, bool, nil:
				state[key] = item
			}
		}
	}
	return state, raw, nil
}

func legacyIdentity(value any, fallback string) string {
	if obj, ok := value.(map[string]any); ok {
		for _, key := range []string{"id", "candidate_id", "experience_id"} {
			if raw, exists := obj[key]; exists {
				if id := strings.TrimSpace(fmt.Sprint(raw)); id != "" {
					return id
				}
			}
		}
	}
	return fallback
}

func upsertLegacyGrowthPayload(e *Engine, kind, identity string, value any) (string, error) {
	state, raw, err := legacyPayloadState(kind, identity, value)
	if err != nil {
		return "", err
	}
	id := legacyPayloadID(kind, identity, raw)
	if _, existing, er := e.resolveLocalFabricMemory(id); er == nil && existing != nil {
		return id, nil
	} else if er != nil && !errors.Is(er, io.EOF) {
		return "", er
	}
	m := &Memory{
		ID:       id,
		Layer:    "acquired",
		Tags:     []string{"memory", legacyGrowthMigrationTag, "legacy-growth-" + kind},
		Content:  "Migrated historical Growth state; preserved as discrete Memory rather than one monolithic JSON record.",
		Revision: 1,
		State:    state,
	}
	if err := e.upsertExplicitMemoryBounded(m); err != nil {
		return "", err
	}
	return id, nil
}

func migrateLegacyGrowthCore(e *Engine) (bool, error) {
	if e == nil {
		return false, nil
	}
	_, core, err := e.resolveLocalFabricMemory(legacyGrowthCoreID)
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if core == nil || core.State == nil {
		return false, errors.New("legacy growth core exists without State; refusing partial migration")
	}
	value, ok := core.State["memory_growth_state"]
	if !ok {
		return false, errors.New("legacy growth core exists without memory_growth_state; refusing partial migration")
	}
	var raw []byte
	switch v := value.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = append([]byte(nil), v...)
	default:
		raw, err = json.Marshal(v)
		if err != nil {
			return false, err
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, fmt.Errorf("decode legacy growth core: %w", err)
	}
	counts := map[string]int{}
	migrateArray := func(field, kind string) error {
		items, _ := doc[field].([]any)
		for i, item := range items {
			identity := legacyIdentity(item, fmt.Sprintf("%s-%d", field, i))
			if _, err := upsertLegacyGrowthPayload(e, kind, identity, item); err != nil {
				return err
			}
			counts[kind]++
		}
		return nil
	}
	for _, item := range []struct{ field, kind string }{
		{"experiences", "experience"},
		{"candidates", "candidate"},
		{"pending_structures", "structure"},
	} {
		if err := migrateArray(item.field, item.kind); err != nil {
			return false, err
		}
	}
	if histories, ok := doc["experience_validations"].(map[string]any); ok {
		keys := make([]string, 0, len(histories))
		for key := range histories {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			items, _ := histories[key].([]any)
			for i, item := range items {
				identity := key + ":" + fmt.Sprint(i)
				if _, err := upsertLegacyGrowthPayload(e, "experience-validation", identity, item); err != nil {
					return false, err
				}
				counts["experience-validation"]++
			}
		}
	}
	if histories, ok := doc["structure_validation_history"].(map[string]any); ok {
		keys := make([]string, 0, len(histories))
		for key := range histories {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			items, _ := histories[key].([]any)
			for i, item := range items {
				identity := key + ":" + fmt.Sprint(i)
				if _, err := upsertLegacyGrowthPayload(e, "structure-validation", identity, item); err != nil {
					return false, err
				}
				counts["structure-validation"]++
			}
		}
	}
	if order, ok := doc["experience_order"].([]any); ok {
		for i, item := range order {
			identity := fmt.Sprintf("%09d:%s", i, strings.TrimSpace(fmt.Sprint(item)))
			value := map[string]any{"index": i, "experience_id": strings.TrimSpace(fmt.Sprint(item))}
			if _, err := upsertLegacyGrowthPayload(e, "experience-order", identity, value); err != nil {
				return false, err
			}
			counts["experience-order"]++
		}
	}
	if validated, ok := doc["validated_candidate_ids"].([]any); ok {
		for i, item := range validated {
			candidateID := strings.TrimSpace(fmt.Sprint(item))
			identity := fmt.Sprintf("%09d:%s", i, candidateID)
			value := map[string]any{"index": i, "candidate_id": candidateID, "validated": true}
			if _, err := upsertLegacyGrowthPayload(e, "validated-candidate", identity, value); err != nil {
				return false, err
			}
			counts["validated-candidate"]++
		}
	}
	migrationState := map[string]any{
		"source_id":           legacyGrowthCoreID,
		"source_revision":     core.Revision,
		"experience_next_seq": doc["experience_next_seq"],
		"counts":              counts,
		"format":              "discrete-memory-v2",
	}
	migration := &Memory{
		ID:       legacyGrowthMigrationID,
		Layer:    "emergent",
		Tags:     []string{"memory", legacyGrowthMigrationTag},
		Content:  "One-time migration marker for historical monolithic Growth state.",
		Revision: 1,
		State:    migrationState,
	}
	if _, current, er := e.resolveLocalFabricMemory(legacyGrowthMigrationID); er == nil && current != nil {
		migration.Revision = current.Revision + 1
	} else if er != nil && !errors.Is(er, io.EOF) {
		return false, er
	}
	if err := e.upsertExplicitMemoryBounded(migration); err != nil {
		return false, err
	}
	if err := e.deleteExplicitMemoryBounded(legacyGrowthCoreID); err != nil {
		return false, err
	}
	return true, nil
}

func mergeDeferredRequests(existing, incoming []MeshRequest) []MeshRequest {
	out := append([]MeshRequest(nil), existing...)
	index := map[string]int{}
	for i, req := range out {
		index[meshJournalLogicalKey(req)] = i
	}
	for _, req := range incoming {
		key := meshJournalLogicalKey(req)
		if i, ok := index[key]; ok {
			out[i] = req
			continue
		}
		index[key] = len(out)
		out = append(out, req)
	}
	return out
}

func importLegacyDeferredJournal(e *Engine, raw []byte) error {
	var disk legacyMeshJournalDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		return err
	}
	if strings.TrimSpace(disk.NodeID) == "" {
		return errors.New("legacy mesh deferred journal missing node_id")
	}
	id := meshDeferredJournalMemoryIDForNode(disk.NodeID)
	existingEntries := []MeshRequest(nil)
	revision := uint64(1)
	if _, current, err := e.resolveLocalFabricMemory(id); err == nil && current != nil {
		revision = current.Revision + 1
		decoded, _, err := decodeMeshJournalMemory(current, hardMeshJournalMaxEntries, hardMeshJournalMaxBytes)
		if err != nil {
			return err
		}
		existingEntries = decoded.Entries
	} else if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	_, err := upsertMeshDeferredJournalMemory(e, disk.NodeID, mergeDeferredRequests(existingEntries, disk.Entries), revision)
	return err
}

func importLegacyProposalReplay(e *Engine, raw []byte) error {
	var disk legacyMeshProposalReplayDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		return err
	}
	for _, entry := range disk.Entries {
		if strings.TrimSpace(entry.RequestID) == "" || strings.TrimSpace(entry.Slot) == "" {
			return errors.New("legacy mesh proposal replay contains invalid entry")
		}
		if _, _, found, err := loadMeshProposalReplayEntry(e, entry.RequestID); err != nil {
			return err
		} else if !found {
			memory, err := meshProposalReplayEntryMemory(entry, 1)
			if err != nil {
				return err
			}
			if err := e.upsertExplicitMemoryBounded(memory); err != nil {
				return err
			}
		}
		_, latestRevision, slotRevision, found, err := loadMeshProposalSlot(e, entry.Slot)
		if err != nil {
			return err
		}
		if !found || entry.Revision > latestRevision {
			if !found {
				slotRevision = 0
			}
			if err := e.upsertExplicitMemoryBounded(meshProposalSlotMemory(entry.Slot, entry.RequestID, entry.Revision, slotRevision+1)); err != nil {
				return err
			}
		}
	}
	return nil
}

func legacyRuntimeSidecarPaths(e *Engine) ([]string, error) {
	if e == nil || strings.TrimSpace(e.bodyPath) == "" {
		return nil, nil
	}
	dir := filepath.Dir(canonicalPhysicalBodyPath(e.bodyPath))
	patterns := []string{
		filepath.Join(dir, "Memory.mesh-journal.*.json"),
		filepath.Join(dir, "Memory.mesh-proposal-replay.*.json"),
	}
	set := map[string]bool{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			set[path] = true
		}
	}
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func migrateLegacyRuntimeSidecars(e *Engine) ([]string, bool, error) {
	paths, err := legacyRuntimeSidecarPaths(e)
	if err != nil {
		return nil, false, err
	}
	changed := false
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, changed, err
		}
		base := filepath.Base(path)
		switch {
		case strings.HasPrefix(base, "Memory.mesh-journal."):
			err = importLegacyDeferredJournal(e, raw)
		case strings.HasPrefix(base, "Memory.mesh-proposal-replay."):
			err = importLegacyProposalReplay(e, raw)
		default:
			continue
		}
		if err != nil {
			return nil, changed, fmt.Errorf("migrate legacy runtime sidecar %s: %w", base, err)
		}
		changed = true
	}
	return paths, changed, nil
}

func removeMigratedSidecars(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	dirs := map[string]bool{}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		dirs[filepath.Dir(path)] = true
	}
	for dir := range dirs {
		d, err := os.Open(dir)
		if err != nil {
			return err
		}
		err = d.Sync()
		_ = d.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyStorageNamespace(e *Engine) (bool, error) {
	if e == nil {
		return false, nil
	}
	memories, err := e.allMemories()
	if err != nil {
		return false, err
	}
	changed := false
	for _, memory := range memories {
		if memory == nil {
			continue
		}
		tags := make([]string, 0, len(memory.Tags))
		memoryChanged := false
		for _, tag := range memory.Tags {
			next := tag
			switch {
			case tag == "cog.storage.remote.endpoint":
				next = "physical.storage.remote.endpoint"
			case strings.HasPrefix(tag, "cog.storage.replica.of."):
				next = "physical.storage.replica.of." + strings.TrimPrefix(tag, "cog.storage.replica.of.")
			}
			if next != tag {
				memoryChanged = true
			}
			if !contains(tags, next) {
				tags = append(tags, next)
			}
		}
		if !memoryChanged {
			continue
		}
		q := copyMemory(memory)
		q.Tags = tags
		q.CapabilitySig = ""
		q.Revision++
		if err := e.upsertExplicitMemoryBounded(q); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// migrateLegacyRuntimeState is a one-way physical format migration. It contains
// no learning policy: it only preserves historical bytes as ordinary Memory,
// persists them into memory.mem, then removes obsolete runtime sidecars.
func migrateLegacyRuntimeState(e *Engine) error {
	if e == nil {
		return nil
	}
	root := fabricRootFor(e)
	if root == nil {
		root = e
	}
	growthChanged, err := migrateLegacyGrowthCore(root)
	if err != nil {
		return err
	}
	storageNamespaceChanged, err := migrateLegacyStorageNamespace(root)
	if err != nil {
		return err
	}
	cleanup, sidecarChanged, err := migrateLegacyRuntimeSidecars(root)
	if err != nil {
		return err
	}
	if growthChanged || storageNamespaceChanged || sidecarChanged {
		if err := root.persistAll(); err != nil {
			return fmt.Errorf("persist legacy runtime migration into memory.mem: %w", err)
		}
	}
	if sidecarChanged {
		if err := removeMigratedSidecars(cleanup); err != nil {
			return fmt.Errorf("remove migrated runtime sidecars: %w", err)
		}
	}
	return nil
}
