package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	meshJournalVersion           = 2
	defaultMeshJournalMaxEntries = 1024
	defaultMeshJournalMaxBytes   = int64(8 << 20)
	hardMeshJournalMaxEntries    = 65536
	hardMeshJournalMaxBytes      = int64(256 << 20)
	meshDeferredJournalTag       = "memory-mesh-deferred-journal"
)

type meshJournalDisk struct {
	Version int           `json:"version"`
	NodeID  string        `json:"node_id"`
	Entries []MeshRequest `json:"entries"`
}

type meshJournalState struct {
	mu         sync.Mutex
	loaded     bool
	memoryID   string
	bytes      int64
	maxEntries int
	maxBytes   int64
}

var meshJournalStates sync.Map // map[*meshRuntime]*meshJournalState

func boundedMeshJournalIntEnv(name string, fallback, hardMax int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	if n > hardMax {
		return hardMax
	}
	return n
}

func boundedMeshJournalInt64Env(name string, fallback, hardMax int64) int64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return fallback
	}
	if n > hardMax {
		return hardMax
	}
	return n
}

func meshDeferredJournalMemoryIDForNode(nodeID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(nodeID)))
	return "__memoryai.mesh.deferred." + hex.EncodeToString(sum[:12])
}

func meshJournalStateFor(m *meshRuntime) *meshJournalState {
	if m == nil {
		return nil
	}
	if raw, ok := meshJournalStates.Load(m); ok {
		return raw.(*meshJournalState)
	}
	m.mu.RLock()
	nodeID := m.nodeID
	m.mu.RUnlock()
	st := &meshJournalState{
		memoryID:   meshDeferredJournalMemoryIDForNode(nodeID),
		maxEntries: boundedMeshJournalIntEnv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", defaultMeshJournalMaxEntries, hardMeshJournalMaxEntries),
		maxBytes:   boundedMeshJournalInt64Env("MEMORYAI_MESH_JOURNAL_MAX_BYTES", defaultMeshJournalMaxBytes, hardMeshJournalMaxBytes),
	}
	actual, _ := meshJournalStates.LoadOrStore(m, st)
	return actual.(*meshJournalState)
}

func marshalMeshJournal(nodeID string, entries []MeshRequest) ([]byte, error) {
	return json.Marshal(meshJournalDisk{Version: meshJournalVersion, NodeID: nodeID, Entries: entries})
}

func decodeMeshJournalMemory(memory *Memory, maxEntries int, maxBytes int64) (meshJournalDisk, int64, error) {
	var disk meshJournalDisk
	if memory == nil {
		return disk, 0, nil
	}
	rawValue, ok := memory.State["journal_state"]
	if !ok {
		return disk, 0, errors.New("mesh deferred journal Memory missing journal_state")
	}
	var raw []byte
	switch v := rawValue.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = append([]byte(nil), v...)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return disk, 0, err
		}
		raw = encoded
	}
	if int64(len(raw)) > maxBytes {
		return disk, int64(len(raw)), fmt.Errorf("mesh deferred journal exceeds physical byte limit: %d > %d", len(raw), maxBytes)
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return disk, int64(len(raw)), fmt.Errorf("mesh deferred journal decode failed: %w", err)
	}
	if disk.Version != meshJournalVersion && disk.Version != 1 {
		return disk, int64(len(raw)), fmt.Errorf("unsupported mesh deferred journal version: %d", disk.Version)
	}
	if len(disk.Entries) > maxEntries {
		return disk, int64(len(raw)), fmt.Errorf("mesh deferred journal exceeds physical entry limit: %d > %d", len(disk.Entries), maxEntries)
	}
	return disk, int64(len(raw)), nil
}

func meshJournalEngine(m *meshRuntime) (*Engine, error) {
	if m == nil {
		return nil, errors.New("mesh journal runtime unavailable")
	}
	m.mu.RLock()
	e := m.engine
	m.mu.RUnlock()
	if e == nil {
		return nil, errors.New("mesh journal requires a bound Memory body")
	}
	if root := fabricRootFor(e); root != nil {
		e = root
	}
	return e, nil
}

func upsertMeshDeferredJournalMemory(e *Engine, nodeID string, entries []MeshRequest, revision uint64) (int64, error) {
	if e == nil {
		return 0, errors.New("mesh journal engine unavailable")
	}
	raw, err := marshalMeshJournal(nodeID, entries)
	if err != nil {
		return 0, err
	}
	m := &Memory{
		ID:       meshDeferredJournalMemoryIDForNode(nodeID),
		Layer:    "emergent",
		Tags:     []string{"memory", meshDeferredJournalTag},
		Content:  "Durable deferred Sovereign Mesh requests stored inside Memory.",
		Revision: revision,
		State: map[string]any{
			"node_id":       nodeID,
			"entry_count":   len(entries),
			"journal_state": string(raw),
		},
	}
	if err := e.upsertExplicitMemoryBounded(m); err != nil {
		return 0, err
	}
	return int64(len(raw)), nil
}

