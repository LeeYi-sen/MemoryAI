package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	meshDirectoryNodeTag = "memory-mesh-directory-node"
	meshSharedRecordTag  = "memory-mesh-shared-record"
)

func stableMeshStateID(prefix, raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return prefix + hex.EncodeToString(sum[:16])
}

func meshDirectoryNodeMemoryID(nodeID string) string {
	return stableMeshStateID("mesh-directory-node-", nodeID)
}

func meshSharedRecordMemoryID(memoryID string) string {
	return stableMeshStateID("mesh-shared-record-", memoryID)
}

func meshStateEngine(m *meshRuntime) (*Engine, error) {
	if m == nil {
		return nil, errors.New("mesh runtime unavailable")
	}
	m.mu.RLock()
	e := m.engine
	m.mu.RUnlock()
	if e == nil {
		return nil, errors.New("mesh engine unavailable")
	}
	if root := fabricRootFor(e); root != nil {
		e = root
	}
	return e, nil
}

func meshNodeMemory(node MeshNode, revision uint64) (*Memory, error) {
	if strings.TrimSpace(node.ID) == "" {
		return nil, errors.New("mesh node id required")
	}
	raw, err := json.Marshal(node)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:       meshDirectoryNodeMemoryID(node.ID),
		Layer:    "emergent",
		Tags:     []string{"memory", meshDirectoryNodeTag},
		Content:  "Sovereign Memory Directory node fact.",
		Revision: revision,
		State: map[string]any{
			"node_id":     node.ID,
			"role":        node.Role,
			"endpoint":    node.Endpoint,
			"last_seen":   node.LastSeen,
			"node_record": string(raw),
		},
	}, nil
}

func meshSharedMemory(record MeshRecord, revision uint64) (*Memory, error) {
	if strings.TrimSpace(record.MemoryID) == "" || strings.TrimSpace(record.OriginNode) == "" {
		return nil, errors.New("mesh shared record requires memory_id and origin_node")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:       meshSharedRecordMemoryID(record.MemoryID),
		Layer:    "emergent",
		Tags:     []string{"memory", meshSharedRecordTag},
		Content:  "Sovereign authorization for direct-read Shared Memory.",
		Revision: revision,
		State: map[string]any{
			"memory_id":        record.MemoryID,
			"origin_node":      record.OriginNode,
			"origin_endpoint":  record.Endpoint,
			"backing_revision": record.Revision,
			"digest":           record.Digest,
			"shared_record":    string(raw),
		},
	}, nil
}

func meshNextRecordRevision(e *Engine, id string) (uint64, error) {
	_, current, err := e.resolveLocalFabricMemory(id)
	if errors.Is(err, io.EOF) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return current.Revision + 1, nil
}

func persistSovereignMeshNode(m *meshRuntime, node MeshNode) error {
	e, err := meshStateEngine(m)
	if err != nil {
		return err
	}
	id := meshDirectoryNodeMemoryID(node.ID)
	revision, err := meshNextRecordRevision(e, id)
	if err != nil {
		return err
	}
	memory, err := meshNodeMemory(node, revision)
	if err != nil {
		return err
	}
	if err := e.upsertExplicitMemoryBounded(memory); err != nil {
		return err
	}
	return e.persistAll()
}

func persistSovereignSharedRecord(m *meshRuntime, record MeshRecord) error {
	e, err := meshStateEngine(m)
	if err != nil {
		return err
	}
	id := meshSharedRecordMemoryID(record.MemoryID)
	revision, err := meshNextRecordRevision(e, id)
	if err != nil {
		return err
	}
	memory, err := meshSharedMemory(record, revision)
	if err != nil {
		return err
	}
	if err := e.upsertExplicitMemoryBounded(memory); err != nil {
		return err
	}
	return e.persistAll()
}

func decodeMeshNodeMemory(memory *Memory) (MeshNode, error) {
	var node MeshNode
	if memory == nil || !memoryHasTag(memory, meshDirectoryNodeTag) {
		return node, errors.New("mesh node Memory unavailable")
	}
	raw := strings.TrimSpace(fmt.Sprint(memory.State["node_record"]))
	if raw == "" {
		return node, errors.New("mesh node Memory missing node_record")
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return node, err
	}
	if strings.TrimSpace(node.ID) == "" {
		return node, errors.New("mesh node Memory has empty id")
	}
	return node, nil
}

func decodeMeshSharedMemory(memory *Memory) (MeshRecord, error) {
	var record MeshRecord
	if memory == nil || !memoryHasTag(memory, meshSharedRecordTag) {
		return record, errors.New("mesh shared Memory unavailable")
	}
	raw := strings.TrimSpace(fmt.Sprint(memory.State["shared_record"]))
	if raw == "" {
		return record, errors.New("mesh shared Memory missing shared_record")
	}
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return record, err
	}
	if strings.TrimSpace(record.MemoryID) == "" || strings.TrimSpace(record.OriginNode) == "" {
		return record, errors.New("mesh shared Memory identity invalid")
	}
	return record, nil
}

func recoverSovereignMeshState(m *meshRuntime) error {
	if m == nil || m.role != "sovereign" {
		return nil
	}
	e, err := meshStateEngine(m)
	if err != nil {
		return err
	}
	nodeIDs, err := e.listTagFabric(meshDirectoryNodeTag)
	if err != nil {
		return err
	}
	sharedIDs, err := e.listTagFabric(meshSharedRecordTag)
	if err != nil {
		return err
	}
	sort.Strings(nodeIDs)
	sort.Strings(sharedIDs)
	directory := map[string]MeshNode{}
	shared := map[string]MeshRecord{}
	for _, id := range nodeIDs {
		_, memory, err := e.resolveLocalFabricMemory(id)
		if err != nil {
			return err
		}
		node, err := decodeMeshNodeMemory(memory)
		if err != nil {
			return err
		}
		directory[node.ID] = node
	}
	for _, id := range sharedIDs {
		_, memory, err := e.resolveLocalFabricMemory(id)
		if err != nil {
			return err
		}
		record, err := decodeMeshSharedMemory(memory)
		if err != nil {
			return err
		}
		shared[record.MemoryID] = record
	}
	m.mu.Lock()
	for id, node := range directory {
		m.directory[id] = node
	}
	for id, record := range shared {
		m.shared[id] = record
	}
	m.mu.Unlock()
	return nil
}
