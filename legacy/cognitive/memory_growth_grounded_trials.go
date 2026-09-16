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
	groundedTrialSeriesActionKey       = "grounded_trial_series_id"
	groundedTrialTemplateActionKey     = "grounded_trial_template_action_id"
	groundedTrialInstanceActionKey     = "grounded_trial_instance_id"
	groundedTrialGenerationActionKey   = "grounded_trial_generation"
	groundedTrialParentActionKey       = "grounded_trial_parent_action_id"
	groundedActionInstanceTag          = "memory-grounded-action-instance"
	groundedActionInstanceStructureTag = "memory-grounded-action-instance.structure:"
	groundedActionInstanceSeriesTag    = "memory-grounded-action-instance.series:"
	groundedTrialActionPrefix          = "grounded-trial-action-"
	groundedTrialInstancePrefix        = "grounded-action-instance-"
)

// GroundedActionInstanceState 是 Memory Structure 明确授权的一次真实动作实例。
// Kernel 只根据已经存在的 Structure/Experience 血缘生成不可重复物理身份；
// 动作语义、adapter 与是否允许 autonomous 仍完全来自 Memory Structure。
type GroundedActionInstanceState struct {
	ID                 string            `json:"id"`
	ActionID           string            `json:"action_id"`
	SeriesID           string            `json:"series_id"`
	StructureID        string            `json:"structure_id"`
	AdapterID          string            `json:"adapter_id"`
	TemplateActionID   string            `json:"template_action_id"`
	Generation         uint64            `json:"generation"`
	ParentInstanceID   string            `json:"parent_instance_id,omitempty"`
	ParentActionID     string            `json:"parent_action_id,omitempty"`
	ParentExperienceID string            `json:"parent_experience_id"`
	Context            map[string]string `json:"context,omitempty"`
	CreatedNano        int64             `json:"created_nano"`
}

// GroundedTrialAuthoringResult 报告一次 Memory-authored Action Instance 物化。
type GroundedTrialAuthoringResult struct {
	StructureID        string `json:"structure_id,omitempty"`
	SeriesID           string `json:"series_id,omitempty"`
	ActionInstanceID   string `json:"action_instance_id,omitempty"`
	ActionID           string `json:"action_id,omitempty"`
	ParentExperienceID string `json:"parent_experience_id,omitempty"`
	Generation         uint64 `json:"generation,omitempty"`
	Persisted          bool   `json:"persisted,omitempty"`
	Skipped            bool   `json:"skipped,omitempty"`
}

func groundedTrialDigest(seriesID, structureID, templateActionID, parentExperienceID string) string {
	payload := strings.Join([]string{
		strings.TrimSpace(seriesID),
		strings.TrimSpace(structureID),
		strings.TrimSpace(templateActionID),
		strings.TrimSpace(parentExperienceID),
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:16])
}

func groundedTrialInstanceIdentity(seriesID, structureID, templateActionID, parentExperienceID string) (instanceID, actionID string) {
	digest := groundedTrialDigest(seriesID, structureID, templateActionID, parentExperienceID)
	return groundedTrialInstancePrefix + digest, groundedTrialActionPrefix + digest
}

func cloneGroundedActionInstance(src *GroundedActionInstanceState) *GroundedActionInstanceState {
	if src == nil {
		return nil
	}
	out := *src
	out.Context = cloneStringMap(src.Context)
	return &out
}

func groundedActionInstanceMemory(state *GroundedActionInstanceState) (*Memory, error) {
	if state == nil || strings.TrimSpace(state.ID) == "" || strings.TrimSpace(state.ActionID) == "" {
		return nil, errors.New("grounded action instance unavailable")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:    state.ID,
		Layer: "emergent",
		Tags: []string{
			"memory",
			groundedActionInstanceTag,
			groundedActionInstanceStructureTag + state.StructureID,
			groundedActionInstanceSeriesTag + state.SeriesID,
		},
		Content:  "Memory-authored grounded action instance lineage.",
		Revision: 1,
		State: map[string]any{
			"action_id":            state.ActionID,
			"series_id":            state.SeriesID,
			"structure_id":         state.StructureID,
			"adapter_id":           state.AdapterID,
			"template_action_id":   state.TemplateActionID,
			"generation":           state.Generation,
			"parent_instance_id":   state.ParentInstanceID,
			"parent_action_id":     state.ParentActionID,
			"parent_experience_id": state.ParentExperienceID,
			"instance_state":       string(raw),
		},
	}, nil
}

func decodeGroundedActionInstance(raw any) (*GroundedActionInstanceState, error) {
	var payload []byte
	switch value := raw.(type) {
	case string:
		payload = []byte(value)
	case []byte:
		payload = append([]byte(nil), value...)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}
	var state GroundedActionInstanceState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func loadGroundedActionInstance(e *Engine, instanceID string) (*GroundedActionInstanceState, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("grounded trial engine unavailable")
	}
	_, memory, err := root.resolveLocalFabricMemory(strings.TrimSpace(instanceID))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	if memory == nil || !memoryHasTag(memory, groundedActionInstanceTag) {
		return nil, fmt.Errorf("grounded action instance id collides with non-instance Memory: %s", instanceID)
	}
	state, err := decodeGroundedActionInstance(memory.State["instance_state"])
	if err != nil {
		return nil, err
	}
	if state.ID != strings.TrimSpace(instanceID) {
		return nil, fmt.Errorf("grounded action instance identity drift: %s", instanceID)
	}
	return state, nil
}

func groundedTrialInstanceForActionID(e *Engine, actionID string) (*GroundedActionInstanceState, error) {
	actionID = strings.TrimSpace(actionID)
	if !strings.HasPrefix(actionID, groundedTrialActionPrefix) {
		return nil, nil
	}
	digest := strings.TrimPrefix(actionID, groundedTrialActionPrefix)
	if len(digest) != 32 {
		return nil, fmt.Errorf("invalid grounded trial action identity: %s", actionID)
	}
	state, err := loadGroundedActionInstance(e, groundedTrialInstancePrefix+digest)
	if err != nil || state == nil {
		return state, err
	}
	if state.ActionID != actionID {
		return nil, fmt.Errorf("grounded action instance/action identity mismatch: %s", actionID)
	}
	return state, nil
}

func listGroundedActionInstancesForStructure(e *Engine, structureID string) ([]*GroundedActionInstanceState, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("grounded trial engine unavailable")
	}
	ids, err := root.listTagFabric(groundedActionInstanceStructureTag + strings.TrimSpace(structureID))
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	out := make([]*GroundedActionInstanceState, 0, len(ids))
	for _, id := range ids {
		state, err := loadGroundedActionInstance(root, id)
		if err != nil {
			return nil, err
		}
		if state != nil {
			out = append(out, state)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Generation == out[j].Generation {
			return out[i].ID < out[j].ID
		}
		return out[i].Generation < out[j].Generation
	})
	return out, nil
}
