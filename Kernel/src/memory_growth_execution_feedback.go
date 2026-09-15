package main

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// MemoryStructureExecutionFeedback 是一次已验证结构真正进入 VM 后的现实反馈。
// 它不做评分或目标判断，只把预测与现实重新写回 Memory 的 Experience。
type MemoryStructureExecutionFeedback struct {
	StructureID      string
	PredictedOutcome map[string]string
	ActualOutcome    map[string]string
	PredictionError  map[string]string
	Experience       *MemoryExperience
}

// capturePredictedOutcome 返回结构自己声明的预测事实。
// 预测字段由 Memory Structure 持有，Kernel 不推断预测内容。
func capturePredictedOutcome(structure *MemoryStructure) map[string]string {
	if structure == nil {
		return nil
	}
	return cloneStringMap(structure.ExpectedOutcome)
}

// captureActualOutcome 只读取预测中声明的键，因此不会把 VM 内部所有临时变量误当成“结果”。
func captureActualOutcome(vars map[string]any, predicted map[string]string) map[string]string {
	if len(predicted) == 0 {
		return nil
	}
	out := make(map[string]string, len(predicted))
	for key := range predicted {
		value, ok := vars[key]
		if !ok {
			continue
		}
		out[key] = fmt.Sprint(value)
	}
	return out
}

// calculatePredictionError 只记录预测值与实际值不同的事实。
// missing 使用固定物理标记，避免把“没有产生结果”误认为成功。
func calculatePredictionError(predicted, actual map[string]string) map[string]string {
	if len(predicted) == 0 {
		return nil
	}
	keys := make([]string, 0, len(predicted))
	for key := range predicted {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string)
	for _, key := range keys {
		expected := predicted[key]
		got, ok := actual[key]
		if !ok {
			out[key] = "expected=" + expected + ";actual=<missing>"
			continue
		}
		if got != expected {
			out[key] = "expected=" + expected + ";actual=" + got
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func equalStringOpProgram(a, b []Op) bool {
	return reflect.DeepEqual(a, b)
}

// ExecuteMemoryStructure 使用现有 Canonical VM 执行已经落入 Memory Fabric 的 Structure。
// 调用方必须先把 Entry 013 的 ExecutableMemory 写入 Fabric；这里绝不新建第二套执行器。
func ExecuteMemoryStructure(e *Engine, structure *MemoryStructure, experiences *experienceLedger, parentExperienceID string) (*MemoryStructureExecutionFeedback, error) {
	if e == nil {
		return nil, errors.New("engine unavailable")
	}
	if structure == nil {
		return nil, errors.New("memory structure unavailable")
	}
	if structure.State != memoryStructureValidatedState {
		return nil, errors.New("memory structure is not validated")
	}
	if strings.TrimSpace(structure.ID) == "" {
		return nil, errors.New("memory structure id unavailable")
	}
	if len(structure.Program) == 0 {
		return nil, errors.New("memory structure has no executable program")
	}
	if len(structure.ExpectedOutcome) == 0 {
		return nil, errors.New("memory structure has no predicted outcome")
	}
	if experiences == nil {
		return nil, errors.New("experience ledger unavailable")
	}
	if strings.TrimSpace(parentExperienceID) != "" {
		if _, ok := experiences.Get(parentExperienceID); !ok {
			return nil, fmt.Errorf("experience parent not found: %s", parentExperienceID)
		}
	}

	// 只允许执行 Fabric 中与 Structure 完全一致的实际 Program，避免 Structure 与运行对象发生漂移。
	_, executable, err := e.resolveLocalFabricMemory(structure.ID)
	if err != nil {
		return nil, fmt.Errorf("executable memory not found: %w", err)
	}
	if !equalStringOpProgram(executable.Program, structure.Program) {
		return nil, errors.New("fabric executable program does not match memory structure")
	}

	predicted := capturePredictedOutcome(structure)
	frame := newFrame()
	if err := globalTxnScheduler.canonical(e, structure.ID, frame); err != nil {
		return nil, fmt.Errorf("memory structure VM execution failed: %w", err)
	}
	actual := captureActualOutcome(frame.Vars, predicted)
	errorMap := calculatePredictionError(predicted, actual)

	parentIDs := []string(nil)
	if strings.TrimSpace(parentExperienceID) != "" {
		parentIDs = []string{parentExperienceID}
	}
	observation := cloneStringMap(structure.Observation)
	if observation == nil {
		observation = map[string]string{}
	}
	observation["memory_structure_id"] = structure.ID
	observation["memory_structure_pattern"] = structure.PatternHash
	action := cloneStringMap(structure.Action)
	if action == nil {
		action = map[string]string{}
	}
	action["memory_structure_id"] = structure.ID

	experience, err := experiences.Record(MemoryExperience{
		ParentIDs:       parentIDs,
		Context:         cloneStringMap(structure.Context),
		Observation:     observation,
		Action:          action,
		Outcome:         actual,
		PredictionHash:  structure.PredictionHash,
		PredictionError: errorMap,
	})
	if err != nil {
		return nil, err
	}
	return &MemoryStructureExecutionFeedback{
		StructureID:      structure.ID,
		PredictedOutcome: predicted,
		ActualOutcome:    actual,
		PredictionError:  errorMap,
		Experience:       experience,
	}, nil
}
