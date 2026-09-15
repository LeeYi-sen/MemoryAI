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

const (
	remoteEvidenceExperience     = "experience"
	remoteEvidenceBelief         = "belief"
	remoteEvidenceContextSplit   = "context_split"
	remoteEvidenceActionInstance = "grounded_action_instance"
)

// RemoteMemoryEvidence is a transient, read-only view of evidence that still
// physically belongs to another Memory node. It is never persisted by the
// requester. Provenance identifies the exact remote body/revision and the
// short-lived Sovereign authorization that permitted this read.
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

func normalizeRemoteEvidenceKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case remoteEvidenceExperience:
		return remoteEvidenceExperience, nil
	case remoteEvidenceBelief:
		return remoteEvidenceBelief, nil
	case remoteEvidenceContextSplit, "context-split", "contextsplit":
		return remoteEvidenceContextSplit, nil
	case remoteEvidenceActionInstance, "action_instance", "action-instance":
		return remoteEvidenceActionInstance, nil
	default:
		return "", fmt.Errorf("unsupported remote evidence kind %q", kind)
	}
}

func remoteEvidenceBackingMemoryID(kind, evidenceID string) (string, error) {
	kind, err := normalizeRemoteEvidenceKind(kind)
	if err != nil {
		return "", err
	}
	evidenceID = strings.TrimSpace(evidenceID)
	if evidenceID == "" {
		return "", errors.New("remote evidence id required")
	}
	if kind == remoteEvidenceExperience {
		return memoryGrowthStateRecordID, nil
	}
	return evidenceID, nil
}

func remoteEvidencePayload(kind, evidenceID string, memory *Memory) ([]byte, error) {
	if memory == nil {
		return nil, errors.New("remote evidence backing Memory unavailable")
	}
	kind, err := normalizeRemoteEvidenceKind(kind)
	if err != nil {
		return nil, err
	}
	evidenceID = strings.TrimSpace(evidenceID)
	switch kind {
	case remoteEvidenceExperience:
		if memory.ID != memoryGrowthStateRecordID {
			return nil, fmt.Errorf("remote Experience backing Memory mismatch: %s", memory.ID)
		}
		value, ok := memory.State["memory_growth_state"]
		if !ok {
			return nil, errors.New("remote Memory Growth state unavailable")
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
				return nil, err
			}
		}
		state, err := decodeMemoryGrowthState(raw)
		if err != nil {
			return nil, err
		}
		for _, experience := range state.Experiences {
			if experience != nil && experience.ID == evidenceID {
				return json.Marshal(experience)
			}
		}
		return nil, fmt.Errorf("remote Experience forgotten: %s", evidenceID)
	case remoteEvidenceBelief:
		if memory.ID != evidenceID || !memoryHasTag(memory, memoryBeliefTag) {
			return nil, fmt.Errorf("remote Belief unavailable: %s", evidenceID)
		}
		state, err := decodeMemoryBeliefState(memory.State["belief_state"])
		if err != nil {
			return nil, err
		}
		if state == nil || state.ID != evidenceID {
			return nil, fmt.Errorf("remote Belief identity drift: %s", evidenceID)
		}
		return json.Marshal(state)
	case remoteEvidenceContextSplit:
		if memory.ID != evidenceID || !memoryHasTag(memory, memoryContextSplitTag) {
			return nil, fmt.Errorf("remote Context Split unavailable: %s", evidenceID)
		}
		state, err := decodeMemoryContextSplitState(memory.State["split_state"])
		if err != nil {
			return nil, err
		}
		if state == nil || state.ID != evidenceID {
			return nil, fmt.Errorf("remote Context Split identity drift: %s", evidenceID)
		}
		return json.Marshal(state)
	case remoteEvidenceActionInstance:
		if memory.ID != evidenceID || !memoryHasTag(memory, groundedActionInstanceTag) {
			return nil, fmt.Errorf("remote Grounded Action Instance unavailable: %s", evidenceID)
		}
		state, err := decodeGroundedActionInstance(memory.State["instance_state"])
		if err != nil {
			return nil, err
		}
		if state == nil || state.ID != evidenceID {
			return nil, fmt.Errorf("remote Grounded Action Instance identity drift: %s", evidenceID)
		}
		return json.Marshal(state)
	default:
		return nil, fmt.Errorf("unsupported remote evidence kind %q", kind)
	}
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
	payload, err := remoteEvidencePayload(kind, evidenceID, memory)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	return &RemoteMemoryEvidence{
		Kind:             strings.TrimSpace(kind),
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

// remoteEvidenceRead performs a fresh Sovereign-authorized read every time.
// It deliberately does not use or create a requester-side Memory cache. When
// the origin is unreachable, the evidence is unavailable ("forgotten") until
// a later call can reach the origin again.
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
	if memory.ID != record.MemoryID {
		return nil, errors.New("remote evidence backing Memory identity mismatch")
	}
	if err := validateMemoryCapabilities(memory, true); err != nil {
		return nil, err
	}
	return transientRemoteEvidence(kind, evidenceID, requester, record, grant, memory)
}

// ReadRemoteMemoryEvidence is the MemoryAI-facing direct-read entrypoint. It
// returns a transient value only; callers that want durable knowledge must let
// Memory form new local Experience through an explicit later process.
func ReadRemoteMemoryEvidence(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
	m := meshRuntimeCurrent()
	if m == nil {
		return nil, errors.New("remote Memory evidence unavailable in standalone mode")
	}
	return m.remoteEvidenceRead(kind, evidenceID)
}
