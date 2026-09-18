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

func loadMeshProposalAckedThrough(e *Engine, slot string) (uint64, error) {
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return 0, err
	}
	_, memory, err := root.resolveLocalFabricMemory(meshProposalSlotID(slot))
	if errors.Is(err, io.EOF) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if memory == nil || !memoryHasTag(memory, meshProposalReplaySlotTag) {
		return 0, errors.New("mesh proposal ACK fence slot unavailable")
	}
	value, ok := memory.State["acked_through_revision"]
	if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
		return 0, nil
	}
	return meshScalarUint64(value)
}

func persistMeshProposalAckFence(e *Engine, slot, receiptID string, proposalRevision uint64) error {
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return err
	}
	_, memory, err := root.resolveLocalFabricMemory(meshProposalSlotID(slot))
	if err != nil {
		return err
	}
	if memory == nil || !memoryHasTag(memory, meshProposalReplaySlotTag) {
		return errors.New("mesh proposal ACK fence slot unavailable")
	}
	storedSlot := strings.TrimSpace(fmt.Sprint(memory.State["slot"]))
	if storedSlot != slot {
		return errors.New("mesh proposal ACK fence slot identity drift")
	}
	ackedThrough, err := loadMeshProposalAckedThrough(root, slot)
	if err != nil {
		return err
	}
	if proposalRevision > ackedThrough {
		ackedThrough = proposalRevision
	}
	q := copyMemory(memory)
	if q.State == nil {
		q.State = map[string]any{}
	}
	q.State["acked_through_revision"] = ackedThrough
	q.State["acked_request_id"] = receiptID
	q.State["acked_nano"] = time.Now().UnixNano()
	q.CapabilitySig = ""
	q.Revision++
	if err := root.upsertExplicitMemoryBounded(q); err != nil {
		return err
	}
	return root.persistAll()
}

func deleteMeshProposalReceiptDurably(e *Engine, receiptID string) error {
	root, err := meshProposalReplayRoot(e)
	if err != nil {
		return err
	}
	if err := root.explicitDeleteMemory(meshProposalReceiptID(receiptID)); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if err := root.persistAll(); err != nil {
		// Keep the delete dirty and cross the persistence barrier once more. If
		// both barriers fail, restart can only resurrect a done receipt, never
		// an unfenced execution.
		if retryErr := root.persistAll(); retryErr != nil {
			return fmt.Errorf("persist Mesh proposal receipt GC: first=%v retry=%w", err, retryErr)
		}
	}
	return nil
}

func ackMeshProposalReplay(e *Engine, req MeshRequest) (string, error) {
	receiptID := strings.TrimSpace(req.ReceiptID)
	memoryID := strings.TrimSpace(req.MemoryID)
	originNode := strings.TrimSpace(req.OriginNode)
	if receiptID == "" || memoryID == "" || originNode == "" || req.Revision == 0 {
		return "", errors.New("mesh proposal ACK requires receipt_id, memory_id, origin_node and revision")
	}
	st, err := meshProposalReplayStateFor(e)
	if err != nil {
		return "", err
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	slot := originNode + "\x00" + memoryID
	slotRequestID, slotRevision, _, slotFound, err := loadMeshProposalSlot(e, slot)
	if err != nil {
		return "", err
	}
	if !slotFound {
		return "", errors.New("mesh proposal ACK missing durable slot fence")
	}
	if req.Revision > slotRevision {
		return "", fmt.Errorf("mesh proposal ACK revision %d is ahead of durable slot revision %d", req.Revision, slotRevision)
	}

	entry, _, found, err := loadMeshProposalReplayEntry(e, receiptID)
	if err != nil {
		return "", err
	}
	if !found {
		ackedThrough, err := loadMeshProposalAckedThrough(e, slot)
		if err != nil {
			return "", err
		}
		if ackedThrough >= req.Revision {
			return "acked-gc", nil
		}
		return "", errors.New("mesh proposal ACK receipt missing without durable ACK fence")
	}
	if entry.RequestID != receiptID || entry.Slot != slot || entry.Revision != req.Revision {
		return "", errors.New("mesh proposal ACK identity mismatch")
	}
	if entry.State != meshProposalReplayDone {
		return "", fmt.Errorf("mesh proposal ACK cannot GC receipt in state %q", entry.State)
	}
	if slotRevision == req.Revision && slotRequestID != receiptID {
		return "", errors.New("mesh proposal ACK conflicts with durable slot identity")
	}
	if err := persistMeshProposalAckFence(e, slot, receiptID, req.Revision); err != nil {
		return "", err
	}
	if err := deleteMeshProposalReceiptDurably(e, receiptID); err != nil {
		return "", err
	}
	return "acked-gc", nil
}

func (m *meshRuntime) ackSharedProposal(req MeshRequest) MeshResponse {
	if m == nil || m.role != "sovereign" {
		return MeshResponse{OK: false, Error: "mesh proposal ACK requires sovereign role"}
	}
	m.mu.RLock()
	e := m.engine
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{OK: false, Error: "sovereign engine unavailable"}
	}
	status, err := ackMeshProposalReplay(e, req)
	if err != nil {
		return MeshResponse{OK: false, Error: err.Error()}
	}
	return MeshResponse{OK: true, Status: status, ReceiptID: req.ReceiptID}
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
		var previousSlot *Memory
		if found {
			_, previousSlot, err = root.resolveLocalFabricMemory(meshProposalSlotID(entry.Slot))
			if err != nil {
				return err
			}
		} else {
			slotRevision = 0
		}
		nextSlot := meshProposalSlotMemory(entry.Slot, entry.RequestID, entry.Revision, slotRevision+1)
		if previousSlot != nil {
			for _, key := range []string{"acked_through_revision", "acked_request_id", "acked_nano"} {
				if value, ok := previousSlot.State[key]; ok {
					nextSlot.State[key] = value
				}
			}
		}
		if err := root.upsertExplicitMemoryBounded(nextSlot); err != nil {
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
	updates := make(map[string]string, len(entry.ResultVars)+1)
	updates["mesh_request_id"] = entry.RequestID
	for k, v := range entry.ResultVars {
		updates[k] = v
	}
	candidate, err := frameVarsMergeCandidate(f.Vars, updates, "Mesh replay result")
	if err != nil {
		return err
	}
	f.Vars = candidate
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
			if setErr := setFrameVarBounded(f, "mesh_request_id", entry.RequestID, "Mesh replay request id"); setErr != nil {
				return setErr
			}
			return err
		}
		return applyMeshProposalReplayResult(f, entry)
	}
	if err != nil {
		return err
	}
	if err := setFrameVarBounded(f, "mesh_request_id", entry.RequestID, "Mesh replay request id"); err != nil {
		return err
	}
	runErr := dispatch()
	if persistErr := finalizeMeshProposalReplay(e, st, entry, receiptRevision, f, runErr); persistErr != nil {
		return fmt.Errorf("mesh proposal event executed but replay result was not durably recorded in memory.mem; request remains fenced: %w", persistErr)
	}
	return runErr
}