func recoverMeshDeferredJournal(m *meshRuntime) error {
	st := meshJournalStateFor(m)
	if st == nil {
		return errors.New("mesh journal runtime unavailable")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return recoverMeshDeferredJournalLocked(m, st)
}

func recoverMeshDeferredJournalLocked(m *meshRuntime, st *meshJournalState) error {
	if st.loaded {
		return nil
	}
	e, err := meshJournalEngine(m)
	if err != nil {
		return err
	}
	m.mu.RLock()
	nodeID := m.nodeID
	m.mu.RUnlock()
	_, memory, err := e.resolveLocalFabricMemory(st.memoryID)
	if errors.Is(err, io.EOF) {
		m.mu.Lock()
		m.journal = nil
		m.mu.Unlock()
		st.bytes = 0
		st.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if memory == nil || !memoryHasTag(memory, meshDeferredJournalTag) {
		return fmt.Errorf("mesh deferred journal id collides with non-journal Memory: %s", st.memoryID)
	}
	disk, rawBytes, err := decodeMeshJournalMemory(memory, st.maxEntries, st.maxBytes)
	if err != nil {
		return err
	}
	if disk.NodeID != "" && disk.NodeID != nodeID {
		return fmt.Errorf("mesh deferred journal node mismatch: %q != %q", disk.NodeID, nodeID)
	}
	m.mu.Lock()
	m.journal = append([]MeshRequest(nil), disk.Entries...)
	m.mu.Unlock()
	st.bytes = rawBytes
	st.loaded = true
	return nil
}

func persistMeshDeferredJournalLocked(m *meshRuntime, st *meshJournalState, entries []MeshRequest) error {
	e, err := meshJournalEngine(m)
	if err != nil {
		return err
	}
	m.mu.RLock()
	nodeID := m.nodeID
	m.mu.RUnlock()
	raw, err := marshalMeshJournal(nodeID, entries)
	if err != nil {
		return err
	}
	if len(entries) > st.maxEntries {
		return fmt.Errorf("mesh deferred journal full: %d entries (limit %d)", len(entries), st.maxEntries)
	}
	if int64(len(raw)) > st.maxBytes {
		return fmt.Errorf("mesh deferred journal full: %d bytes (limit %d)", len(raw), st.maxBytes)
	}
	revision := uint64(1)
	if _, existing, er := e.resolveLocalFabricMemory(st.memoryID); er == nil && existing != nil {
		revision = existing.Revision + 1
	} else if er != nil && !errors.Is(er, io.EOF) {
		return er
	}
	bytesWritten, err := upsertMeshDeferredJournalMemory(e, nodeID, entries, revision)
	if err != nil {
		return err
	}
	// This receipt is part of the physical delivery boundary. Persist it before
	// reporting that the request was durably deferred.
	if err := e.persistAll(); err != nil {
		return fmt.Errorf("persist mesh deferred journal in memory.mem: %w", err)
	}
	st.bytes = bytesWritten
	return nil
}

func deferMeshRequest(m *meshRuntime, req MeshRequest) error {
	st := meshJournalStateFor(m)
	if st == nil {
		return errors.New("mesh journal runtime unavailable")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := recoverMeshDeferredJournalLocked(m, st); err != nil {
		return err
	}
	m.mu.RLock()
	next := append([]MeshRequest(nil), m.journal...)
	m.mu.RUnlock()
	logicalKey := meshJournalLogicalKey(req)
	replaced := false
	for i := range next {
		if meshJournalLogicalKey(next[i]) == logicalKey {
			next[i] = req
			replaced = true
			break
		}
	}
	if !replaced {
		next = append(next, req)
	}
	if err := persistMeshDeferredJournalLocked(m, st, next); err != nil {
		return err
	}
	m.mu.Lock()
	m.journal = next
	m.mu.Unlock()
	return nil
}

func meshJournalLogicalKey(req MeshRequest) string {
	if req.Op == "shared_propose" && strings.TrimSpace(req.MemoryID) != "" {
		return req.Op + "\x00" + strings.TrimSpace(req.MemoryID)
	}
	b, _ := json.Marshal(req)
	h := sha256.Sum256(b)
	return req.Op + "\x00" + hex.EncodeToString(h[:])
}

func meshJournalExactKey(req MeshRequest) string {
	b, _ := json.Marshal(req)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func removeDeferredMeshRequestExact(m *meshRuntime, req MeshRequest) error {
	st := meshJournalStateFor(m)
	if st == nil {
		return errors.New("mesh journal runtime unavailable")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := recoverMeshDeferredJournalLocked(m, st); err != nil {
		return err
	}
	m.mu.RLock()
	current := append([]MeshRequest(nil), m.journal...)
	m.mu.RUnlock()
	target := meshJournalExactKey(req)
	idx := -1
	for i := range current {
		if meshJournalExactKey(current[i]) == target {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	next := append([]MeshRequest(nil), current[:idx]...)
	next = append(next, current[idx+1:]...)
	if err := persistMeshDeferredJournalLocked(m, st, next); err != nil {
		return err
	}
	m.mu.Lock()
	m.journal = next
	m.mu.Unlock()
	return nil
}

func flushMeshDeferredJournal(m *meshRuntime) error {
	if err := recoverMeshDeferredJournal(m); err != nil {
		return err
	}
	m.mu.RLock()
	pending := append([]MeshRequest(nil), m.journal...)
	m.mu.RUnlock()
	for _, req := range pending {
		if _, err := m.authorityRPC(req); err != nil {
			continue
		}
		if err := removeDeferredMeshRequestExact(m, req); err != nil {
			return fmt.Errorf("authority accepted deferred mesh request but Memory spool update failed: %w", err)
		}
	}
	m.mu.RLock()
	remaining := len(m.journal)
	m.mu.RUnlock()
	if remaining > 0 {
		return fmt.Errorf("%d mesh journal entries remain deferred", remaining)
	}
	return nil
}

func recoverMeshJournalAfterBind(e *Engine) error {
	if e == nil {
		return nil
	}
	m := meshRuntimeCurrent()
	if m == nil {
		return nil
	}
	if err := recoverSovereignMeshState(m); err != nil {
		return fmt.Errorf("mesh sovereign Memory recovery: %w", err)
	}
	if m.role == "sovereign" {
		m.mu.RLock()
		self := m.directory[m.nodeID]
		m.mu.RUnlock()
		if err := persistSovereignMeshNode(m, self); err != nil {
			return fmt.Errorf("mesh sovereign self-directory persistence: %w", err)
		}
	}
	if err := recoverMeshDeferredJournal(m); err != nil {
		return fmt.Errorf("mesh journal recovery: %w", err)
	}
	return nil
}

func (m *meshRuntime) proposeSharedDurable(id string) (MeshResponse, error) {
	if m == nil {
		return MeshResponse{}, errors.New("mesh runtime unavailable")
	}
	m.mu.RLock()
	e := m.engine
	origin := m.nodeID
	endpoint := m.endpoint
	m.mu.RUnlock()
	if e == nil {
		return MeshResponse{}, errors.New("mesh engine unavailable")
	}
	_, mem, err := e.resolveLocalFabricMemoryCopy(strings.TrimSpace(id))
	if err != nil {
		return MeshResponse{}, err
	}
	req := MeshRequest{
		Op: "shared_propose", MemoryID: mem.ID, ProposalDigest: memoryJSONDigest(mem),
		Revision: mem.Revision, OriginNode: origin, Endpoint: endpoint, Tags: append([]string(nil), mem.Tags...),
	}
	res, authorityErr := m.authorityRPC(req)
	if authorityErr == nil {
		return res, nil
	}
	if err := deferMeshRequest(m, req); err != nil {
		return MeshResponse{OK: false, Status: "backpressure", Decision: "not-deferred", Reason: authorityErr.Error()}, fmt.Errorf("sovereign authority unavailable and Memory journal rejected request: %w", err)
	}
	return MeshResponse{OK: true, Status: "deferred", Decision: "authority-unreachable", Reason: authorityErr.Error()}, nil
}

func meshDeferredJournalInfo(m *meshRuntime) map[string]any {
	st := meshJournalStateFor(m)
	if st == nil {
		return map[string]any{"loaded": false}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	m.mu.RLock()
	entries := len(m.journal)
	m.mu.RUnlock()
	return map[string]any{
		"loaded":       st.loaded,
		"memory_id":    st.memoryID,
		"entries":      entries,
		"bytes":        st.bytes,
		"max_entries":  st.maxEntries,
		"max_bytes":    st.maxBytes,
		"durability":   "memory.mem",
		"backpressure": true,
		"sidecar":      false,
	}
}

func forgetMeshJournalState(m *meshRuntime) {
	if m != nil {
		meshJournalStates.Delete(m)
	}
}
