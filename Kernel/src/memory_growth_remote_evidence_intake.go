package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

const (
	remoteEvidenceIntakeActionKey = "remote_evidence_intake"
	remoteEvidenceKindActionKey   = "remote_evidence_kind"
	remoteEvidenceIDActionKey     = "remote_evidence_id"

	remoteEvidenceCommitVar           = "memory_remote_evidence_commit"
	remoteEvidenceReasonVar           = "memory_remote_evidence_reason"
	remoteEvidenceParentExperienceVar = "memory_experience_parent_id"
	remoteEvidencePredictionHashVar   = "memory_experience_prediction_hash"

	remoteEvidenceExperienceContextPrefix         = "memory_experience_context."
	remoteEvidenceExperienceObservationPrefix     = "memory_experience_observation."
	remoteEvidenceExperienceActionPrefix          = "memory_experience_action."
	remoteEvidenceExperienceOutcomePrefix         = "memory_experience_outcome."
	remoteEvidenceExperiencePredictionErrorPrefix = "memory_experience_prediction_error."

	remoteEvidenceIntakeFactTag     = "memory-remote-evidence-intake-fact"
	remoteEvidenceDecisionCommitted = "committed"
	remoteEvidenceDecisionObserved  = "observed_not_committed"
)

// RemoteEvidenceIntakeFact records only the local Memory decision/provenance
// fingerprint for one transient remote read. It never stores the remote payload.
// Re-evaluation occurs when either the remote backing revision/content or the
// Memory-owned intake contract changes.
type RemoteEvidenceIntakeFact struct {
	ID                string `json:"id"`
	StructureID       string `json:"structure_id"`
	ContractDigest    string `json:"contract_digest"`
	EvidenceKind      string `json:"evidence_kind"`
	EvidenceID        string `json:"evidence_id"`
	OriginNode        string `json:"origin_node"`
	BackingMemoryID   string `json:"backing_memory_id"`
	BackingRevision   uint64 `json:"backing_revision"`
	PayloadDigest     string `json:"payload_digest"`
	ReadNano          int64  `json:"read_nano"`
	GrantNonce        string `json:"grant_nonce,omitempty"`
	Decision          string `json:"decision"`
	DecisionReason    string `json:"decision_reason,omitempty"`
	LocalExperienceID string `json:"local_experience_id,omitempty"`
	CreatedNano       int64  `json:"created_nano"`
}

// RemoteEvidenceIntakeResult reports one opportunity-driven Memory decision.
type RemoteEvidenceIntakeResult struct {
	StructureID       string `json:"structure_id,omitempty"`
	EvidenceKind      string `json:"evidence_kind,omitempty"`
	EvidenceID        string `json:"evidence_id,omitempty"`
	OriginNode        string `json:"origin_node,omitempty"`
	PayloadDigest     string `json:"payload_digest,omitempty"`
	IntakeFactID      string `json:"intake_fact_id,omitempty"`
	LocalExperienceID string `json:"local_experience_id,omitempty"`
	Committed         bool   `json:"committed,omitempty"`
	Rejected          bool   `json:"rejected,omitempty"`
	Persisted         bool   `json:"persisted,omitempty"`
	Skipped           bool   `json:"skipped,omitempty"`
}

type remoteEvidenceReader func(kind, evidenceID string) (*RemoteMemoryEvidence, error)

var remoteEvidenceIntakeActive sync.Map

func enterRemoteEvidenceIntake(e *Engine) bool {
	root := memoryGrowthRoot(e)
	if root == nil {
		return false
	}
	_, loaded := remoteEvidenceIntakeActive.LoadOrStore(root, struct{}{})
	return !loaded
}

func leaveRemoteEvidenceIntake(e *Engine) {
	if root := memoryGrowthRoot(e); root != nil {
		remoteEvidenceIntakeActive.Delete(root)
	}
}

func remoteEvidenceIntakeEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func remoteEvidenceIntakeContract(structure *MemoryStructure) (kind, evidenceID string, ok bool) {
	if structure == nil || structure.State != memoryStructureValidatedState || len(structure.Program) == 0 {
		return "", "", false
	}
	if !remoteEvidenceIntakeEnabled(structure.Action[remoteEvidenceIntakeActionKey]) {
		return "", "", false
	}
	kind, err := normalizeRemoteEvidenceKind(structure.Action[remoteEvidenceKindActionKey])
	if err != nil {
		return "", "", false
	}
	evidenceID = strings.TrimSpace(structure.Action[remoteEvidenceIDActionKey])
	return kind, evidenceID, evidenceID != ""
}

func remoteEvidenceContractDigest(structure *MemoryStructure) string {
	if structure == nil {
		return ""
	}
	payload := struct {
		Context         map[string]string `json:"context,omitempty"`
		Observation     map[string]string `json:"observation,omitempty"`
		Action          map[string]string `json:"action,omitempty"`
		ExpectedOutcome map[string]string `json:"expected_outcome,omitempty"`
		PredictionHash  string            `json:"prediction_hash,omitempty"`
		Program         []Op              `json:"program,omitempty"`
	}{
		Context: cloneStringMap(structure.Context), Observation: cloneStringMap(structure.Observation),
		Action: cloneStringMap(structure.Action), ExpectedOutcome: cloneStringMap(structure.ExpectedOutcome),
		PredictionHash: strings.TrimSpace(structure.PredictionHash), Program: append([]Op(nil), structure.Program...),
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func remoteEvidenceIntakeIdentity(structureID, contractDigest string, evidence *RemoteMemoryEvidence) string {
	if evidence == nil {
		return ""
	}
	payload := strings.Join([]string{
		strings.TrimSpace(structureID), strings.TrimSpace(contractDigest),
		strings.TrimSpace(evidence.Kind), strings.TrimSpace(evidence.EvidenceID),
		strings.TrimSpace(evidence.OriginNode), strings.TrimSpace(evidence.BackingMemoryID),
		fmt.Sprint(evidence.BackingRevision), strings.TrimSpace(evidence.PayloadDigest),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return "remote-evidence-intake-" + hex.EncodeToString(sum[:16])
}

func remoteEvidenceLocalExperienceID(factID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(factID)))
	return "remote-evidence-experience-" + hex.EncodeToString(sum[:16])
}

func remoteEvidenceIntakeFactMemory(fact *RemoteEvidenceIntakeFact) (*Memory, error) {
	if fact == nil || strings.TrimSpace(fact.ID) == "" {
		return nil, errors.New("remote evidence intake fact unavailable")
	}
	raw, err := json.Marshal(fact)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:       fact.ID,
		Layer:    "emergent",
		Tags:     []string{"memory", remoteEvidenceIntakeFactTag},
		Content:  "Memory-authored decision over a transient remote evidence read.",
		Revision: 1,
		State: map[string]any{
			"structure_id":        fact.StructureID,
			"evidence_kind":       fact.EvidenceKind,
			"evidence_id":         fact.EvidenceID,
			"origin_node":         fact.OriginNode,
			"backing_memory_id":   fact.BackingMemoryID,
			"backing_revision":    fact.BackingRevision,
			"payload_digest":      fact.PayloadDigest,
			"contract_digest":     fact.ContractDigest,
			"decision":            fact.Decision,
			"local_experience_id": fact.LocalExperienceID,
			"intake_fact":         string(raw),
		},
	}, nil
}

func remoteEvidenceIntakeFactExists(e *Engine, factID string) (bool, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return false, errors.New("remote evidence intake engine unavailable")
	}
	_, memory, err := root.resolveLocalFabricMemory(strings.TrimSpace(factID))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	if memory == nil || !memoryHasTag(memory, remoteEvidenceIntakeFactTag) {
		return false, fmt.Errorf("remote evidence intake fact id collides with non-fact Memory: %s", factID)
	}
	return true, nil
}

func mergeStringMap(base, overlay map[string]string) map[string]string {
	out := cloneStringMap(base)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range overlay {
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
