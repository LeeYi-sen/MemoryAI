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
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	meshProposalReplayVersion           = 1
	defaultMeshProposalReplayMaxEntries = 65536
	defaultMeshProposalReplayMaxBytes   = int64(64 << 20)
	hardMeshProposalReplayMaxEntries    = 1 << 20
	hardMeshProposalReplayMaxBytes      = int64(1 << 30)
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

type meshProposalReplayDisk struct {
	Version int                       `json:"version"`
	Entries []meshProposalReplayEntry `json:"entries"`
}

type meshProposalReplayState struct {
	mu         sync.Mutex
	path       string
	loaded     bool
	entries    map[string]meshProposalReplayEntry // request_id -> durable receipt/fence
	latest     map[string]string                  // slot -> newest request_id
	live       map[string]bool                    // slot -> executing in this process
	bytes      int64
	maxEntries int
	maxBytes   int64
}

var meshProposalReplayStates sync.Map // map[canonical replay file path]*meshProposalReplayState

func meshProposalReplayPath(e *Engine) (string, error) {
	root := eventFabricRoot(e)
	if root == nil || strings.TrimSpace(root.bodyPath) == "" {
		return "", fmt.Errorf("mesh proposal replay requires a bound physical Memory body")
	}
	body := canonicalPhysicalBodyPath(root.bodyPath)
	sum := sha256.Sum256([]byte(body))
	name := "Memory.mesh-proposal-replay." + hex.EncodeToString(sum[:6]) + ".json"
	return filepath.Join(filepath.Dir(body), name), nil
}

func meshProposalReplayStateFor(e *Engine) (*meshProposalReplayState, error) {
	path, err := meshProposalReplayPath(e)
	if err != nil {
		return nil, err
	}
	if raw, ok := meshProposalReplayStates.Load(path); ok {
		return raw.(*meshProposalReplayState), nil
	}
	st := &meshProposalReplayState{
		path:       path,
		entries:    map[string]meshProposalReplayEntry{},
		latest:     map[string]string{},
		live:       map[string]bool{},
		maxEntries: boundedMeshJournalIntEnv("MEMORYAI_MESH_REPLAY_MAX_ENTRIES", defaultMeshProposalReplayMaxEntries, hardMeshProposalReplayMaxEntries),
		maxBytes:   boundedMeshJournalInt64Env("MEMORYAI_MESH_REPLAY_MAX_BYTES", defaultMeshProposalReplayMaxBytes, hardMeshProposalReplayMaxBytes),
	}
	actual, _ := meshProposalReplayStates.LoadOrStore(path, st)
	return actual.(*meshProposalReplayState), nil
}

func forgetMeshProposalReplayState(e *Engine) {
	path, err := meshProposalReplayPath(e)
	if err == nil {
		meshProposalReplayStates.Delete(path)
	}
}

