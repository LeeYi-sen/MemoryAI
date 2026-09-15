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
	"strconv"
	"strings"
	"sync"
)

const (
	meshJournalVersion           = 1
	defaultMeshJournalMaxEntries = 1024
	defaultMeshJournalMaxBytes   = int64(8 << 20)
	hardMeshJournalMaxEntries    = 65536
	hardMeshJournalMaxBytes      = int64(256 << 20)
)

type meshJournalDisk struct {
	Version int           `json:"version"`
	NodeID  string        `json:"node_id"`
	Entries []MeshRequest `json:"entries"`
}

type meshJournalState struct {
	mu         sync.Mutex
	path       string
	loaded     bool
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

func meshJournalStateFor(m *meshRuntime) *meshJournalState {
	if m == nil {
		return nil
	}
	if raw, ok := meshJournalStates.Load(m); ok {
		return raw.(*meshJournalState)
	}
	st := &meshJournalState{
		maxEntries: boundedMeshJournalIntEnv("MEMORYAI_MESH_JOURNAL_MAX_ENTRIES", defaultMeshJournalMaxEntries, hardMeshJournalMaxEntries),
		maxBytes:   boundedMeshJournalInt64Env("MEMORYAI_MESH_JOURNAL_MAX_BYTES", defaultMeshJournalMaxBytes, hardMeshJournalMaxBytes),
	}
	actual, _ := meshJournalStates.LoadOrStore(m, st)
	return actual.(*meshJournalState)
}

func meshJournalPath(m *meshRuntime) (string, error) {
	if m == nil {
		return "", fmt.Errorf("mesh journal runtime unavailable")
	}
	if override := strings.TrimSpace(os.Getenv("MEMORYAI_MESH_JOURNAL_PATH")); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		return filepath.Clean(abs), nil
	}
	m.mu.RLock()
	e := m.engine
	nodeID := m.nodeID
	m.mu.RUnlock()
	if e == nil || strings.TrimSpace(e.bodyPath) == "" {
		return "", fmt.Errorf("mesh journal requires a bound physical Memory body")
	}
	h := sha256.Sum256([]byte(nodeID))
	name := "Memory.mesh-journal." + hex.EncodeToString(h[:6]) + ".json"
	return filepath.Join(filepath.Dir(canonicalPhysicalBodyPath(e.bodyPath)), name), nil
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

func marshalMeshJournal(nodeID string, entries []MeshRequest) ([]byte, error) {
	return json.Marshal(meshJournalDisk{
		Version: meshJournalVersion,
		NodeID:  nodeID,
		Entries: entries,
	})
}

func persistMeshJournalFile(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".memoryai-mesh-journal-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func recoverMeshDeferredJournal(m *meshRuntime) error {
	st := meshJournalStateFor(m)
	if st == nil {
		return fmt.Errorf("mesh journal runtime unavailable")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return recoverMeshDeferredJournalLocked(m, st)
}

func recoverMeshDeferredJournalLocked(m *meshRuntime, st *meshJournalState) error {
	if st.loaded {
		return nil
	}
	path, err := meshJournalPath(m)
	if err != nil {
		return err
	}
	st.path = path
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			m.mu.Lock()
			m.journal = nil
			m.mu.Unlock()
			st.bytes = 0
			st.loaded = true
			return nil
		}
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > st.maxBytes {
		return fmt.Errorf("mesh deferred journal exceeds physical byte limit: %d > %d", info.Size(), st.maxBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(f, st.maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > st.maxBytes {
		return fmt.Errorf("mesh deferred journal exceeds physical byte limit: %d > %d", len(raw), st.maxBytes)
	}
	var disk meshJournalDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		return fmt.Errorf("mesh deferred journal decode failed: %w", err)
	}
	if disk.Version != meshJournalVersion {
		return fmt.Errorf("unsupported mesh deferred journal version: %d", disk.Version)
	}
	m.mu.RLock()
	nodeID := m.nodeID
	m.mu.RUnlock()
	if disk.NodeID != "" && disk.NodeID != nodeID {
		return fmt.Errorf("mesh deferred journal node mismatch: %q != %q", disk.NodeID, nodeID)
	}
	if len(disk.Entries) > st.maxEntries {
		return fmt.Errorf("mesh deferred journal exceeds physical entry limit: %d > %d", len(disk.Entries), st.maxEntries)
	}
	entries := append([]MeshRequest(nil), disk.Entries...)
	m.mu.Lock()
	m.journal = entries
	m.mu.Unlock()
	st.bytes = int64(len(raw))
	st.loaded = true
	return nil
}

func deferMeshRequest(m *meshRuntime, req MeshRequest) error {
	st := meshJournalStateFor(m)
	if st == nil {
		return fmt.Errorf("mesh journal runtime unavailable")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := recoverMeshDeferredJournalLocked(m, st); err != nil {
		return err
	}
	m.mu.RLock()
	nodeID := m.nodeID
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
		if len(next) >= st.maxEntries {
			return fmt.Errorf("mesh deferred journal full: %d entries (limit %d)", len(next), st.maxEntries)
		}
		next = append(next, req)
	}
	raw, err := marshalMeshJournal(nodeID, next)
	if err != nil {
		return err
	}
	if int64(len(raw)) > st.maxBytes {
		return fmt.Errorf("mesh deferred journal full: %d bytes (limit %d)", len(raw), st.maxBytes)
	}
	if err := persistMeshJournalFile(st.path, raw); err != nil {
		return fmt.Errorf("persist mesh deferred journal: %w", err)
	}
	m.mu.Lock()
	m.journal = next
	m.mu.Unlock()
	st.bytes = int64(len(raw))
	return nil
}

func removeDeferredMeshRequestExact(m *meshRuntime, req MeshRequest) error {
	st := meshJournalStateFor(m)
	if st == nil {
		return fmt.Errorf("mesh journal runtime unavailable")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := recoverMeshDeferredJournalLocked(m, st); err != nil {
		return err
	}
	m.mu.RLock()
	nodeID := m.nodeID
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
	next := make([]MeshRequest, 0, len(current)-1)
	next = append(next, current[:idx]...)
	next = append(next, current[idx+1:]...)
	raw, err := marshalMeshJournal(nodeID, next)
	if err != nil {
		return err
	}
	if int64(len(raw)) > st.maxBytes {
		return fmt.Errorf("mesh deferred journal rewrite exceeds byte limit")
	}
	if err := persistMeshJournalFile(st.path, raw); err != nil {
		return fmt.Errorf("persist mesh deferred journal removal: %w", err)
	}
	m.mu.Lock()
	m.journal = next
	m.mu.Unlock()
	st.bytes = int64(len(raw))
	return nil
}

func flushMeshDeferredJournal(m *meshRuntime) error {
	if err := recoverMeshDeferredJournal(m); err != nil {
		return err
	}
	m.mu.RLock()
	pending := append([]MeshRequest(nil), m.journal...)
	m.mu.RUnlock()
	failed := 0
	for _, req := range pending {
		if _, err := m.authorityRPC(req); err != nil {
			failed++
			continue
		}
		if err := removeDeferredMeshRequestExact(m, req); err != nil {
			return fmt.Errorf("authority accepted deferred mesh request but local spool update failed: %w", err)
		}
	}
	m.mu.RLock()
	remaining := len(m.journal)
	m.mu.RUnlock()
	if failed > 0 || remaining > 0 {
		return fmt.Errorf("%d mesh journal entries remain deferred", remaining)
	}
	return nil
}

func recoverMeshJournalAfterBind(e *Engine) {
	if e == nil {
		return
	}
	m := meshRuntimeCurrent()
	if m == nil {
		return
	}
	if err := recoverMeshDeferredJournal(m); err != nil {
		m.mu.Lock()
		if m.startupErr == "" {
			m.startupErr = "mesh journal recovery: " + err.Error()
		} else {
			m.startupErr += "; mesh journal recovery: " + err.Error()
		}
		m.mu.Unlock()
	}
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
		return MeshResponse{OK: false, Status: "backpressure", Decision: "not-deferred", Reason: authorityErr.Error()}, fmt.Errorf("sovereign authority unavailable and durable mesh journal rejected request: %w", err)
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
		"path":         st.path,
		"entries":      entries,
		"bytes":        st.bytes,
		"max_entries":  st.maxEntries,
		"max_bytes":    st.maxBytes,
		"durability":   "atomic-file-spool",
		"backpressure": true,
	}
}

func forgetMeshJournalState(m *meshRuntime) {
	if m != nil {
		meshJournalStates.Delete(m)
	}
}
