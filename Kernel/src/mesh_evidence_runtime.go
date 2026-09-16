package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RemoteMemoryEvidence is a transient, read-only physical view of one Memory
// that still belongs to another node. Kind is an opaque Memory-authored label;
// Kernel deliberately does not maintain a table of cognitive evidence types.
type RemoteMemoryEvidence struct {
	Kind             string          `json:"kind"`
	EvidenceID       string          `json:"evidence_id"`
	OriginNode       string          `json:"origin_node"`
	OriginEndpoint   string          `json:"origin_endpoint,omitempty"`
	RequesterNode    string          `json:"requester_node"`
	BackingMemoryID  string          `json:"backing_memory_id"`
	BackingRevision  uint64          `json:"backing_revision"`
	ReadNano         int64           `json:"read_nano"`
	GrantNonce       string          `json:"grant_nonce"`
	GrantExpiresUnix int64           `json:"grant_expires_unix"`
	PayloadDigest    string          `json:"payload_digest"`
	Payload          json.RawMessage `json:"payload"`
}

// normalizeRemoteEvidenceKind enforces only a physical envelope constraint.
// Meaning belongs to executable Memory, so new evidence classes never require
// a Kernel release.
func normalizeRemoteEvidenceKind(kind string) (string, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", errors.New("remote evidence kind required")
	}
	if len(kind) > 128 {
		return "", errors.New("remote evidence kind exceeds physical limit")
	}
	return kind, nil
}

// remoteEvidenceBackingMemoryID is intentionally identity-only. Evidence must
// already be a discrete Memory; Kernel no longer knows about historical
// monolithic Growth ledgers or Belief/Context/Action Go structs.
func remoteEvidenceBackingMemoryID(kind, evidenceID string) (string, error) {
	if _, err := normalizeRemoteEvidenceKind(kind); err != nil {
		return "", err
	}
	evidenceID = strings.TrimSpace(evidenceID)
	if evidenceID == "" {
		return "", errors.New("remote evidence id required")
	}
	return evidenceID, nil
}

func remoteEvidencePayload(kind, evidenceID string, memory *Memory) ([]byte, error) {
	if _, err := normalizeRemoteEvidenceKind(kind); err != nil {
		return nil, err
	}
	if memory == nil {
		return nil, errors.New("remote evidence backing Memory unavailable")
	}
	if strings.TrimSpace(memory.ID) != strings.TrimSpace(evidenceID) {
		return nil, fmt.Errorf("remote evidence backing Memory identity mismatch: %s != %s", memory.ID, evidenceID)
	}
	// Preserve the source Memory as factual evidence. No kind-specific parsing,
	// ranking or semantic projection occurs in Kernel.
	return json.Marshal(memory)
}

func transientRemoteEvidence(
	kind, evidenceID, requester string,
	record MeshRecord,
	grant *MeshGrant,
	memory *Memory,
) (*RemoteMemoryEvidence, error) {
	if grant == nil {
		return nil, errors.New("remote evidence Sovereign grant unavailable")
	}
	kind, err := normalizeRemoteEvidenceKind(kind)
	if err != nil {
		return nil, err
	}
	payload, err := remoteEvidencePayload(kind, evidenceID, memory)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	return &RemoteMemoryEvidence{
		Kind:             kind,
		EvidenceID:       strings.TrimSpace(evidenceID),
		OriginNode:       record.OriginNode,
		OriginEndpoint:   record.Endpoint,
		RequesterNode:    strings.TrimSpace(requester),
		BackingMemoryID:  memory.ID,
		BackingRevision:  memory.Revision,
		ReadNano:         time.Now().UnixNano(),
		GrantNonce:       grant.Nonce,
		GrantExpiresUnix: grant.ExpiresUnix,
		PayloadDigest:    hex.EncodeToString(sum[:]),
		Payload:          append(json.RawMessage(nil), payload...),
	}, nil
}

// remoteEvidenceRead performs a fresh Sovereign-authorized direct read every
// time. It never creates a requester-side persistent cache. Origin loss means
// the evidence is unavailable until that origin becomes reachable again.
func (m *meshRuntime) remoteEvidenceRead(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
	if m == nil {
		return nil, errors.New("mesh runtime unavailable")
	}
	kind, err := normalizeRemoteEvidenceKind(kind)
	if err != nil {
		return nil, err
	}
	backingID, err := remoteEvidenceBackingMemoryID(kind, evidenceID)
	if err != nil {
		return nil, err
	}
	record, grant, err := m.requestSharedGrant(backingID, "shared_fetch")
	if err != nil {
		return nil, fmt.Errorf("remote evidence authorization failed: %w", err)
	}
	m.mu.RLock()
	requester := m.nodeID
	m.mu.RUnlock()

	var memory *Memory
	if record.OriginNode == requester {
		res := m.serveLocalMemory(record.MemoryID)
		if !res.OK || res.Memory == nil {
			if res.Error == "" {
				res.Error = "local evidence Memory unavailable"
			}
			return nil, errors.New(res.Error)
		}
		memory = res.Memory
	} else {
		res, rpcErr := m.rpc(record.Endpoint, MeshRequest{
			Op: "shared_fetch", MemoryID: record.MemoryID, Grant: grant,
		})
		if rpcErr != nil {
			return nil, fmt.Errorf("remote evidence forgotten: %w", rpcErr)
		}
		if res.Memory == nil {
			return nil, errors.New("remote evidence forgotten: origin returned no Memory")
		}
		memory = res.Memory
	}
	if memory.ID != record.MemoryID || memory.ID != backingID {
		return nil, errors.New("remote evidence backing Memory identity mismatch")
	}
	if err := validateMemoryCapabilities(memory, true); err != nil {
		return nil, err
	}
	return transientRemoteEvidence(kind, evidenceID, requester, record, grant, memory)
}

func ReadRemoteMemoryEvidence(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
	m := meshRuntimeCurrent()
	if m == nil {
		return nil, errors.New("remote Memory evidence unavailable in standalone mode")
	}
	return m.remoteEvidenceRead(kind, evidenceID)
}
