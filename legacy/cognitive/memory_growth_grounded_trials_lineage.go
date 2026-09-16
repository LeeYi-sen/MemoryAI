package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func groundedTrialTemplateActionID(structure *MemoryStructure) string {
	if structure == nil || structure.Action == nil {
		return ""
	}
	if template := strings.TrimSpace(structure.Action[groundedTrialTemplateActionKey]); template != "" {
		return template
	}
	return strings.TrimSpace(structure.Action[groundedActionIDKey])
}

func currentGroundedTrialInstance(e *Engine, structure *MemoryStructure) (*GroundedActionInstanceState, error) {
	if structure == nil || structure.Action == nil {
		return nil, nil
	}
	seriesID := strings.TrimSpace(structure.Action[groundedTrialSeriesActionKey])
	if seriesID == "" {
		return nil, nil
	}
	templateActionID := groundedTrialTemplateActionID(structure)
	adapterID := strings.TrimSpace(structure.Action[groundedAdapterActionKey])
	instances, err := listGroundedActionInstancesForStructure(e, structure.ID)
	if err != nil {
		return nil, err
	}
	var current *GroundedActionInstanceState
	for _, instance := range instances {
		if instance.SeriesID != seriesID {
			continue
		}
		if instance.TemplateActionID != templateActionID || instance.AdapterID != adapterID {
			return nil, fmt.Errorf("grounded trial contract drift for structure %s; change series id to start a new trial lineage", structure.ID)
		}
		if current != nil && current.Generation == instance.Generation && current.ID != instance.ID {
			return nil, fmt.Errorf("grounded trial lineage fork at generation %d for structure %s", instance.Generation, structure.ID)
		}
		if current == nil || instance.Generation > current.Generation {
			current = instance
		}
	}
	return cloneGroundedActionInstance(current), nil
}

func upsertGroundedTrialStructure(validation *memoryStructureValidationLedger, structure *MemoryStructure) {
	if validation == nil || structure == nil {
		return
	}
	candidateID := candidateIDFromStructure(structure)
	validation.mu.Lock()
	validation.structures[candidateID] = cloneMemoryStructure(structure)
	validation.validated[candidateID] = cloneMemoryStructure(structure)
	validation.mu.Unlock()
}

func firstGroundedTrialParentExperience(experiences *experienceLedger, structure *MemoryStructure) string {
	if experiences == nil || structure == nil {
		return ""
	}
	return firstExistingExperienceID(experiences, structure.SourceExperienceIDs)
}

func authorizeNextGroundedTrialInstance(
	e *Engine,
	experiences *experienceLedger,
	validation *memoryStructureValidationLedger,
	structure *MemoryStructure,
) (*GroundedActionInstanceState, *MemoryStructure, bool, error) {
	if e == nil || experiences == nil || validation == nil || structure == nil || structure.Action == nil {
		return nil, structure, false, nil
	}
	seriesID := strings.TrimSpace(structure.Action[groundedTrialSeriesActionKey])
	if seriesID == "" {
		return nil, structure, false, nil
	}
	_, adapterID, ok := groundedStructureContract(structure)
	if !ok {
		return nil, structure, false, nil
	}
	templateActionID := groundedTrialTemplateActionID(structure)
	if templateActionID == "" {
		return nil, structure, false, errors.New("grounded trial template action id unavailable")
	}

	current, err := currentGroundedTrialInstance(e, structure)
	if err != nil {
		return nil, structure, false, err
	}
	parentExperienceID := ""
	parentInstanceID := ""
	parentActionID := templateActionID
	generation := uint64(1)
	if current == nil {
		parentExperienceID = firstGroundedTrialParentExperience(experiences, structure)
		if parentExperienceID == "" {
			return nil, structure, false, fmt.Errorf("grounded trial source experience unavailable for structure %s", structure.ID)
		}
	} else {
		currentActionID := strings.TrimSpace(structure.Action[groundedActionIDKey])
		if currentActionID != current.ActionID {
			return nil, structure, false, fmt.Errorf("grounded trial current action identity drift for structure %s", structure.ID)
		}
		receipt, _, err := loadGroundedActionReceipt(e, current.ActionID)
		if err != nil {
			return nil, structure, false, err
		}
		if receipt == nil || receipt.Status == groundedReceiptPrepared || receipt.Status == groundedReceiptResultPersisted {
			return current, structure, false, nil
		}
		if receipt.Status != groundedReceiptFeedbackCommitted {
			return nil, structure, false, fmt.Errorf("unsupported grounded trial receipt status: %s", receipt.Status)
		}
		if strings.TrimSpace(receipt.ExperienceID) == "" {
			return nil, structure, false, errors.New("grounded trial committed receipt has no experience lineage")
		}
		if _, ok := experiences.Get(receipt.ExperienceID); !ok {
			return nil, structure, false, fmt.Errorf("grounded trial parent experience not found: %s", receipt.ExperienceID)
		}
		parentExperienceID = receipt.ExperienceID
		parentInstanceID = current.ID
		parentActionID = current.ActionID
		generation = current.Generation + 1
	}

	instanceID, actionID := groundedTrialInstanceIdentity(seriesID, structure.ID, templateActionID, parentExperienceID)
	if existing, err := loadGroundedActionInstance(e, instanceID); err != nil {
		return nil, structure, false, err
	} else if existing != nil {
		if existing.ActionID != actionID || existing.ParentExperienceID != parentExperienceID || existing.Generation != generation {
			return nil, structure, false, fmt.Errorf("grounded trial instance identity collision: %s", instanceID)
		}
		return existing, structure, false, nil
	}

	instance := &GroundedActionInstanceState{
		ID:                 instanceID,
		ActionID:           actionID,
		SeriesID:           seriesID,
		StructureID:        structure.ID,
		AdapterID:          adapterID,
		TemplateActionID:   templateActionID,
		Generation:         generation,
		ParentInstanceID:   parentInstanceID,
		ParentActionID:     parentActionID,
		ParentExperienceID: parentExperienceID,
		Context:            cloneStringMap(structure.Context),
		CreatedNano:        time.Now().UnixNano(),
	}
	memory, err := groundedActionInstanceMemory(instance)
	if err != nil {
		return nil, structure, false, err
	}
	root := memoryGrowthRoot(e)
	if err := root.upsertExplicitMemoryBounded(memory); err != nil {
		return nil, structure, false, err
	}

	updated := cloneMemoryStructure(structure)
	if updated.Action == nil {
		updated.Action = map[string]string{}
	}
	updated.Action[groundedTrialTemplateActionKey] = templateActionID
	updated.Action[groundedTrialInstanceActionKey] = instance.ID
	updated.Action[groundedTrialGenerationActionKey] = strconv.FormatUint(instance.Generation, 10)
	updated.Action[groundedTrialParentActionKey] = parentActionID
	updated.Action[groundedActionIDKey] = actionID
	upsertGroundedTrialStructure(validation, updated)
	return instance, updated, true, nil
}

// RunGroundedTrialAuthoringCycle 至多物化一个新的 Memory-authored Action Instance。
// 是否形成连续试验只取决于 Structure 自身是否声明 grounded_trial_series_id；Kernel 不按成功/失败
// 决定“要不要继续”，也没有固定重试次数。每个新实例只能建立在上一实例已经形成 Experience 之后。