func recoverMeshProposalReplayLocked(st *meshProposalReplayState) error {
	if st.loaded {
		return nil
	}
	f, err := os.Open(st.path)
	if err != nil {
		if os.IsNotExist(err) {
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
		return fmt.Errorf("mesh proposal replay ledger exceeds physical byte limit: %d > %d", info.Size(), st.maxBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(f, st.maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > st.maxBytes {
		return fmt.Errorf("mesh proposal replay ledger exceeds physical byte limit: %d > %d", len(raw), st.maxBytes)
	}
	var disk meshProposalReplayDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		return fmt.Errorf("mesh proposal replay ledger decode failed: %w", err)
	}
	if disk.Version != meshProposalReplayVersion {
		return fmt.Errorf("unsupported mesh proposal replay ledger version: %d", disk.Version)
	}
	if len(disk.Entries) > st.maxEntries {
		return fmt.Errorf("mesh proposal replay ledger exceeds physical entry limit: %d > %d", len(disk.Entries), st.maxEntries)
	}
	entries := make(map[string]meshProposalReplayEntry, len(disk.Entries))
	latest := make(map[string]string)
	for _, entry := range disk.Entries {
		if strings.TrimSpace(entry.Slot) == "" || strings.TrimSpace(entry.RequestID) == "" {
			return fmt.Errorf("mesh proposal replay ledger contains invalid identity")
		}
		if entry.State != "executing" && entry.State != "done" {
			return fmt.Errorf("mesh proposal replay ledger contains invalid state %q", entry.State)
		}
		if _, exists := entries[entry.RequestID]; exists {
			return fmt.Errorf("mesh proposal replay ledger contains duplicate request %q", entry.RequestID)
		}
		entry.ResultVars = cloneStringMap(entry.ResultVars)
		entries[entry.RequestID] = entry
		if currentID, ok := latest[entry.Slot]; ok {
			current := entries[currentID]
			if entry.Revision == current.Revision {
				return fmt.Errorf("mesh proposal replay ledger contains conflicting slot revision %q@%d", entry.Slot, entry.Revision)
			}
			if entry.Revision < current.Revision {
				continue
			}
		}
		latest[entry.Slot] = entry.RequestID
	}
	st.entries = entries
	st.latest = latest
	st.live = map[string]bool{}
	st.bytes = int64(len(raw))
	st.loaded = true
	return nil
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

func marshalMeshProposalReplay(entries map[string]meshProposalReplayEntry) ([]byte, error) {
	keys := make([]string, 0, len(entries))
	for requestID := range entries {
		keys = append(keys, requestID)
	}
	sort.Strings(keys)
	ordered := make([]meshProposalReplayEntry, 0, len(keys))
	for _, requestID := range keys {
		entry := entries[requestID]
		entry.ResultVars = cloneStringMap(entry.ResultVars)
		ordered = append(ordered, entry)
	}
	return json.Marshal(meshProposalReplayDisk{Version: meshProposalReplayVersion, Entries: ordered})
}

func persistMeshProposalReplayLocked(st *meshProposalReplayState, entries map[string]meshProposalReplayEntry) error {
	raw, err := marshalMeshProposalReplay(entries)
	if err != nil {
		return err
	}
	if int64(len(raw)) > st.maxBytes {
		return fmt.Errorf("mesh proposal replay ledger full: %d bytes (limit %d)", len(raw), st.maxBytes)
	}
	if err := persistMeshJournalFile(st.path, raw); err != nil {
		return fmt.Errorf("persist mesh proposal replay ledger: %w", err)
	}
	st.bytes = int64(len(raw))
	return nil
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
		return meshProposalIdentity{}, "", "", fmt.Errorf("mesh proposal replay requires frame")
	}
	memoryID := strings.TrimSpace(f.Vars["memory_id"])
	originNode := strings.TrimSpace(f.Vars["origin_node"])
	if memoryID == "" || originNode == "" {
		return meshProposalIdentity{}, "", "", fmt.Errorf("mesh proposal replay requires memory_id and origin_node")
	}
	revision, err := strconv.ParseUint(strings.TrimSpace(f.Vars["revision"]), 10, 64)
	if err != nil {
		return meshProposalIdentity{}, "", "", fmt.Errorf("mesh proposal replay invalid revision: %w", err)
	}
	tags := append([]string(nil), f.Lists["tags"]...)
	sort.Strings(tags)
	id := meshProposalIdentity{
		Event:          strings.TrimSpace(ev.Name),
		Subject:        strings.TrimSpace(ev.Subject),
		MemoryID:       memoryID,
		OriginNode:     originNode,
		ProposalDigest: strings.TrimSpace(f.Vars["proposal_digest"]),
		Revision:       revision,
		Tags:           tags,
	}
	raw, err := json.Marshal(id)
	if err != nil {
		return meshProposalIdentity{}, "", "", err
	}
	sum := sha256.Sum256(raw)
	requestID := "mesh-proposal-" + hex.EncodeToString(sum[:])
	slot := originNode + "\x00" + memoryID
	return id, slot, requestID, nil
}

func cloneReplayEntries(src map[string]meshProposalReplayEntry) map[string]meshProposalReplayEntry {
	out := make(map[string]meshProposalReplayEntry, len(src))
	for k, v := range src {
		v.ResultVars = cloneStringMap(v.ResultVars)
		out[k] = v
	}
	return out
}

func applyMeshProposalReplayResult(f *Frame, entry meshProposalReplayEntry) error {
	if f == nil {
		return fmt.Errorf("mesh proposal replay requires frame")
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

func prepareMeshProposalReplay(e *Engine, f *Frame, ev PhysicalEvent) (*meshProposalReplayState, meshProposalReplayEntry, bool, error) {
	identity, slot, requestID, err := meshProposalIdentityFromFrame(f, ev)
	if err != nil {
		return nil, meshProposalReplayEntry{}, false, err
	}
	st, err := meshProposalReplayStateFor(e)
	if err != nil {
		return nil, meshProposalReplayEntry{}, false, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := recoverMeshProposalReplayLocked(st); err != nil {
		return nil, meshProposalReplayEntry{}, false, err
	}
	if previous, ok := st.entries[requestID]; ok {
		if previous.Slot != slot || previous.Revision != identity.Revision {
			return st, previous, true, fmt.Errorf("mesh proposal replay request identity collision")
		}
		if previous.State == "done" {
			return st, previous, true, nil
		}
		return st, previous, true, fmt.Errorf("mesh proposal request %s is durably fenced as in-flight; refusing duplicate execution", requestID)
	}
	if st.live[slot] {
		return st, meshProposalReplayEntry{}, false, fmt.Errorf("mesh proposal slot %q already executing", slot)
	}
	if latestID, ok := st.latest[slot]; ok {
		previous := st.entries[latestID]
		if identity.Revision < previous.Revision {
			return st, previous, false, fmt.Errorf("stale unseen mesh proposal revision %d < %d", identity.Revision, previous.Revision)
		}
		if identity.Revision == previous.Revision {
			return st, previous, false, fmt.Errorf("conflicting mesh proposal identity at revision %d", identity.Revision)
		}
		// A higher Memory revision is a new physical proposal. Historical receipts
		// remain addressable by request_id so a delayed old spool replay can still
		// recover its original result without re-running the Memory event.
	}
	if len(st.entries) >= st.maxEntries {
		return st, meshProposalReplayEntry{}, false, fmt.Errorf("mesh proposal replay ledger full: %d entries (limit %d)", len(st.entries), st.maxEntries)
	}

	entry := meshProposalReplayEntry{
		Slot:        slot,
		RequestID:   requestID,
		Revision:    identity.Revision,
		State:       "executing",
		UpdatedNano: time.Now().UnixNano(),
	}
	next := cloneReplayEntries(st.entries)
	next[requestID] = entry
	if err := persistMeshProposalReplayLocked(st, next); err != nil {
		return st, meshProposalReplayEntry{}, false, err
	}
	st.entries = next
	st.latest[slot] = requestID
	st.live[slot] = true
	return st, entry, false, nil
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

func finalizeMeshProposalReplay(st *meshProposalReplayState, entry meshProposalReplayEntry, f *Frame, runErr error) error {
	if st == nil {
		return fmt.Errorf("mesh proposal replay state unavailable")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	current, ok := st.entries[entry.RequestID]
	if !ok || current.Slot != entry.Slot {
		delete(st.live, entry.Slot)
		return fmt.Errorf("mesh proposal replay identity changed during execution")
	}
	done := current
	done.State = "done"
	done.ResultVars = meshProposalResultVars(f)
	if runErr != nil {
		done.ResultError = runErr.Error()
	}
	done.UpdatedNano = time.Now().UnixNano()
	next := cloneReplayEntries(st.entries)
	next[entry.RequestID] = done
	if err := persistMeshProposalReplayLocked(st, next); err != nil {
		// Keep the durable/in-memory executing fence. A retry must not re-run an
		// event whose effects may already have happened.
		delete(st.live, entry.Slot)
		return err
	}
	st.entries = next
	delete(st.live, entry.Slot)
	return nil
}

func executeMeshProposalEventOnce(e *Engine, f *Frame, ev PhysicalEvent, dispatch func() error) error {
	if dispatch == nil {
		return fmt.Errorf("mesh proposal replay requires dispatch function")
	}
	st, entry, replay, err := prepareMeshProposalReplay(e, f, ev)
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
	if persistErr := finalizeMeshProposalReplay(st, entry, f, runErr); persistErr != nil {
		return fmt.Errorf("mesh proposal event executed but replay result was not durably recorded; request remains fenced: %w", persistErr)
	}
	return runErr
}
