package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type remoteMutationACK struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func requireRemoteMutationACK(operation, payload string) error {
	var ack remoteMutationACK
	if err := json.Unmarshal([]byte(payload), &ack); err != nil {
		return fmt.Errorf("remote %s returned invalid mutation ACK: %w", operation, err)
	}
	if ack.OK {
		return nil
	}
	msg := strings.TrimSpace(ack.Error)
	if msg == "" {
		msg = "remote node rejected mutation"
	}
	return fmt.Errorf("remote %s failed: %s", operation, msg)
}

func transferTargetEngine(root *Engine, target string) (*Engine, error) {
	if root == nil {
		return nil, errors.New("Memory transfer requires Fabric root")
	}
	target = strings.TrimSpace(target)
	if target == "" || target == "primary" || target == root.bodyPath {
		return root, nil
	}
	cp, err := root.mountSpace(target)
	if err != nil {
		return nil, err
	}
	if cp == root.bodyPath {
		return root, nil
	}
	root.spaceMu.RLock()
	dst := root.spaces[cp]
	root.spaceMu.RUnlock()
	if dst == nil {
		return nil, fmt.Errorf("Memory transfer target not mounted: %s", cp)
	}
	if dst.manifest.Role != "storage" {
		return nil, fmt.Errorf("Memory transfer target is not passive storage: %s", cp)
	}
	return dst, nil
}

func sameStructuralMemory(a, b *Memory) bool {
	return structuralMemoryDigest(a) == structuralMemoryDigest(b)
}

type logicalDiskMemoryState int

const (
	logicalDiskUnknown logicalDiskMemoryState = iota
	logicalDiskAbsent
	logicalDiskPresent
)

// physicalMemoryWithJournal reads the durable logical view of one body: the
// indexed base Store plus the newest valid in-body mutation-journal overlay.
// It never trusts a journal whose base fingerprint does not match the body.
func physicalMemoryWithJournal(path, id string) (*Memory, logicalDiskMemoryState, error) {
	path = canonicalPhysicalBodyPath(path)
	zr, err := zip.OpenReader(path)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, os.ErrNotExist) {
			return nil, logicalDiskAbsent, nil
		}
		return nil, logicalDiskUnknown, err
	}
	defer zr.Close()
	var mf Manifest
	if err := readJSONZip(&zr.Reader, "manifest.json", &mf); err != nil {
		return nil, logicalDiskUnknown, err
	}
	st, err := openIndexedStore(path, zr, mf.Store)
	if err != nil {
		return nil, logicalDiskUnknown, err
	}
	baseMemory, baseErr := st.GetID(id)
	_ = st.Close()
	if baseErr != nil && !errors.Is(baseErr, io.EOF) {
		return nil, logicalDiskUnknown, baseErr
	}

	rec, journalErr := readMutationJournal(path)
	if journalErr != nil && !errors.Is(journalErr, errMutationJournalUnavailable) {
		return nil, logicalDiskUnknown, journalErr
	}
	if rec != nil {
		base, err := mutationJournalBaseFingerprint(path)
		if err != nil {
			return nil, logicalDiskUnknown, err
		}
		if rec.payload.Base != base {
			return nil, logicalDiskUnknown, errors.New("mutation journal base fingerprint mismatch during physical verification")
		}
		for i := len(rec.payload.Mutations) - 1; i >= 0; i-- {
			mutation := rec.payload.Mutations[i]
			if mutation.ID != id {
				continue
			}
			if mutation.Deleted {
				return nil, logicalDiskAbsent, nil
			}
			if mutation.Memory == nil {
				return nil, logicalDiskUnknown, errors.New("mutation journal contains nil Memory mutation")
			}
			return copyMemory(mutation.Memory), logicalDiskPresent, nil
		}
	}
	if errors.Is(baseErr, io.EOF) || baseMemory == nil {
		return nil, logicalDiskAbsent, nil
	}
	return copyMemory(baseMemory), logicalDiskPresent, nil
}

func durableTargetMatches(dst *Engine, id string, intended *Memory) error {
	if dst == nil {
		return errors.New("durable transfer target unavailable")
	}
	got, state, err := physicalMemoryWithJournal(dst.bodyPath, id)
	if err != nil {
		return fmt.Errorf("durable transfer target %q physical verification failed: %w", id, err)
	}
	if state != logicalDiskPresent || got == nil {
		return fmt.Errorf("durable transfer target %q cannot be read after persistence", id)
	}
	if !sameStructuralMemory(got, intended) {
		return fmt.Errorf("durable transfer target %q changed before ACK", id)
	}
	return nil
}

