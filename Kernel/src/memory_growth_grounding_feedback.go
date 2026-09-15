package main

import (
	"errors"
	"fmt"
	"sort"
)

func selectGroundedAction(e *Engine, validation *memoryStructureValidationLedger) (*MemoryStructure, *groundedActionReceiptState, uint64, bool, error) {
	if e == nil || validation == nil {
		return nil, nil, 0, false, nil
	}
	structures := validation.Snapshot()
	sort.Slice(structures, func(i, j int) bool { return structures[i].ID < structures[j].ID })

	for _, structure := range structures {
		actionID, _, ok := groundedStructureContract(structure)
		if !ok {
			continue
		}
		receipt, revision, err := loadGroundedActionReceipt(e, actionID)
		if err != nil {
			return nil, nil, 0, false, err
		}
		if receipt == nil {
			continue
		}
		if receipt.StructureID != structure.ID {
			// Context branch 会保留父结构的事实 Action，但不能重放父代已经消费的物理 action id。
			// 只有明确血缘能继承“该 action 已执行”的事实；无血缘的重复 action id 仍视为冲突。
			if containsString(structure.ParentStructureIDs, receipt.StructureID) {
				continue
			}
			return nil, nil, 0, false, fmt.Errorf("grounded action id %s is already owned by structure %s", actionID, receipt.StructureID)
		}
		if receipt.Status == groundedReceiptResultPersisted {
			return structure, receipt, revision, true, nil
		}
		if receipt.Status != groundedReceiptPrepared && receipt.Status != groundedReceiptFeedbackCommitted {
			return nil, nil, 0, false, fmt.Errorf("unsupported grounded receipt status: %s", receipt.Status)
		}
	}

	for _, structure := range structures {
		actionID, _, ok := groundedStructureContract(structure)
		if !ok {
			continue
		}
		receipt, _, err := loadGroundedActionReceipt(e, actionID)
		if err != nil {
			return nil, nil, 0, false, err
		}
		if receipt == nil {
			return structure, nil, 0, false, nil
		}
	}
	return nil, nil, 0, false, nil
}

func commitGroundedFeedback(
	e *Engine,
	structure *MemoryStructure,
	receipt *groundedActionReceiptState,
	receiptRevision uint64,
	experiences *experienceLedger,
	formation *memoryStructureFormation,
	validation *memoryStructureValidationLedger,
) (*GroundedActionCycleResult, error) {
	result := &GroundedActionCycleResult{
		StructureID:   structure.ID,
		ActionID:      receipt.ActionID,
		AdapterID:     receipt.AdapterID,
		ReceiptID:     receipt.ReceiptID,
		PhysicalError: receipt.PhysicalError,
	}

	instance, err := groundedTrialInstanceForActionID(e, receipt.ActionID)
	if err != nil {
		return result, err
	}
	if instance != nil {
		if instance.StructureID != structure.ID || instance.AdapterID != receipt.AdapterID {
			return result, errors.New("grounded trial instance does not match receipt/structure")
		}
		result.ActionInstanceID = instance.ID
		result.TrialSeriesID = instance.SeriesID
		result.TrialGeneration = instance.Generation
		result.ParentExperienceID = instance.ParentExperienceID
	}

	experienceID := groundedExperienceID(receipt.ReceiptID)
	experience, exists := experiences.Get(experienceID)
	if !exists {
		actual := captureActualOutcome(receipt.PhysicalResult, structure.ExpectedOutcome)
		predictionError := calculatePredictionError(structure.ExpectedOutcome, actual)
		observation := cloneStringMap(structure.Observation)
		if observation == nil {
			observation = map[string]string{}
		}
		observation["memory_structure_id"] = structure.ID
		observation["memory_structure_pattern"] = structure.PatternHash
		observation[groundedAdapterActionKey] = receipt.AdapterID
		observation[groundedActionIDKey] = receipt.ActionID
		observation["grounded_receipt_id"] = receipt.ReceiptID
		if receipt.PhysicalError != "" {
			observation["grounded_physical_error"] = receipt.PhysicalError
		}
		action := cloneStringMap(structure.Action)
		if action == nil {
			action = map[string]string{}
		}
		action["memory_structure_id"] = structure.ID

		parentIDs := []string(nil)
		context := cloneStringMap(structure.Context)
		if instance != nil {
			if _, ok := experiences.Get(instance.ParentExperienceID); !ok {
				return result, fmt.Errorf("grounded trial parent experience not found: %s", instance.ParentExperienceID)
			}
			parentIDs = []string{instance.ParentExperienceID}
			observation[groundedTrialInstanceActionKey] = instance.ID
			observation[groundedTrialSeriesActionKey] = instance.SeriesID
			observation[groundedTrialGenerationActionKey] = fmt.Sprint(instance.Generation)
			observation["grounded_trial_parent_experience_id"] = instance.ParentExperienceID
			action[groundedTrialInstanceActionKey] = instance.ID
			action[groundedTrialSeriesActionKey] = instance.SeriesID
			action[groundedTrialGenerationActionKey] = fmt.Sprint(instance.Generation)
			action[groundedTrialParentActionKey] = instance.ParentActionID
			context = cloneStringMap(instance.Context)
		} else if parentID := firstExistingExperienceID(experiences, structure.SourceExperienceIDs); parentID != "" {
			parentIDs = []string{parentID}
		}
		var recordErr error
		experience, recordErr = experiences.Record(MemoryExperience{
			ID:              experienceID,
			ParentIDs:       parentIDs,
			Context:         context,
			Observation:     observation,
			Action:          action,
			Outcome:         actual,
			PredictionHash:  structure.PredictionHash,
			PredictionError: predictionError,
		})
		if recordErr != nil {
			return result, recordErr
		}
	}

	beliefs, err := UpdateMemoryBeliefsFromExperience(e, structure, experience)
	if err != nil {
		return result, err
	}
	beliefIDs := make([]string, 0, len(beliefs))
	for _, belief := range beliefs {
		if belief != nil {
			beliefIDs = append(beliefIDs, belief.ID)
		}
	}
	sort.Strings(beliefIDs)

	receipt = cloneGroundedReceipt(receipt)
	receipt.Status = groundedReceiptFeedbackCommitted
	receipt.ExperienceID = experience.ID
	receipt.BeliefIDs = beliefIDs
	receipt.PredictionMatched = len(experience.PredictionError) == 0
	if err := upsertGroundedActionReceipt(e, receipt, receiptRevision+1); err != nil {
		return result, err
	}
	root := memoryGrowthRoot(e)
	if err := PersistMemoryGrowthState(root, experiences, formation, validation); err != nil {
		return result, err
	}
	if err := root.persistAll(); err != nil {
		return result, err
	}

	result.ExperienceID = experience.ID
	result.BeliefIDs = beliefIDs
	result.PredictionMatched = receipt.PredictionMatched
	result.Persisted = true
	return result, nil
}

// RunAutonomousGroundedActionCycle 执行至多一个 Memory 明确授权的真实外部动作。
// grounded_action_id 是 Memory 提供的全局幂等键；receipt 在发出外部动作前先持久化，
// 因而崩溃恢复采用 at-most-once，而不是可能重复副作用的自动重试。
// 若 Structure 另外声明 grounded_trial_series_id，则先物化至多一个由上一条 Experience 血缘派生的
// 新 Action Instance；继续与否来自 Memory 的 series 声明，不由 Kernel 根据结果好坏决定。
