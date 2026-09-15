package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

func remoteEvidenceEpisodeFactMemory(fact *RemoteEvidenceEpisodeFact) (*Memory, error) {
	if fact == nil || strings.TrimSpace(fact.ID) == "" {
		return nil, errors.New("remote evidence episode fact unavailable")
	}
	raw, err := json.Marshal(fact)
	if err != nil {
		return nil, err
	}
	fingerprints, err := json.Marshal(fact.Evidence)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID: fact.ID, Layer: "emergent", Tags: []string{"memory", remoteEvidenceEpisodeFactTag},
		Content: "Memory-authored decision over one transient multi-remote evidence episode.", Revision: 1,
		State: map[string]any{
			"structure_id": fact.StructureID, "contract_digest": fact.ContractDigest,
			"episode_digest": fact.EpisodeDigest, "evidence_count": len(fact.Evidence),
			"evidence_fingerprints": string(fingerprints), "decision": fact.Decision,
			"local_experience_id": fact.LocalExperienceID, "episode_fact": string(raw),
		},
	}, nil
}

func remoteEvidenceEpisodeFactExists(e *Engine, factID string) (bool, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return false, errors.New("remote evidence episode engine unavailable")
	}
	_, memory, err := root.resolveLocalFabricMemory(strings.TrimSpace(factID))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	if memory == nil || !memoryHasTag(memory, remoteEvidenceEpisodeFactTag) {
		return false, fmt.Errorf("remote evidence episode fact id collides with non-fact Memory: %s", factID)
	}
	return true, nil
}

func addRemoteEvidenceEpisodeProvenance(observation map[string]string, evidences []*RemoteMemoryEvidence) {
	observation["remote_evidence_count"] = strconv.Itoa(len(evidences))
	observation["remote_evidence_episode_digest"] = remoteEvidenceEpisodeDigest(evidences)
	for index, evidence := range evidences {
		prefix := fmt.Sprintf("remote_evidence.%d.", index)
		observation[prefix+"kind"] = evidence.Kind
		observation[prefix+"id"] = evidence.EvidenceID
		observation[prefix+"origin_node"] = evidence.OriginNode
		observation[prefix+"backing_memory_id"] = evidence.BackingMemoryID
		observation[prefix+"backing_revision"] = fmt.Sprint(evidence.BackingRevision)
		observation[prefix+"payload_digest"] = evidence.PayloadDigest
		observation[prefix+"read_nano"] = fmt.Sprint(evidence.ReadNano)
		observation[prefix+"grant_nonce"] = evidence.GrantNonce
	}
}

func buildRemoteEvidenceEpisodeExperience(experiences *experienceLedger, structure *MemoryStructure, evidences []*RemoteMemoryEvidence, frame *Frame, factID string) (*MemoryExperience, error) {
	parentID, err := remoteEvidenceExperienceParent(experiences, structure, frame)
	if err != nil {
		return nil, err
	}
	var parentIDs []string
	if parentID != "" {
		parentIDs = []string{parentID}
	}
	context := mergeStringMap(structure.Context, framePrefixedStringMap(frame.Vars, remoteEvidenceExperienceContextPrefix))
	observation := mergeStringMap(structure.Observation, framePrefixedStringMap(frame.Vars, remoteEvidenceExperienceObservationPrefix))
	if observation == nil {
		observation = map[string]string{}
	}
	observation["memory_structure_id"] = structure.ID
	addRemoteEvidenceEpisodeProvenance(observation, evidences)
	action := mergeStringMap(structure.Action, framePrefixedStringMap(frame.Vars, remoteEvidenceExperienceActionPrefix))
	if action == nil {
		action = map[string]string{}
	}
	action["memory_structure_id"] = structure.ID
	action["remote_evidence_episode_fact_id"] = factID
	outcome := framePrefixedStringMap(frame.Vars, remoteEvidenceExperienceOutcomePrefix)
	predictionError := framePrefixedStringMap(frame.Vars, remoteEvidenceExperiencePredictionErrorPrefix)
	if len(predictionError) == 0 && len(structure.ExpectedOutcome) > 0 {
		predictionError = calculatePredictionError(structure.ExpectedOutcome, outcome)
	}
	predictionHash := strings.TrimSpace(frame.Vars[remoteEvidencePredictionHashVar])
	if predictionHash == "" {
		predictionHash = structure.PredictionHash
	}
	id := remoteEvidenceEpisodeExperienceID(factID)
	if existing, ok := experiences.Get(id); ok {
		return existing, nil
	}
	return experiences.Record(MemoryExperience{ID: id, ParentIDs: parentIDs, Context: context, Observation: observation, Action: action, Outcome: outcome, PredictionHash: predictionHash, PredictionError: predictionError})
}

