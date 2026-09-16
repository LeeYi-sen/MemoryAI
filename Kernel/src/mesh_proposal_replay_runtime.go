package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMeshProposalReplayMaxEntries = 65536
	hardMeshProposalReplayMaxEntries    = 1 << 20
	meshProposalReplayTag               = "memory-mesh-proposal-replay"
	meshProposalReplaySlotTag           = "memory-mesh-proposal-replay-slot"
	meshProposalReplayExecuting         = "executing"
	meshProposalReplayDone              = "done"
)

type meshProposalReplayEntry struct {
	Slot        string            `json:"slot"`
	RequestID   string            `json:"request_id"`
	Revision    uint64            `json:"revision"`
	State       string            `json:"state"`
	ResultVars  map[string]string `json:"result_vars,omitempty"`
	ResultError string            `json:"result_error,omitempty"`
	UpdatedNano int64             `json:"updated_nano"`
}

type meshProposalReplayState struct {
	mu         sync.Mutex
	live       map[string]bool // slot -> executing in this process
	maxEntries int
}

var meshProposalReplayStates sync.Map // map[*Engine]*meshProposalReplayState

func meshProposalReplayRoot(e *Engine) (*Engine, error) {
	root := eventFabricRoot(e)
	if root == nil {
		return nil, errors.New("mesh proposal replay requires a bound Memory body")
	}
	return root, nil
}

func meshProposalReplayStateFor(e *Engine) (*meshProposalReplayState, error) {
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return nil, err
	}
	if raw, ok := meshProposalReplayStates.Load(root); ok {
		return raw.(*meshProposalReplayState), nil
	}
	st := &meshProposalReplayState{
		live:       map[string]bool{},
		maxEntries: boundedMeshJournalIntEnv("MEMORYAI_MESH_REPLAY_MAX_ENTRIES", defaultMeshProposalReplayMaxEntries, hardMeshProposalReplayMaxEntries),
	}
	actual, _ := meshProposalReplayStates.LoadOrStore(root, st)
	return actual.(*meshProposalReplayState), nil
}

func forgetMeshProposalReplayState(e *Engine) {
	if root, err := meshProposalReplayRoot(e); err == nil {
		meshProposalReplayStates.Delete(root)
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func meshProposalSlotID(slot string) string {
	sum := sha256.Sum256([]byte(slot))
	return "mesh-proposal-slot-" + hex.EncodeToString(sum[:16])
}

func meshProposalReceiptID(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if strings.HasPrefix(requestID, "mesh-proposal-") {
		return requestID
	}
	sum := sha256.Sum256([]byte(requestID))
	return "mesh-proposal-" + hex.EncodeToString(sum[:])
}

func meshProposalReplayEntryMemory(entry meshProposalReplayEntry, revision uint64) (*Memory, error) {
	if strings.TrimSpace(entry.RequestID) == "" || strings.TrimSpace(entry.Slot) == "" {
		return nil, errors.New("mesh proposal replay entry requires request_id and slot")
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:       meshProposalReceiptID(entry.RequestID),
		Layer:    "emergent",
		Tags:     []string{"memory", meshProposalReplayTag},
		Content:  "Durable Mesh proposal execution receipt stored inside Memory.",
		Revision: revision,
		State: map[string]any{
			"request_id":   entry.RequestID,
			"slot":         entry.Slot,
			"revision":     entry.Revision,
			"status":       entry.State,
			"updated_nano": entry.UpdatedNano,
			"receipt":      string(raw),
		},
	}, nil
}

func meshProposalSlotMemory(slot, requestID string, proposalRevision, revision uint64) *Memory {
	return &Memory{
		ID:       meshProposalSlotID(slot),
		Layer:    "emergent",
		Tags:     []string{"memory", meshProposalReplaySlotTag},
		Content:  "Latest durable revision for one Mesh proposal identity slot.",
		Revision: revision,
		State: map[string]any{
			"slot":              slot,
			"request_id":        requestID,
			"proposal_revision": proposalRevision,
		},
	}
}

func decodeMeshProposalReplayEntry(memory *Memory) (meshProposalReplayEntry, error) {
	var entry meshProposalReplayEntry
	if memory == nil || !memoryHasTag(memory, meshProposalReplayTag) {
		return entry, errors.New("mesh proposal replay Memory unavailable")
	}
	value, ok := memory.State["receipt"]
	if !ok {
		return entry, errors.New("mesh proposal replay Memory missing receipt")
	}
	var raw []byte
	switch v := value.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = append([]byte(nil), v...)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return entry, err
		}
		raw = b
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return entry, err
	}
	entry.ResultVars = cloneStringMap(entry.ResultVars)
	return entry, nil
}

