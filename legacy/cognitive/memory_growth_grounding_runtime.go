package main

import (
	"errors"
	"time"
)

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

	if _, err := RunGroundedTrialAuthoringCycle(root); err != nil {
		return result, err
	}
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
