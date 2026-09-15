package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func runRemoteEvidenceIntakeCycle(e *Engine, reader remoteEvidenceReader) (*RemoteEvidenceIntakeResult, error) {
	result := &RemoteEvidenceIntakeResult{}
	root := memoryGrowthRoot(e)
	if root == nil {
		return result, errors.New("engine unavailable")
	}
	if reader == nil {
		return result, errors.New("remote evidence reader unavailable")
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
		kind, evidenceID, ok := remoteEvidenceIntakeContract(structure)
		if !ok {
			continue
		}
		evidence, readErr := reader(kind, evidenceID)
		if readErr != nil {
			readErrors = append(readErrors, structure.ID+": "+readErr.Error())
			continue
		}
		if evidence == nil {
			readErrors = append(readErrors, structure.ID+": remote evidence unavailable")
			continue
		}
		contractDigest := remoteEvidenceContractDigest(structure)
		factID := remoteEvidenceIntakeIdentity(structure.ID, contractDigest, evidence)
		exists, err := remoteEvidenceIntakeFactExists(root, factID)
		if err != nil {
			return result, err
		}
		if exists {
			continue
		}

		frame, err := executeRemoteEvidenceMemoryDecision(root, structure, evidence)
		if err != nil {
			return result, err
		}
		fact := &RemoteEvidenceIntakeFact{
			ID: factID, StructureID: structure.ID, ContractDigest: contractDigest,
			EvidenceKind: evidence.Kind, EvidenceID: evidence.EvidenceID, OriginNode: evidence.OriginNode,
			BackingMemoryID: evidence.BackingMemoryID, BackingRevision: evidence.BackingRevision,
			PayloadDigest: evidence.PayloadDigest, ReadNano: evidence.ReadNano, GrantNonce: evidence.GrantNonce,
			DecisionReason: strings.TrimSpace(frame.Vars[remoteEvidenceReasonVar]), CreatedNano: time.Now().UnixNano(),
		}
		result.StructureID = structure.ID
		result.EvidenceKind = evidence.Kind
		result.EvidenceID = evidence.EvidenceID
		result.OriginNode = evidence.OriginNode
		result.PayloadDigest = evidence.PayloadDigest
		result.IntakeFactID = fact.ID

		if !remoteEvidenceIntakeEnabled(frame.Vars[remoteEvidenceCommitVar]) {
			fact.Decision = remoteEvidenceDecisionObserved
			if err := persistRemoteEvidenceDecision(root, nil, nil, nil, fact); err != nil {
				return result, err
			}
			result.Rejected = true
			result.Persisted = true
			return result, nil
		}

		experience, err := buildRemoteEvidenceLocalExperience(experiences, structure, evidence, frame, fact.ID)
		if err != nil {
			return result, err
		}
		if len(structure.ExpectedOutcome) > 0 {
			if _, err := UpdateMemoryBeliefsFromExperience(root, structure, experience); err != nil {
				return result, fmt.Errorf("update Memory belief from remote evidence intake: %w", err)
			}
		}
		fact.Decision = remoteEvidenceDecisionCommitted
		fact.LocalExperienceID = experience.ID
		if err := persistRemoteEvidenceDecision(root, experiences, formation, validation, fact); err != nil {
			return result, err
		}
		result.LocalExperienceID = experience.ID
		result.Committed = true
		result.Persisted = true
		return result, nil
	}

	result.Skipped = true
	if len(readErrors) > 0 {
		return result, fmt.Errorf("remote evidence unavailable: %s", strings.Join(readErrors, "; "))
	}
	return result, nil
}

// RunAutonomousRemoteEvidenceIntakeCycle lets one validated Memory Structure
// inspect one fresh remote evidence value and decide, through its own Program,
// whether to form a new local Experience. Kernel never persists the remote
// payload automatically and never adjudicates evidence truth.
func RunAutonomousRemoteEvidenceIntakeCycle(e *Engine) (*RemoteEvidenceIntakeResult, error) {
	episode, err := runRemoteEvidenceEpisodeCycle(e, ReadRemoteMemoryEvidence)
	if err != nil {
		return &RemoteEvidenceIntakeResult{Skipped: episode != nil && episode.Skipped}, err
	}
	if episode != nil && !episode.Skipped {
		return &RemoteEvidenceIntakeResult{
			StructureID:       episode.StructureID,
			IntakeFactID:      episode.EpisodeFactID,
			LocalExperienceID: episode.LocalExperienceID,
			Committed:         episode.Committed,
			Rejected:          episode.Rejected,
			Persisted:         episode.Persisted,
		}, nil
	}
	return runRemoteEvidenceIntakeCycle(e, ReadRemoteMemoryEvidence)
}
