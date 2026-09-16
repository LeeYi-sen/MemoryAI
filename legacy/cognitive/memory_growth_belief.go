package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	memoryBeliefTag                = "memory-belief"
	memoryBeliefStructureTagPrefix = "memory-belief.structure:"
)

// MemoryBeliefState 是 Memory 对一个具体预测事实保留的动态证据状态。
// 它不保存 Kernel 计算出的“置信度”；支持、冲突、缺失与观测值都来自真实 Experience。
// PredictionKey 是动态键，因此 Belief 不依赖固定认知向量或固定维度表。
type MemoryBeliefState struct {
	ID                       string            `json:"id"`
	StructureID              string            `json:"structure_id"`
	PredictionKey            string            `json:"prediction_key"`
	PredictedValue           string            `json:"predicted_value"`
	EvidenceExperienceIDs    []string          `json:"evidence_experience_ids,omitempty"`
	SupportingExperienceIDs  []string          `json:"supporting_experience_ids,omitempty"`
	ConflictingExperienceIDs []string          `json:"conflicting_experience_ids,omitempty"`
	MissingExperienceIDs     []string          `json:"missing_experience_ids,omitempty"`
	ObservedValues           map[string]uint64 `json:"observed_values,omitempty"`
	LastExperienceID         string            `json:"last_experience_id,omitempty"`
	LastUpdatedNano          int64             `json:"last_updated_nano,omitempty"`
}

func memoryBeliefID(structureID, predictionKey, predictedValue string) string {
	payload := strings.TrimSpace(structureID) + "\x00" + strings.TrimSpace(predictionKey) + "\x00" + predictedValue
	sum := sha256.Sum256([]byte(payload))
	return "memory-belief-" + hex.EncodeToString(sum[:16])
}

func cloneObservedValues(src map[string]uint64) map[string]uint64 {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]uint64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneMemoryBeliefState(src *MemoryBeliefState) *MemoryBeliefState {
	if src == nil {
		return nil
	}
	out := *src
	out.EvidenceExperienceIDs = cloneStrings(src.EvidenceExperienceIDs)
	out.SupportingExperienceIDs = cloneStrings(src.SupportingExperienceIDs)
	out.ConflictingExperienceIDs = cloneStrings(src.ConflictingExperienceIDs)
	out.MissingExperienceIDs = cloneStrings(src.MissingExperienceIDs)
	out.ObservedValues = cloneObservedValues(src.ObservedValues)
	return &out
}