func loadMeshProposalReplayEntry(e *Engine, requestID string) (meshProposalReplayEntry, uint64, bool, error) {
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return meshProposalReplayEntry{}, 0, false, err
	}
	id := meshProposalReceiptID(requestID)
	_, memory, err := root.resolveLocalFabricMemory(id)
	if errors.Is(err, io.EOF) {
		return meshProposalReplayEntry{}, 0, false, nil
	}
	if err != nil {
		return meshProposalReplayEntry{}, 0, false, err
	}
	entry, err := decodeMeshProposalReplayEntry(memory)
	if err != nil {
		return meshProposalReplayEntry{}, 0, false, err
	}
	return entry, memory.Revision, true, nil
}

func loadMeshProposalSlot(e *Engine, slot string) (requestID string, proposalRevision, memoryRevision uint64, found bool, err error) {
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return "", 0, 0, false, err
	}
	_, memory, err := root.resolveLocalFabricMemory(meshProposalSlotID(slot))
	if errors.Is(err, io.EOF) {
		return "", 0, 0, false, nil
	}
	if err != nil {
		return "", 0, 0, false, err
	}
	if memory == nil || !memoryHasTag(memory, meshProposalReplaySlotTag) {
		return "", 0, 0, false, fmt.Errorf("mesh proposal replay slot collides with non-slot Memory: %s", meshProposalSlotID(slot))
	}
	storedSlot := strings.TrimSpace(fmt.Sprint(memory.State["slot"]))
	if storedSlot != slot {
		return "", 0, 0, false, errors.New("mesh proposal replay slot identity drift")
	}
	requestID = strings.TrimSpace(fmt.Sprint(memory.State["request_id"]))
	proposalRevision, err = meshScalarUint64(memory.State["proposal_revision"])
	if err != nil {
		return "", 0, 0, false, err
	}
	return requestID, proposalRevision, memory.Revision, true, nil
}

func meshScalarUint64(v any) (uint64, error) {
	switch x := v.(type) {
	case uint64:
		return x, nil
	case uint:
		return uint64(x), nil
	case int:
		if x < 0 {
			return 0, errors.New("negative uint64 value")
		}
		return uint64(x), nil
	case int64:
		if x < 0 {
			return 0, errors.New("negative uint64 value")
		}
		return uint64(x), nil
	case float64:
		if x < 0 {
			return 0, errors.New("negative uint64 value")
		}
		return uint64(x), nil
	case string:
		return strconv.ParseUint(strings.TrimSpace(x), 10, 64)
	default:
		return strconv.ParseUint(strings.TrimSpace(fmt.Sprint(v)), 10, 64)
	}
}

func persistMeshProposalReplayEntry(e *Engine, entry meshProposalReplayEntry, receiptRevision uint64, updateSlot bool) error {
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return err
	}
	memory, err := meshProposalReplayEntryMemory(entry, receiptRevision)
	if err != nil {
		return err
	}
	if err := root.upsertExplicitMemoryBounded(memory); err != nil {
		return err
	}
	if updateSlot {
		_, _, slotRevision, found, err := loadMeshProposalSlot(root, entry.Slot)
		if err != nil {
			return err
		}
		if !found {
			slotRevision = 0
		}
		if err := root.upsertExplicitMemoryBounded(meshProposalSlotMemory(entry.Slot, entry.RequestID, entry.Revision, slotRevision+1)); err != nil {
			return err
		}
	}
	return root.persistAll()
}

