package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	remoteEvidenceInputsActionKey      = "remote_evidence_inputs"
	remoteEvidenceEpisodeFactTag       = "memory-remote-evidence-episode-fact"
	remoteEvidenceEpisodeMaxInputs     = 32
	remoteEvidenceEpisodeCountFrameKey = "remote_evidence_count"
)

type RemoteEvidenceInput struct {
	Kind       string `json:"kind"`
	EvidenceID string `json:"id"`
}

type RemoteEvidenceFingerprint struct {
	Kind            string `json:"kind"`
	EvidenceID      string `json:"evidence_id"`
	OriginNode      string `json:"origin_node"`
	BackingMemoryID string `json:"backing_memory_id"`
	BackingRevision uint64 `json:"backing_revision"`
	PayloadDigest   string `json:"payload_digest"`
	ReadNano        int64  `json:"read_nano"`
	GrantNonce      string `json:"grant_nonce,omitempty"`
}

type RemoteEvidenceEpisodeFact struct {
	ID                string                      `json:"id"`
	StructureID       string                      `json:"structure_id"`
	ContractDigest    string                      `json:"contract_digest"`
	EpisodeDigest     string                      `json:"episode_digest"`
	Evidence          []RemoteEvidenceFingerprint `json:"evidence"`
	Decision          string                      `json:"decision"`
	DecisionReason    string                      `json:"decision_reason,omitempty"`
	LocalExperienceID string                      `json:"local_experience_id,omitempty"`
	CreatedNano       int64                       `json:"created_nano"`
}

type RemoteEvidenceEpisodeResult struct {
	StructureID       string `json:"structure_id,omitempty"`
	EvidenceCount     int    `json:"evidence_count,omitempty"`
	EpisodeDigest     string `json:"episode_digest,omitempty"`
	EpisodeFactID     string `json:"episode_fact_id,omitempty"`
	LocalExperienceID string `json:"local_experience_id,omitempty"`
	Committed         bool   `json:"committed,omitempty"`
	Rejected          bool   `json:"rejected,omitempty"`
	Persisted         bool   `json:"persisted,omitempty"`
	Skipped           bool   `json:"skipped,omitempty"`
}

func normalizeRemoteEvidenceInputs(inputs []RemoteEvidenceInput) ([]RemoteEvidenceInput, error) {
	if len(inputs) == 0 {
		return nil, errors.New("remote evidence input list is empty")
	}
	if len(inputs) > remoteEvidenceEpisodeMaxInputs {
		return nil, fmt.Errorf("remote evidence episode exceeds physical input quota: %d > %d", len(inputs), remoteEvidenceEpisodeMaxInputs)
	}
	out := make([]RemoteEvidenceInput, 0, len(inputs))
	seen := map[string]struct{}{}
	for index, input := range inputs {
		kind, err := normalizeRemoteEvidenceKind(input.Kind)
		if err != nil {
			return nil, fmt.Errorf("remote evidence input %d: %w", index, err)
		}
		id := strings.TrimSpace(input.EvidenceID)
		if id == "" {
			return nil, fmt.Errorf("remote evidence input %d id required", index)
		}
		key := kind + "\x00" + id
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate remote evidence input: %s/%s", kind, id)
		}
		seen[key] = struct{}{}
		out = append(out, RemoteEvidenceInput{Kind: kind, EvidenceID: id})
	}
	return out, nil
}

func remoteEvidenceEpisodeContract(structure *MemoryStructure) ([]RemoteEvidenceInput, bool, error) {
	if structure == nil || structure.State != memoryStructureValidatedState || len(structure.Program) == 0 {
		return nil, false, nil
	}
	if !remoteEvidenceIntakeEnabled(structure.Action[remoteEvidenceIntakeActionKey]) {
		return nil, false, nil
	}
	raw := strings.TrimSpace(structure.Action[remoteEvidenceInputsActionKey])
	if raw == "" {
		return nil, false, nil
	}
	var inputs []RemoteEvidenceInput
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return nil, true, fmt.Errorf("invalid Memory-authored remote_evidence_inputs: %w", err)
	}
	normalized, err := normalizeRemoteEvidenceInputs(inputs)
	return normalized, true, err
}

func remoteEvidenceEpisodeFingerprint(evidence *RemoteMemoryEvidence) RemoteEvidenceFingerprint {
	if evidence == nil {
		return RemoteEvidenceFingerprint{}
	}
	return RemoteEvidenceFingerprint{
		Kind: evidence.Kind, EvidenceID: evidence.EvidenceID, OriginNode: evidence.OriginNode,
		BackingMemoryID: evidence.BackingMemoryID, BackingRevision: evidence.BackingRevision,
		PayloadDigest: evidence.PayloadDigest, ReadNano: evidence.ReadNano, GrantNonce: evidence.GrantNonce,
	}
}