func encodeMemoryBeliefState(state *MemoryBeliefState) (string, error) {
	if state == nil {
		return "", errors.New("memory belief state unavailable")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeMemoryBeliefState(raw any) (*MemoryBeliefState, error) {
	var payload []byte
	switch v := raw.(type) {
	case string:
		payload = []byte(v)
	case []byte:
		payload = append([]byte(nil), v...)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}
	var state MemoryBeliefState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func memoryBeliefMemory(state *MemoryBeliefState, revision uint64) (*Memory, error) {
	encoded, err := encodeMemoryBeliefState(state)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:       state.ID,
		Layer:    "emergent",
		Tags:     []string{"memory", memoryBeliefTag, memoryBeliefStructureTagPrefix + state.StructureID},
		Content:  "Memory-native dynamic belief evidence state.",
		Revision: revision,
		State: map[string]any{
			"structure_id":    state.StructureID,
			"prediction_key":  state.PredictionKey,
			"predicted_value": state.PredictedValue,
			"belief_state":    encoded,
		},
	}, nil
}

func loadMemoryBeliefState(e *Engine, beliefID string) (*MemoryBeliefState, uint64, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, 0, errors.New("memory belief engine unavailable")
	}
	_, m, err := root.resolveLocalFabricMemory(strings.TrimSpace(beliefID))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	if m == nil || !memoryHasTag(m, memoryBeliefTag) {
		return nil, 0, fmt.Errorf("Memory id %s collides with non-belief Memory", beliefID)
	}
	state, err := decodeMemoryBeliefState(m.State["belief_state"])
	if err != nil {
		return nil, 0, err
	}
	return state, m.Revision, nil
}

// UpdateMemoryBeliefsFromExperience 把一次真实 Experience 投影到 Structure 自己声明的预测键。
// 这里只累积事实证据，不计算 confidence、reward 或任何语义权重。
func UpdateMemoryBeliefsFromExperience(e *Engine, structure *MemoryStructure, experience *MemoryExperience) ([]*MemoryBeliefState, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("memory belief engine unavailable")
	}
	if structure == nil || strings.TrimSpace(structure.ID) == "" {
		return nil, errors.New("memory belief structure unavailable")
	}
	if experience == nil || strings.TrimSpace(experience.ID) == "" {
		return nil, errors.New("memory belief experience unavailable")
	}
	if len(structure.ExpectedOutcome) == 0 {
		return nil, nil
	}
	if observedStructureID := strings.TrimSpace(experience.Observation["memory_structure_id"]); observedStructureID != "" && observedStructureID != structure.ID {
		return nil, fmt.Errorf("belief evidence structure mismatch: experience=%s structure=%s", observedStructureID, structure.ID)
	}

	keys := make([]string, 0, len(structure.ExpectedOutcome))
	for key := range structure.ExpectedOutcome {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	updated := make([]*MemoryBeliefState, 0, len(keys))
	for _, key := range keys {
		predicted := structure.ExpectedOutcome[key]
		beliefID := memoryBeliefID(structure.ID, key, predicted)
		state, revision, err := loadMemoryBeliefState(root, beliefID)
		if err != nil {
			return nil, err
		}
		if state == nil {
			state = &MemoryBeliefState{
				ID:             beliefID,
				StructureID:    structure.ID,
				PredictionKey:  key,
				PredictedValue: predicted,
				ObservedValues: map[string]uint64{},
			}
		} else if state.StructureID != structure.ID || state.PredictionKey != key || state.PredictedValue != predicted {
			return nil, fmt.Errorf("belief identity drift detected: %s", beliefID)
		}
		if containsString(state.EvidenceExperienceIDs, experience.ID) {
			updated = append(updated, cloneMemoryBeliefState(state))
			continue
		}

		state.EvidenceExperienceIDs = mergeSortedUnique(state.EvidenceExperienceIDs, []string{experience.ID})
		actual, present := experience.Outcome[key]
		if !present {
			state.MissingExperienceIDs = mergeSortedUnique(state.MissingExperienceIDs, []string{experience.ID})
		} else {
			if state.ObservedValues == nil {
				state.ObservedValues = map[string]uint64{}
			}
			state.ObservedValues[actual]++
			if actual == predicted {
				state.SupportingExperienceIDs = mergeSortedUnique(state.SupportingExperienceIDs, []string{experience.ID})
			} else {
				state.ConflictingExperienceIDs = mergeSortedUnique(state.ConflictingExperienceIDs, []string{experience.ID})
			}
		}
		state.LastExperienceID = experience.ID
		state.LastUpdatedNano = experience.CreatedNano

		memory, err := memoryBeliefMemory(state, revision+1)
		if err != nil {
			return nil, err
		}
		if err := root.upsertExplicitMemoryBounded(memory); err != nil {
			return nil, err
		}
		updated = append(updated, cloneMemoryBeliefState(state))
	}
	return updated, nil
}

// MemoryBeliefsForStructure 返回一个 Structure 的全部动态 Belief Memory，稳定按 ID 排序。
func MemoryBeliefsForStructure(e *Engine, structureID string) ([]*MemoryBeliefState, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("memory belief engine unavailable")
	}
	ids, err := root.listTagFabric(memoryBeliefStructureTagPrefix + strings.TrimSpace(structureID))
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	out := make([]*MemoryBeliefState, 0, len(ids))
	for _, id := range ids {
		state, _, err := loadMemoryBeliefState(root, id)
		if err != nil {
			return nil, err
		}
		if state != nil {
			out = append(out, state)
		}
	}
	return out, nil
}