func markTransferSourceDeletedIfUnchanged(owner *Engine, id, expectedDigest string) error {
	if owner == nil {
		return errors.New("Memory transfer source owner unavailable")
	}
	loaded, err := owner.resolveIDLocal(id)
	if err != nil {
		return err
	}
	owner.dataMu.Lock()
	defer owner.dataMu.Unlock()
	if owner.deletedIDs[id] {
		return io.EOF
	}
	current := owner.cache[id]
	if current == nil {
		current = loaded
	}
	if current == nil {
		return io.EOF
	}
	if structuralMemoryDigest(current) != expectedDigest {
		return fmt.Errorf("Memory transfer source %q changed after destination durability; source retained", id)
	}
	for _, tag := range current.Tags {
		owner.tagDeltaRemoveLocked(id, tag)
	}
	owner.deletedIDs[id] = true
	delete(owner.cache, id)
	delete(owner.newIDs, id)
	delete(owner.dirtyIDs, id)
	owner.dirty = true
	return nil
}

func restoreTransferSourceAfterDeleteFailure(owner *Engine, snapshot *Memory) {
	if owner == nil || snapshot == nil || strings.TrimSpace(snapshot.ID) == "" {
		return
	}
	owner.dataMu.Lock()
	defer owner.dataMu.Unlock()
	id := snapshot.ID
	if !owner.deletedIDs[id] || owner.cache[id] != nil {
		return
	}
	q := copyMemory(snapshot)
	owner.cache[id] = q
	owner.newIDs[id] = true
	owner.dirtyIDs[id] = true
	delete(owner.deletedIDs, id)
	for _, tag := range q.Tags {
		owner.tagDeltaAddLocked(id, tag)
	}
	owner.dirty = true
}

func durableDeleteTransferSource(owner *Engine, snapshot *Memory) error {
	if owner == nil || snapshot == nil {
		return errors.New("Memory transfer source unavailable")
	}
	id := strings.TrimSpace(snapshot.ID)
	if id == "" {
		return errors.New("Memory transfer source id required")
	}
	expected := structuralMemoryDigest(snapshot)
	if err := markTransferSourceDeletedIfUnchanged(owner, id, expected); err != nil {
		return err
	}
	// Source deletion can now use the in-body journal because verification below
	// reads the journal overlay rather than only the base ZIP Store.
	if err := persistEngineIncremental(owner); err != nil {
		restoreTransferSourceAfterDeleteFailure(owner, snapshot)
		return fmt.Errorf("source durable delete failed after destination commit: %w", err)
	}
	got, state, err := physicalMemoryWithJournal(owner.bodyPath, id)
	if err != nil {
		restoreTransferSourceAfterDeleteFailure(owner, snapshot)
		return fmt.Errorf("source %q durable delete verification failed: %w", id, err)
	}
	if state != logicalDiskAbsent || got != nil {
		restoreTransferSourceAfterDeleteFailure(owner, snapshot)
		return fmt.Errorf("source %q still exists after durable delete; destination retained", id)
	}
	return nil
}

type transferCandidateDiskState int

const (
	transferCandidateDiskUnknown transferCandidateDiskState = iota
	transferCandidateDiskAbsent
	transferCandidateDiskMatching
	transferCandidateDiskConflicting
)

func probeTransferCandidateOnDisk(dst *Engine, candidate *Memory) transferCandidateDiskState {
	if dst == nil || candidate == nil || strings.TrimSpace(candidate.ID) == "" {
		return transferCandidateDiskUnknown
	}
	got, state, err := physicalMemoryWithJournal(dst.bodyPath, candidate.ID)
	if err != nil || state == logicalDiskUnknown {
		return transferCandidateDiskUnknown
	}
	if state == logicalDiskAbsent || got == nil {
		return transferCandidateDiskAbsent
	}
	if sameStructuralMemory(got, candidate) {
		return transferCandidateDiskMatching
	}
	return transferCandidateDiskConflicting
}

func handleFailedTransferTargetPersistence(dst *Engine, candidate *Memory) string {
	if dst == nil || candidate == nil {
		return "unknown"
	}
	unlock := lockEnginePersistence(dst)
	defer unlock()
	switch probeTransferCandidateOnDisk(dst, candidate) {
	case transferCandidateDiskAbsent:
		if rollbackUnpersistedTransferCandidate(dst, candidate) {
			return "rolled_back"
		}
		return "not_safely_rollbackable"
	case transferCandidateDiskMatching:
		return "durable_duplicate_retained"
	case transferCandidateDiskConflicting:
		return "conflicting_disk_state_retained"
	default:
		return "unknown_disk_state_retained"
	}
}

