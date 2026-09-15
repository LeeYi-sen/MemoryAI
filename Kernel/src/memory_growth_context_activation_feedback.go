package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func cleanLiveContext(vars map[string]string) map[string]string {
	if len(vars) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range vars {
		if strings.HasPrefix(key, "__") {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RecordContextualExecutionFeedback 把已经由原有 scheduler/VM 执行完成的 Context leaf 结果写回 Experience + Belief。
// 它不重复执行 Program，也不评价结果，只保存预测/现实差异和调用时真实 Context。
func RecordContextualExecutionFeedback(e *Engine, structure *MemoryStructure, liveContext map[string]string, frame *Frame, activationFactID string) (*MemoryStructureExecutionFeedback, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("context execution feedback engine unavailable")
	}
	if structure == nil || structure.State != memoryStructureValidatedState || len(structure.ExpectedOutcome) == 0 {
		return nil, errors.New("validated contextual structure with predicted outcome required")
	}
	if frame == nil {
		return nil, errors.New("context execution feedback frame unavailable")
	}
	experiences, formation, validation, err := LoadMemoryGrowthState(root)
	if err != nil {
		return nil, err
	}
	current := findValidatedStructureByID(validation, structure.ID)
	if current != nil {
		structure = current
	}
	predicted := capturePredictedOutcome(structure)
	actual := captureActualOutcome(frame.Vars, predicted)
	predictionError := calculatePredictionError(predicted, actual)
	observation := cloneStringMap(structure.Observation)
	if observation == nil {
		observation = map[string]string{}
	}
	observation["memory_structure_id"] = structure.ID
	observation["memory_structure_pattern"] = structure.PatternHash
	if strings.TrimSpace(activationFactID) != "" {
		observation["memory_context_activation_fact"] = activationFactID
	}
	action := cloneStringMap(structure.Action)
	if action == nil {
		action = map[string]string{}
	}
	action["memory_structure_id"] = structure.ID
	parentIDs := []string(nil)
	if parentID := firstExistingExperienceID(experiences, structure.SourceExperienceIDs); parentID != "" {
		parentIDs = []string{parentID}
	}
	experience, err := experiences.Record(MemoryExperience{
		ParentIDs:       parentIDs,
		Context:         cleanLiveContext(liveContext),
		Observation:     observation,
		Action:          action,
		Outcome:         actual,
		PredictionHash:  structure.PredictionHash,
		PredictionError: predictionError,
	})
	if err != nil {
		return nil, err
	}
	if _, err := UpdateMemoryBeliefsFromExperience(root, structure, experience); err != nil {
		return nil, err
	}
	if err := PersistMemoryGrowthState(root, experiences, formation, validation); err != nil {
		return nil, err
	}
	if err := root.persistAll(); err != nil {
		return nil, err
	}
	return &MemoryStructureExecutionFeedback{
		StructureID:      structure.ID,
		PredictedOutcome: predicted,
		ActualOutcome:    actual,
		PredictionError:  predictionError,
		Experience:       experience,
	}, nil
}

// loadMemoryContextActivationFact is used by restart tests and diagnostics.
func loadMemoryContextActivationFact(e *Engine, id string) (*MemoryContextActivationFact, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("context activation engine unavailable")
	}
	_, memory, err := root.resolveLocalFabricMemory(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if memory == nil || !memoryHasTag(memory, memoryContextActivationFactTag) {
		return nil, fmt.Errorf("context activation fact not found: %s", id)
	}
	raw, ok := memory.State["activation_fact"]
	if !ok {
		return nil, errors.New("context activation fact payload missing")
	}
	var payload []byte
	switch value := raw.(type) {
	case string:
		payload = []byte(value)
	case []byte:
		payload = append([]byte(nil), value...)
	default:
		payload, err = json.Marshal(value)
		if err != nil {
			return nil, err
		}
	}
	var fact MemoryContextActivationFact
	if err := json.Unmarshal(payload, &fact); err != nil {
		return nil, err
	}
	return cloneContextActivationFact(&fact), nil
}

func contextualExecutableCondition(e *Engine, structureID, key string) (string, bool, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return "", false, errors.New("context activation engine unavailable")
	}
	_, memory, err := root.resolveLocalFabricMemory(structureID)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", false, nil
		}
		return "", false, err
	}
	value, ok := memory.State[memoryContextConditionStatePrefix+key]
	if !ok {
		return "", false, nil
	}
	return fmt.Sprint(value), true, nil
}