type meshProposalIdentity struct {
	Event          string   `json:"event"`
	Subject        string   `json:"subject"`
	MemoryID       string   `json:"memory_id"`
	OriginNode     string   `json:"origin_node"`
	ProposalDigest string   `json:"proposal_digest"`
	Revision       uint64   `json:"revision"`
	Tags           []string `json:"tags,omitempty"`
}

func meshProposalIdentityFromFrame(f *Frame, ev PhysicalEvent) (meshProposalIdentity, string, string, error) {
	if f == nil {
		return meshProposalIdentity{}, "", "", errors.New("mesh proposal replay requires frame")
	}
	memoryID := strings.TrimSpace(f.Vars["memory_id"])
	originNode := strings.TrimSpace(f.Vars["origin_node"])
	if memoryID == "" || originNode == "" {
		return meshProposalIdentity{}, "", "", errors.New("mesh proposal replay requires memory_id and origin_node")
	}
	revision, err := strconv.ParseUint(strings.TrimSpace(f.Vars["revision"]), 10, 64)
	if err != nil {
		return meshProposalIdentity{}, "", "", fmt.Errorf("mesh proposal replay invalid revision: %w", err)
	}
	tags := append([]string(nil), f.Lists["tags"]...)
	sort.Strings(tags)
	identity := meshProposalIdentity{
		Event:          strings.TrimSpace(ev.Name),
		Subject:        strings.TrimSpace(ev.Subject),
		MemoryID:       memoryID,
		OriginNode:     originNode,
		ProposalDigest: strings.TrimSpace(f.Vars["proposal_digest"]),
		Revision:       revision,
		Tags:           tags,
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		return meshProposalIdentity{}, "", "", err
	}
	sum := sha256.Sum256(raw)
	requestID := "mesh-proposal-" + hex.EncodeToString(sum[:])
	slot := originNode + "\x00" + memoryID
	return identity, slot, requestID, nil
}

func applyMeshProposalReplayResult(f *Frame, entry meshProposalReplayEntry) error {
	if f == nil {
		return errors.New("mesh proposal replay requires frame")
	}
	f.Vars["mesh_request_id"] = entry.RequestID
	for k, v := range entry.ResultVars {
		f.Vars[k] = v
	}
	if entry.ResultError != "" {
		return errors.New(entry.ResultError)
	}
	return nil
}

