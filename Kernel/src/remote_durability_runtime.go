package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type remoteMutationACK struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// requireRemoteMutationACK converts the remote mem-node application-level ACK
// into the VM's physical error boundary. A successful TCP/JSON exchange is not
// a successful mutation unless the remote node explicitly confirms ok=true.
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

func durableTargetMatches(dst *Engine, id string, intended *Memory) error {
	got, err := resolveSpecificOwnerMemoryCopy(dst, id)
	if err != nil {
		return fmt.Errorf("durable transfer target %q cannot be read after persistence: %w", id, err)
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

// restoreTransferSourceAfterDeleteFailure is deliberately loss-averse. If the
// durable-delete phase fails after the destination has already committed, the
// original source is reintroduced as dirty local state when no concurrent writer
// has recreated it. A later persistence may therefore leave a duplicate, but it
// must never erase the only surviving copy of Memory.
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
	if err := persistEngineIfDirty(owner); err != nil {
		restoreTransferSourceAfterDeleteFailure(owner, snapshot)
		return fmt.Errorf("source durable delete failed after destination commit: %w", err)
	}
	if _, err := resolveSpecificOwnerMemoryCopy(owner, id); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("source %q still exists after durable delete; destination retained", id)
		}
		return fmt.Errorf("source %q durable delete verification failed: %w", id, err)
	}
	return nil
}

// transferMemoryDurable is a physical two-phase move/copy boundary:
//  1. copy a stable source snapshot into the destination;
//  2. durably persist and verify the destination;
//  3. only for move, delete the unchanged source and durably persist deletion.
//
// If phase 3 fails, the already-durable destination is kept and the source is
// retained/restored. Duplicate physical copies are preferable to Memory loss.
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
			if err := persistEngineIfDirty(dst); err != nil {
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

	dst.addRuntimeMemory(candidate)
	if err := persistEngineIfDirty(dst); err != nil {
		return "", fmt.Errorf("destination durability failed; source retained: %w", err)
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