func addTransferTargetCandidateBounded(dst *Engine, candidate *Memory) error {
	if dst == nil || candidate == nil || strings.TrimSpace(candidate.ID) == "" {
		return errors.New("Memory transfer target candidate unavailable")
	}
	automaticShardMu.Lock()
	defer automaticShardMu.Unlock()
	if existing, err := resolveSpecificOwnerMemoryCopy(dst, candidate.ID); err == nil && existing != nil {
		return fmt.Errorf("Memory transfer target id appeared concurrently: %q", candidate.ID)
	} else if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if !shardHasCapacity(dst, 1) {
		return fmt.Errorf("explicit Memory transfer target full: %d Memory limit reached", memoryShardMax())
	}
	dst.addRuntimeMemory(candidate)
	return nil
}

func rollbackUnpersistedTransferCandidate(dst *Engine, candidate *Memory) bool {
	if dst == nil || candidate == nil || strings.TrimSpace(candidate.ID) == "" {
		return false
	}
	id := candidate.ID
	expected := structuralMemoryDigest(candidate)
	dst.dataMu.Lock()
	defer dst.dataMu.Unlock()
	live := dst.cache[id]
	if live == nil || !dst.newIDs[id] || dst.deletedIDs[id] || structuralMemoryDigest(live) != expected {
		return false
	}
	delete(dst.cache, id)
	delete(dst.newIDs, id)
	delete(dst.dirtyIDs, id)
	delete(dst.deletedIDs, id)
	for _, tag := range candidate.Tags {
		if ids := dst.tagAdded[tag]; ids != nil {
			delete(ids, id)
			if len(ids) == 0 {
				delete(dst.tagAdded, tag)
			}
		}
	}
	pending := len(dst.newIDs) > 0 || len(dst.dirtyIDs) > 0
	if !pending {
		for _, yes := range dst.deletedIDs {
			if yes {
				pending = true
				break
			}
		}
	}
	if !pending {
		for _, ids := range dst.tagAdded {
			if len(ids) > 0 {
				pending = true
				break
			}
		}
	}
	if !pending {
		for _, ids := range dst.tagRemoved {
			if len(ids) > 0 {
				pending = true
				break
			}
		}
	}
	if !pending {
		dst.dirty = false
	}
	return true
}

func transferMemoryDurable(e *Engine, id, target string, move bool) (string, error) {
	if e == nil {
		return "", errors.New("Memory transfer requires engine")
	}
	root := fabricRootFor(e)
	if root == nil {
		root = e
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("Memory transfer source id required")
	}

	src, source, err := root.resolveLocalFabricMemoryCopy(id)
	if err != nil {
		return "", err
	}
	dst, err := transferTargetEngine(root, target)
	if err != nil {
		return "", err
	}
	if src == dst {
		return source.ID, nil
	}

	candidate := copyMemory(source)
	newID := candidate.ID
	if existing, er := resolveSpecificOwnerMemoryCopy(dst, newID); er == nil {
		if sameStructuralMemory(existing, candidate) {
			if err := persistEngineIncremental(dst); err != nil {
				return "", fmt.Errorf("destination durability failed: %w", err)
			}
			if err := durableTargetMatches(dst, newID, candidate); err != nil {
				return "", err
			}
			if move {
				if err := durableDeleteTransferSource(src, source); err != nil {
					return newID, err
				}
			}
			return newID, nil
		}
		oldID := newID
		for {
			newID = nextID("mem")
			if _, er := resolveSpecificOwnerMemoryCopy(dst, newID); errors.Is(er, io.EOF) {
				break
			} else if er != nil {
				return "", er
			}
		}
		candidate.ID = newID
		if candidate.State == nil {
			candidate.State = map[string]any{}
		}
		candidate.State["merge_source_id"] = oldID
	} else if !errors.Is(er, io.EOF) {
		return "", er
	}

	if err := addTransferTargetCandidateBounded(dst, candidate); err != nil {
		return "", err
	}
	if err := persistEngineIncremental(dst); err != nil {
		outcome := handleFailedTransferTargetPersistence(dst, candidate)
		switch outcome {
		case "rolled_back":
			return "", fmt.Errorf("destination durability failed; source retained and transient target candidate rolled back: %w", err)
		case "durable_duplicate_retained":
			return newID, fmt.Errorf("destination persistence reported failure after target became durable; source retained and durable duplicate kept: %w", err)
		default:
			return "", fmt.Errorf("destination durability failed; source retained; target state preserved because rollback safety is %s: %w", outcome, err)
		}
	}
	if err := durableTargetMatches(dst, newID, candidate); err != nil {
		return "", err
	}
	if move {
		if err := durableDeleteTransferSource(src, source); err != nil {
			return newID, err
		}
	}
	return newID, nil
}