func prepareMeshProposalReplay(e *Engine, f *Frame, ev PhysicalEvent) (*meshProposalReplayState, meshProposalReplayEntry, uint64, bool, error) {
	identity, slot, requestID, err := meshProposalIdentityFromFrame(f, ev)
	if err != nil {
		return nil, meshProposalReplayEntry{}, 0, false, err
	}
	st, err := meshProposalReplayStateFor(e)
	if err != nil {
		return nil, meshProposalReplayEntry{}, 0, false, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	if previous, revision, found, err := loadMeshProposalReplayEntry(e, requestID); err != nil {
		return st, meshProposalReplayEntry{}, 0, false, err
	} else if found {
		if previous.Slot != slot || previous.Revision != identity.Revision {
			return st, previous, revision, true, errors.New("mesh proposal replay request identity collision")
		}
		if previous.State == meshProposalReplayDone {
			return st, previous, revision, true, nil
		}
		return st, previous, revision, true, fmt.Errorf("mesh proposal request %s is durably fenced as in-flight; refusing duplicate execution", requestID)
	}
	if st.live[slot] {
		return st, meshProposalReplayEntry{}, 0, false, fmt.Errorf("mesh proposal slot %q already executing", slot)
	}
	if _, latestRevision, _, found, err := loadMeshProposalSlot(e, slot); err != nil {
		return st, meshProposalReplayEntry{}, 0, false, err
	} else if found {
		if identity.Revision < latestRevision {
			return st, meshProposalReplayEntry{}, 0, false, fmt.Errorf("stale unseen mesh proposal revision %d < %d", identity.Revision, latestRevision)
		}
		if identity.Revision == latestRevision {
			return st, meshProposalReplayEntry{}, 0, false, fmt.Errorf("conflicting mesh proposal identity at revision %d", identity.Revision)
		}
	}
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return st, meshProposalReplayEntry{}, 0, false, err
	}
	ids, err := root.listTagFabric(meshProposalReplayTag)
	if err != nil {
		return st, meshProposalReplayEntry{}, 0, false, err
	}
	if len(ids) >= st.maxEntries {
		reclaimed, reclaimErr := reclaimOneSupersededMeshProposalReplayReceipt(root, ids)
		if reclaimErr != nil {
			return st, meshProposalReplayEntry{}, 0, false, reclaimErr
		}
		if reclaimed {
			ids, err = root.listTagFabric(meshProposalReplayTag)
			if err != nil {
				return st, meshProposalReplayEntry{}, 0, false, err
			}
		}
	}
	if len(ids) >= st.maxEntries {
		return st, meshProposalReplayEntry{}, 0, false, fmt.Errorf("mesh proposal replay ledger full: %d entries (limit %d)", len(ids), st.maxEntries)
	}
	entry := meshProposalReplayEntry{
		Slot:        slot,
		RequestID:   requestID,
		Revision:    identity.Revision,
		State:       meshProposalReplayExecuting,
		UpdatedNano: time.Now().UnixNano(),
	}
	if err := persistMeshProposalReplayEntry(root, entry, 1, true); err != nil {
		return st, meshProposalReplayEntry{}, 0, false, err
	}
	st.live[slot] = true
	return st, entry, 1, false, nil
}

func meshProposalResultVars(f *Frame) map[string]string {
	if f == nil {
		return nil
	}
	out := map[string]string{}
	for _, key := range []string{"mesh_decision", "decision", "mesh_reason", "reason"} {
		if value, ok := f.Vars[key]; ok {
			out[key] = value
		}
	}
	return out
}

func finalizeMeshProposalReplay(e *Engine, st *meshProposalReplayState, entry meshProposalReplayEntry, receiptRevision uint64, f *Frame, runErr error) error {
	if st == nil {
		return errors.New("mesh proposal replay state unavailable")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	current, currentRevision, found, err := loadMeshProposalReplayEntry(e, entry.RequestID)
	if err != nil {
		delete(st.live, entry.Slot)
		return err
	}
	if !found || current.Slot != entry.Slot || currentRevision != receiptRevision {
		delete(st.live, entry.Slot)
		return errors.New("mesh proposal replay identity changed during execution")
	}
	done := current
	done.State = meshProposalReplayDone
	done.ResultVars = meshProposalResultVars(f)
	if runErr != nil {
		done.ResultError = runErr.Error()
	}
	done.UpdatedNano = time.Now().UnixNano()
	if err := persistMeshProposalReplayEntry(e, done, currentRevision+1, false); err != nil {
		delete(st.live, entry.Slot)
		return err
	}
	delete(st.live, entry.Slot)
	return nil
}

func executeMeshProposalEventOnce(e *Engine, f *Frame, ev PhysicalEvent, dispatch func() error) error {
	if dispatch == nil {
		return errors.New("mesh proposal replay requires dispatch function")
	}
	st, entry, receiptRevision, replay, err := prepareMeshProposalReplay(e, f, ev)
	if replay {
		if err != nil {
			f.Vars["mesh_request_id"] = entry.RequestID
			return err
		}
		return applyMeshProposalReplayResult(f, entry)
	}
	if err != nil {
		return err
	}
	f.Vars["mesh_request_id"] = entry.RequestID
	runErr := dispatch()
	if persistErr := finalizeMeshProposalReplay(e, st, entry, receiptRevision, f, runErr); persistErr != nil {
		return fmt.Errorf("mesh proposal event executed but replay result was not durably recorded in memory.mem; request remains fenced: %w", persistErr)
	}
	return runErr
}
