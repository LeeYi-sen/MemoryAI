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
	groundedAdapterActionKey    = "grounded_adapter_id"
	groundedActionIDKey         = "grounded_action_id"
	groundedAutonomousActionKey = "grounded_autonomous"
	groundedTimeoutActionKey    = "grounded_timeout"

	groundedActionReceiptTag = "memory-grounded-action-receipt"

	groundedReceiptPrepared          = "prepared"
	groundedReceiptResultPersisted   = "result_persisted"
	groundedReceiptFeedbackCommitted = "feedback_committed"
)

// GroundedActionCycleResult 报告一次 Memory 授权的真实外部动作闭环。
// Action 是否执行来自 Structure 自己携带的精确物理授权字段，而不是 Kernel 目标/奖励策略。
type GroundedActionCycleResult struct {
	StructureID        string   `json:"structure_id,omitempty"`
	ActionID           string   `json:"action_id,omitempty"`
	AdapterID          string   `json:"adapter_id,omitempty"`
	ReceiptID          string   `json:"receipt_id,omitempty"`
	ActionInstanceID   string   `json:"action_instance_id,omitempty"`
	TrialSeriesID      string   `json:"trial_series_id,omitempty"`
	TrialGeneration    uint64   `json:"trial_generation,omitempty"`
	ParentExperienceID string   `json:"parent_experience_id,omitempty"`
	ExperienceID       string   `json:"experience_id,omitempty"`
	BeliefIDs          []string `json:"belief_ids,omitempty"`
	PredictionMatched  bool     `json:"prediction_matched,omitempty"`
	RecoveredFeedback  bool     `json:"recovered_feedback,omitempty"`
	PhysicalError      string   `json:"physical_error,omitempty"`
	Persisted          bool     `json:"persisted,omitempty"`
	Skipped            bool     `json:"skipped,omitempty"`
}

// groundedActionCycleActive 只防止并发 live 请求重复穿透同一个 at-most-once fence；不保存认知状态。
var groundedActionCycleActive sync.Map

func enterGroundedActionCycle(e *Engine) bool {
	root := memoryGrowthRoot(e)
	if root == nil {
		return false
	}
	_, loaded := groundedActionCycleActive.LoadOrStore(root, struct{}{})
	return !loaded
}

func leaveGroundedActionCycle(e *Engine) {
	if root := memoryGrowthRoot(e); root != nil {
		groundedActionCycleActive.Delete(root)
	}
}

type groundedActionReceiptState struct {
	ReceiptID         string         `json:"receipt_id"`
	ActionID          string         `json:"action_id"`
	StructureID       string         `json:"structure_id"`
	AdapterID         string         `json:"adapter_id"`
	Status            string         `json:"status"`
	PreparedNano      int64          `json:"prepared_nano"`
	CompletedNano     int64          `json:"completed_nano,omitempty"`
	PhysicalResult    map[string]any `json:"physical_result,omitempty"`
	PhysicalError     string         `json:"physical_error,omitempty"`
	ExperienceID      string         `json:"experience_id,omitempty"`
	BeliefIDs         []string       `json:"belief_ids,omitempty"`
	PredictionMatched bool           `json:"prediction_matched,omitempty"`
}

func groundedActionReceiptID(actionID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(actionID)))
	return "grounded-action-receipt-" + hex.EncodeToString(sum[:16])
}

func groundedExperienceID(receiptID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(receiptID)))
	return "grounded-experience-" + hex.EncodeToString(sum[:16])
}

func groundedAutonomousEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func groundedStructureContract(structure *MemoryStructure) (actionID, adapterID string, ok bool) {
	if structure == nil || structure.State != memoryStructureValidatedState || len(structure.ExpectedOutcome) == 0 {
		return "", "", false
	}
	if !groundedAutonomousEnabled(structure.Action[groundedAutonomousActionKey]) {
		return "", "", false
	}
	actionID = strings.TrimSpace(structure.Action[groundedActionIDKey])
	adapterID = strings.TrimSpace(structure.Action[groundedAdapterActionKey])
	return actionID, adapterID, actionID != "" && adapterID != ""
}

func cloneGroundedPhysicalResult(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneGroundedReceipt(src *groundedActionReceiptState) *groundedActionReceiptState {
	if src == nil {
		return nil
	}
	out := *src
	out.PhysicalResult = cloneGroundedPhysicalResult(src.PhysicalResult)
	out.BeliefIDs = cloneStrings(src.BeliefIDs)
	return &out
}

func groundedReceiptMemory(state *groundedActionReceiptState, revision uint64) (*Memory, error) {
	if state == nil || strings.TrimSpace(state.ReceiptID) == "" {
		return nil, errors.New("grounded action receipt unavailable")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:       state.ReceiptID,
		Layer:    "emergent",
		Tags:     []string{"memory", groundedActionReceiptTag},
		Content:  "Durable at-most-once grounded action receipt.",
		Revision: revision,
		State: map[string]any{
			"action_id":     state.ActionID,
			"structure_id":  state.StructureID,
			"adapter_id":    state.AdapterID,
			"status":        state.Status,
			"receipt_state": string(raw),
		},
	}, nil
}

func decodeGroundedReceipt(raw any) (*groundedActionReceiptState, error) {
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
	var state groundedActionReceiptState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func loadGroundedActionReceipt(e *Engine, actionID string) (*groundedActionReceiptState, uint64, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, 0, errors.New("grounded action engine unavailable")
	}
	receiptID := groundedActionReceiptID(actionID)
	_, m, err := root.resolveLocalFabricMemory(receiptID)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	if m == nil || !memoryHasTag(m, groundedActionReceiptTag) {
		return nil, 0, fmt.Errorf("grounded receipt id collides with non-receipt Memory: %s", receiptID)
	}
	state, err := decodeGroundedReceipt(m.State["receipt_state"])
	if err != nil {
		return nil, 0, err
	}
	if state.ActionID != strings.TrimSpace(actionID) || state.ReceiptID != receiptID {
		return nil, 0, fmt.Errorf("grounded receipt identity drift: %s", receiptID)
	}
	return state, m.Revision, nil
}

func upsertGroundedActionReceipt(e *Engine, state *groundedActionReceiptState, revision uint64) error {
	root := memoryGrowthRoot(e)
	if root == nil {
		return errors.New("grounded action engine unavailable")
	}
	m, err := groundedReceiptMemory(state, revision)
	if err != nil {
		return err
	}
	return root.upsertExplicitMemoryBounded(m)
}

// selectGroundedAction 优先恢复“外部结果已经持久化、但反馈尚未写回 Experience”的动作；
// 其次才选择新的、拥有 Memory 明确 autonomous 授权且没有 receipt 的动作。
// prepared receipt 代表崩溃窗口中的不确定外部副作用，Kernel 采取 at-most-once：绝不自动重放。