func persistRemoteEvidenceEpisodeDecision(e *Engine, experiences *experienceLedger, formation *memoryStructureFormation, validation *memoryStructureValidationLedger, fact *RemoteEvidenceEpisodeFact) error {
	root := memoryGrowthRoot(e)
	if experiences != nil && formation != nil && validation != nil {
		if err := PersistMemoryGrowthState(root, experiences, formation, validation); err != nil {
			return err
		}
	}
	memory, err := remoteEvidenceEpisodeFactMemory(fact)
	if err != nil {
		return err
	}
	if err := root.upsertExplicitMemoryBounded(memory); err != nil {
		return err
	}
	return root.persistAll()
}

func runRemoteEvidenceEpisodeCycle(e *Engine, reader remoteEvidenceReader) (*RemoteEvidenceEpisodeResult, error) {
	result := &RemoteEvidenceEpisodeResult{}
	root := memoryGrowthRoot(e)
	if root == nil || reader == nil {
		return result, errors.New("remote evidence episode runtime unavailable")
	}
	if !enterRemoteEvidenceIntake(root) {
		result.Skipped = true
		return result, nil
	}
	defer leaveRemoteEvidenceIntake(root)
	experiences, formation, validation, err := LoadMemoryGrowthState(root)
	if err != nil {
		return result, err
	}
	structures := validation.Snapshot()
	sort.Slice(structures, func(i, j int) bool { return structures[i].ID < structures[j].ID })
	var readErrors []string
	for _, structure := range structures {
		inputs, enabled, contractErr := remoteEvidenceEpisodeContract(structure)
		if !enabled {
			continue
		}
		if contractErr != nil {
			return result, fmt.Errorf("remote evidence episode contract %s: %w", structure.ID, contractErr)
		}
		evidences := make([]*RemoteMemoryEvidence, 0, len(inputs))
		failed := false
		for index, input := range inputs {
			evidence, readErr := reader(input.Kind, input.EvidenceID)
			if readErr == nil {
				readErr = validateRemoteEvidenceEpisodeRead(input, evidence)
			}
			if readErr != nil {
				readErrors = append(readErrors, fmt.Sprintf("%s[%d]=%s/%s: %v", structure.ID, index, input.Kind, input.EvidenceID, readErr))
				failed = true
				break
			}
			evidences = append(evidences, evidence)
		}
		if failed {
			continue
		}
		contractDigest := remoteEvidenceContractDigest(structure)
		factID := remoteEvidenceEpisodeIdentity(structure.ID, contractDigest, evidences)
		exists, err := remoteEvidenceEpisodeFactExists(root, factID)
		if err != nil {
			return result, err
		}
		if exists {
			continue
		}
		frame, err := executeRemoteEvidenceEpisodeMemoryDecision(root, structure, evidences)
		if err != nil {
			return result, err
		}
		fact := &RemoteEvidenceEpisodeFact{ID: factID, StructureID: structure.ID, ContractDigest: contractDigest, EpisodeDigest: remoteEvidenceEpisodeDigest(evidences), DecisionReason: strings.TrimSpace(frame.Vars[remoteEvidenceReasonVar]), CreatedNano: time.Now().UnixNano()}
		for _, evidence := range evidences {
			fact.Evidence = append(fact.Evidence, remoteEvidenceEpisodeFingerprint(evidence))
		}
		result.StructureID, result.EvidenceCount, result.EpisodeDigest, result.EpisodeFactID = structure.ID, len(evidences), fact.EpisodeDigest, fact.ID
		if !remoteEvidenceIntakeEnabled(frame.Vars[remoteEvidenceCommitVar]) {
			fact.Decision = remoteEvidenceDecisionObserved
			if err := persistRemoteEvidenceEpisodeDecision(root, nil, nil, nil, fact); err != nil {
				return result, err
			}
			result.Rejected, result.Persisted = true, true
			return result, nil
		}
		experience, err := buildRemoteEvidenceEpisodeExperience(experiences, structure, evidences, frame, fact.ID)
		if err != nil {
			return result, err
		}
		if len(structure.ExpectedOutcome) > 0 {
			if _, err := UpdateMemoryBeliefsFromExperience(root, structure, experience); err != nil {
				return result, err
			}
		}
		fact.Decision, fact.LocalExperienceID = remoteEvidenceDecisionCommitted, experience.ID
		if err := persistRemoteEvidenceEpisodeDecision(root, experiences, formation, validation, fact); err != nil {
			return result, err
		}
		result.LocalExperienceID, result.Committed, result.Persisted = experience.ID, true, true
		return result, nil
	}
	result.Skipped = true
	if len(readErrors) > 0 {
		return result, fmt.Errorf("remote evidence episode unavailable: %s", strings.Join(readErrors, "; "))
	}
	return result, nil
}

func RunAutonomousRemoteEvidenceEpisodeCycle(e *Engine) (*RemoteEvidenceEpisodeResult, error) {
	return runRemoteEvidenceEpisodeCycle(e, ReadRemoteMemoryEvidence)
}
