package main

import (
	"errors"
	"fmt"
	"strings"
)

const memoryStructureSupersededState = "superseded"

// ReconcileValidatedStructureFromExecution 让已验证 Memory Structure 根据自身执行反馈产生新的结构版本。
//
// 这里不直接“猜测”如何修改结构，而是把真实执行产生的 Experience 再送回已有的
// Experience -> Candidate -> Reality Validation 生长链。只有新的事实模式再次出现并
// 通过独立验证后，旧 Structure 才会被标记为 superseded，新 Structure 才成为 validated。
// Program 必须沿用调用方明确提供的旧 Structure Program，不由 Kernel 推断或改写。
func ReconcileValidatedStructureFromExecution(
	experiences *experienceLedger,
	formation *memoryStructureFormation,
	validation *memoryStructureValidationLedger,
	current *MemoryStructure,
	minRecurrence int,
	minSuccess int,
) (*MemoryStructure, error) {
	if experiences == nil {
		return nil, errors.New("experience ledger unavailable")
	}
	if formation == nil {
		return nil, errors.New("memory structure formation unavailable")
	}
	if validation == nil {
		return nil, errors.New("memory structure validation ledger unavailable")
	}
	if current == nil {
		return nil, errors.New("current memory structure unavailable")
	}
	if current.State != memoryStructureValidatedState {
		return nil, errors.New("current memory structure is not validated")
	}
	if strings.TrimSpace(current.ID) == "" {
		return nil, errors.New("current memory structure id unavailable")
	}
	if minRecurrence < 2 {
		return nil, errors.New("minimum recurrence must be at least 2")
	}
	if minSuccess < 1 {
		return nil, errors.New("minimum validation success must be at least 1")
	}

	// 只接收由该 Structure 自己执行产生、且确实记录 PredictionError 的 Experience。
	// Kernel 不判断误差“好坏”；这里只寻找已经发生的事实反馈。
	feedback := make([]*MemoryExperience, 0)
	for _, experience := range experiences.Snapshot() {
		if experience == nil || len(experience.PredictionError) == 0 {
			continue
		}
		if experience.Observation["memory_structure_id"] != current.ID {
			continue
		}
		feedback = append(feedback, experience)
	}
	if len(feedback) < minRecurrence {
		return nil, nil
	}

	// 让全部历史 Experience 重新经过既有的确定性形成逻辑；这不会产生第二套学习账本。
	if _, err := formation.FormCandidates(experiences.Snapshot(), minRecurrence); err != nil {
		return nil, err
	}

	candidates := formation.Snapshot()
	for _, candidate := range candidates {
		if candidate == nil || candidate.PatternHash == current.PatternHash {
			continue
		}
		matching := make([]*MemoryExperience, 0, len(feedback))
		for _, experience := range feedback {
			if memoryStructurePatternHash(*experience) == candidate.PatternHash {
				matching = append(matching, experience)
			}
		}
		if len(matching) < minRecurrence {
			continue
		}

		// 第一条是结构来源，第二条必须是独立 witness；二者共同满足现实验证要求。
		source := matching[0]
		witness := matching[1]
		_, revised, err := validation.ValidateCandidate(candidate, source, witness, minSuccess)
		if err != nil || revised == nil {
			continue
		}

		// 修正后的结构必须保持原结构的执行 Program；否则这不是“根据反馈修正”，
		// 而是无依据地改变执行行为。Program 的复制仍由已有 Entry 013 原语完成。
		if err := BindExecutableProgram(revised, current.Program); err != nil {
			return nil, fmt.Errorf("bind revised memory structure program: %w", err)
		}

		oldCandidateID := candidateIDFromStructure(current)
		validation.mu.Lock()
		if old := validation.structures[oldCandidateID]; old != nil {
			old.State = memoryStructureSupersededState
		}
		delete(validation.validated, oldCandidateID)
		// 同步更新 pending structure，否则后续 memory.mem 快照会丢失新的 Program。
		validation.structures[candidate.ID] = cloneMemoryStructure(revised)
		validation.validated[candidate.ID] = cloneMemoryStructure(revised)
		validation.mu.Unlock()

		return cloneMemoryStructure(revised), nil
	}
	return nil, nil
}
