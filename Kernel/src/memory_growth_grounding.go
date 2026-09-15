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
	"sync"
	"time"
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
	StructureID       string   `json:"structure_id,omitempty"`
	ActionID          string   `json:"action_id,omitempty"`
	AdapterID         string   `json:"adapter_id,omitempty"`
	ReceiptID         string   `json:"receipt_id,omitempty"`
	ExperienceID      string   `json:"experience_id,omitempty"`
	BeliefIDs         []string `json:"belief_ids,omitempty"`
	PredictionMatched bool     `json:"prediction_matched,omitempty"`
	RecoveredFeedback bool     `json:"recovered_feedback,omitempty"`
	PhysicalError     string   `json:"physical_error,omitempty"`
	Persisted         bool     `json:"persisted,omitempty"`
	Skipped           bool     `json:"skipped,omitempty"`
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
		if parentID := firstExistingExperienceID(experiences, structure.SourceExperienceIDs); parentID != "" {
			parentIDs = []string{parentID}
		}
		var err error
		experience, err = experiences.Record(MemoryExperience{
			ID:              experienceID,
			ParentIDs:       parentIDs,
			Context:         cloneStringMap(structure.Context),
			Observation:     observation,
			Action:          action,
			Outcome:         actual,
			PredictionHash:  structure.PredictionHash,
			PredictionError: predictionError,
		})
		if err != nil {
			return result, err
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
func RunAutonomousGroundedActionCycle(e *Engine) (*GroundedActionCycleResult, error) {
	result := &GroundedActionCycleResult{}
	root := memoryGrowthRoot(e)
	if root == nil {
		return result, errors.New("engine unavailable")
	}
	if !enterGroundedActionCycle(root) {
		result.Skipped = true
		return result, nil
	}
	defer leaveGroundedActionCycle(root)

	experiences, formation, validation, err := LoadMemoryGrowthState(root)
	if err != nil {
		return result, err
	}
	structure, receipt, receiptRevision, recovering, err := selectGroundedAction(root, validation)
	if err != nil {
		return result, err
	}
	if structure == nil {
		result.Skipped = true
		return result, nil
	}
	actionID, adapterID, ok := groundedStructureContract(structure)
	if !ok {
		result.Skipped = true
		return result, nil
	}
	if recovering {
		result, err = commitGroundedFeedback(root, structure, receipt, receiptRevision, experiences, formation, validation)
		if result != nil {
			result.RecoveredFeedback = true
		}
		return result, err
	}

	receipt = &groundedActionReceiptState{
		ReceiptID:    groundedActionReceiptID(actionID),
		ActionID:     actionID,
		StructureID:  structure.ID,
		AdapterID:    adapterID,
		Status:       groundedReceiptPrepared,
		PreparedNano: time.Now().UnixNano(),
	}
	if err := upsertGroundedActionReceipt(root, receipt, 1); err != nil {
		return result, err
	}
	// 先把 at-most-once fence 物理持久化，再允许真实 I/O 离开进程。
	if err := root.persistAll(); err != nil {
		return result, err
	}

	physicalResult, physicalErr := root.executeSourceAdapter(adapterID, sourceParseTimeout(structure.Action[groundedTimeoutActionKey]))
	if physicalResult == nil {
		physicalResult = map[string]any{"ok": false}
	}
	if physicalErr != nil {
		physicalResult["error"] = physicalErr.Error()
	}
	receipt.PhysicalResult = cloneGroundedPhysicalResult(physicalResult)
	receipt.CompletedNano = time.Now().UnixNano()
	receipt.Status = groundedReceiptResultPersisted
	if physicalErr != nil {
		receipt.PhysicalError = physicalErr.Error()
	}
	if err := upsertGroundedActionReceipt(root, receipt, 2); err != nil {
		return result, err
	}
	// 真实结果先独立持久化；若随后反馈提交崩溃，下一次 live 周期只恢复 Experience/Belief，绝不重复外部动作。
	if err := root.persistAll(); err != nil {
		return result, err
	}

	return commitGroundedFeedback(root, structure, receipt, 2, experiences, formation, validation)
}