func remoteEvidenceEpisodeDigest(evidences []*RemoteMemoryEvidence) string {
	type stableFingerprint struct {
		Kind            string `json:"kind"`
		EvidenceID      string `json:"evidence_id"`
		OriginNode      string `json:"origin_node"`
		BackingMemoryID string `json:"backing_memory_id"`
		BackingRevision uint64 `json:"backing_revision"`
		PayloadDigest   string `json:"payload_digest"`
	}
	stable := make([]stableFingerprint, 0, len(evidences))
	for _, evidence := range evidences {
		if evidence == nil {
			stable = append(stable, stableFingerprint{})
			continue
		}
		stable = append(stable, stableFingerprint{
			Kind: evidence.Kind, EvidenceID: evidence.EvidenceID, OriginNode: evidence.OriginNode,
			BackingMemoryID: evidence.BackingMemoryID, BackingRevision: evidence.BackingRevision,
			PayloadDigest: evidence.PayloadDigest,
		})
	}
	raw, _ := json.Marshal(stable)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func remoteEvidenceEpisodeIdentity(structureID, contractDigest string, evidences []*RemoteMemoryEvidence) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(structureID), strings.TrimSpace(contractDigest), remoteEvidenceEpisodeDigest(evidences),
	}, "\x00")))
	return "remote-evidence-episode-" + hex.EncodeToString(sum[:16])
}

func remoteEvidenceEpisodeExperienceID(factID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(factID)))
	return "remote-evidence-episode-experience-" + hex.EncodeToString(sum[:16])
}

func validateRemoteEvidenceEpisodeRead(input RemoteEvidenceInput, evidence *RemoteMemoryEvidence) error {
	if evidence == nil {
		return errors.New("remote evidence unavailable")
	}
	kind, err := normalizeRemoteEvidenceKind(evidence.Kind)
	if err != nil {
		return err
	}
	if kind != input.Kind || strings.TrimSpace(evidence.EvidenceID) != input.EvidenceID {
		return fmt.Errorf("remote evidence identity mismatch: requested=%s/%s got=%s/%s", input.Kind, input.EvidenceID, evidence.Kind, evidence.EvidenceID)
	}
	if strings.TrimSpace(evidence.OriginNode) == "" || strings.TrimSpace(evidence.BackingMemoryID) == "" || strings.TrimSpace(evidence.PayloadDigest) == "" {
		return errors.New("remote evidence provenance incomplete")
	}
	return nil
}

func remoteEvidenceEpisodeDecisionFrame(evidences []*RemoteMemoryEvidence, structureID string) (*Frame, error) {
	if len(evidences) == 0 {
		return nil, errors.New("remote evidence episode unavailable")
	}
	f := newFrame()
	if f.Vars == nil {
		f.Vars = map[string]string{}
	}
	if f.Lists == nil {
		f.Lists = map[string][]string{}
	}
	f.Vars["__subject"] = structureID
	f.Vars[remoteEvidenceEpisodeCountFrameKey] = strconv.Itoa(len(evidences))
	for index, evidence := range evidences {
		one, err := remoteEvidenceDecisionFrame(evidence, structureID)
		if err != nil {
			return nil, fmt.Errorf("remote evidence input %d: %w", index, err)
		}
		prefix := "remote_evidence." + strconv.Itoa(index) + "."
		for key, value := range one.Vars {
			if key != "__subject" {
				f.Vars[prefix+key] = value
			}
		}
		for key, values := range one.Lists {
			f.Lists[prefix+key] = append([]string(nil), values...)
		}
		f.Lists["remote_evidence_kinds"] = append(f.Lists["remote_evidence_kinds"], evidence.Kind)
		f.Lists["remote_evidence_ids"] = append(f.Lists["remote_evidence_ids"], evidence.EvidenceID)
		f.Lists["remote_evidence_origin_nodes"] = append(f.Lists["remote_evidence_origin_nodes"], evidence.OriginNode)
		f.Lists["remote_evidence_payload_digests"] = append(f.Lists["remote_evidence_payload_digests"], evidence.PayloadDigest)
	}
	return f, nil
}

func executeRemoteEvidenceEpisodeMemoryDecision(e *Engine, structure *MemoryStructure, evidences []*RemoteMemoryEvidence) (*Frame, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("remote evidence episode engine unavailable")
	}
	if structure == nil || structure.State != memoryStructureValidatedState || len(structure.Program) == 0 {
		return nil, errors.New("remote evidence episode Structure is not validated/executable")
	}
	_, executable, err := root.resolveLocalFabricMemory(structure.ID)
	if err != nil {
		return nil, fmt.Errorf("remote evidence episode executable unavailable: %w", err)
	}
	if !equalStringOpProgram(executable.Program, structure.Program) {
		return nil, errors.New("remote evidence episode executable program drift")
	}
	frame, err := remoteEvidenceEpisodeDecisionFrame(evidences, structure.ID)
	if err != nil {
		return nil, err
	}
	if err := globalTxnScheduler.canonical(root, structure.ID, frame); err != nil {
		return nil, fmt.Errorf("remote evidence episode Memory decision execution failed: %w", err)
	}
	return frame, nil
}
