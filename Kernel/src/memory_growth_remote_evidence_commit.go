package main

import (
	"errors"
	"fmt"
	"strings"
)

func remoteEvidenceExperienceParent(experiences *experienceLedger, structure *MemoryStructure, frame *Frame) (string, error) {
	if experiences == nil {
		return "", errors.New("experience ledger unavailable")
	}
	if frame != nil {
		if parentID := strings.TrimSpace(frame.Vars[remoteEvidenceParentExperienceVar]); parentID != "" {
			if _, ok := experiences.Get(parentID); !ok {
				return "", fmt.Errorf("Memory-authored remote evidence parent not found: %s", parentID)
			}
			return parentID, nil
		}
	}
	if structure == nil {
		return "", nil
	}
	return firstExistingExperienceID(experiences, structure.SourceExperienceIDs), nil
}

func buildRemoteEvidenceLocalExperience(
	experiences *experienceLedger,
	structure *MemoryStructure,
	evidence *RemoteMemoryEvidence,
	frame *Frame,
	factID string,
) (*MemoryExperience, error) {
	if experiences == nil || structure == nil || evidence == nil || frame == nil {
		return nil, errors.New("remote evidence local experience inputs unavailable")
	}
	parentID, err := remoteEvidenceExperienceParent(experiences, structure, frame)
	if err != nil {
		return nil, err
	}
	parentIDs := []string(nil)
	if parentID != "" {
		parentIDs = []string{parentID}
	}

	context := mergeStringMap(structure.Context, framePrefixedStringMap(frame.Vars, remoteEvidenceExperienceContextPrefix))
	observation := mergeStringMap(structure.Observation, framePrefixedStringMap(frame.Vars, remoteEvidenceExperienceObservationPrefix))
	if observation == nil {
		observation = map[string]string{}
	}
	observation["memory_structure_id"] = structure.ID
	observation["remote_evidence_kind"] = evidence.Kind
	observation["remote_evidence_id"] = evidence.EvidenceID
	observation["remote_evidence_origin_node"] = evidence.OriginNode
	observation["remote_evidence_backing_memory_id"] = evidence.BackingMemoryID
	observation["remote_evidence_backing_revision"] = fmt.Sprint(evidence.BackingRevision)
	observation["remote_evidence_payload_digest"] = evidence.PayloadDigest
	observation["remote_evidence_read_nano"] = fmt.Sprint(evidence.ReadNano)
	observation["remote_evidence_grant_nonce"] = evidence.GrantNonce

	action := mergeStringMap(structure.Action, framePrefixedStringMap(frame.Vars, remoteEvidenceExperienceActionPrefix))
	if action == nil {
		action = map[string]string{}
	}
	action["memory_structure_id"] = structure.ID
	action["remote_evidence_intake_fact_id"] = factID
	outcome := framePrefixedStringMap(frame.Vars, remoteEvidenceExperienceOutcomePrefix)
	predictionError := framePrefixedStringMap(frame.Vars, remoteEvidenceExperiencePredictionErrorPrefix)
	if len(predictionError) == 0 && len(structure.ExpectedOutcome) > 0 {
		predictionError = calculatePredictionError(structure.ExpectedOutcome, outcome)
	}
	predictionHash := strings.TrimSpace(frame.Vars[remoteEvidencePredictionHashVar])
	if predictionHash == "" {
		predictionHash = structure.PredictionHash
	}

	experienceID := remoteEvidenceLocalExperienceID(factID)
	if existing, ok := experiences.Get(experienceID); ok {
		return existing, nil
	}
	return experiences.Record(MemoryExperience{
		ID:              experienceID,
		ParentIDs:       parentIDs,
		Context:         context,
		Observation:     observation,
		Action:          action,
		Outcome:         outcome,
		PredictionHash:  predictionHash,
		PredictionError: predictionError,
	})
}

func executeRemoteEvidenceMemoryDecision(e *Engine, structure *MemoryStructure, evidence *RemoteMemoryEvidence) (*Frame, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("remote evidence intake engine unavailable")
	}
	if structure == nil || structure.State != memoryStructureValidatedState || len(structure.Program) == 0 {
		return nil, errors.New("remote evidence intake Structure is not validated/executable")
	}
	_, executable, err := root.resolveLocalFabricMemory(structure.ID)
	if err != nil {
		return nil, fmt.Errorf("remote evidence intake executable unavailable: %w", err)
	}
	if !equalStringOpProgram(executable.Program, structure.Program) {
		return nil, errors.New("remote evidence intake executable program drift")
	}
	frame, err := remoteEvidenceDecisionFrame(evidence, structure.ID)
	if err != nil {
		return nil, err
	}
	if err := globalTxnScheduler.canonical(root, structure.ID, frame); err != nil {
		return nil, fmt.Errorf("remote evidence Memory decision execution failed: %w", err)
	}
	return frame, nil
}

func persistRemoteEvidenceDecision(
	e *Engine,
	experiences *experienceLedger,
	formation *memoryStructureFormation,
	validation *memoryStructureValidationLedger,
	fact *RemoteEvidenceIntakeFact,
) error {
	root := memoryGrowthRoot(e)
	if root == nil {
		return errors.New("remote evidence intake engine unavailable")
	}
	if experiences != nil && formation != nil && validation != nil {
		if err := PersistMemoryGrowthState(root, experiences, formation, validation); err != nil {
			return err
		}
	}
	memory, err := remoteEvidenceIntakeFactMemory(fact)
	if err != nil {
		return err
	}
	if err := root.upsertExplicitMemoryBounded(memory); err != nil {
		return err
	}
	return root.persistAll()
}
