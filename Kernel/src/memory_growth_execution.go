package main

import (
	"errors"
	"strings"
)

// MemoryStructureExecution 是 Memory Structure 的可执行物理表示。
// Kernel 只负责保存/承载 VM Program；程序表达的意义、何时调用以及如何评价结果，仍属于 Memory。
type MemoryStructureExecution struct {
	StructureID string `json:"structure_id"`
	Program     []Op  `json:"program"`
}

// BindExecutableProgram 将一个已经验证的 Memory Structure 绑定到真实 VM Program。
// 这里不根据 Action 字段猜测程序；可执行结构必须由 Memory 明确提供程序，避免 Kernel 偷渡认知策略。
func BindExecutableProgram(structure *MemoryStructure, program []Op) error {
	if structure == nil {
		return errors.New("memory structure unavailable")
	}
	if strings.TrimSpace(structure.ID) == "" {
		return errors.New("memory structure id unavailable")
	}
	if structure.State != memoryStructureValidatedState {
		return errors.New("only validated memory structure can become executable")
	}
	if len(program) == 0 {
		return errors.New("executable program unavailable")
	}
	structure.Program = append([]Op(nil), program...)
	return nil
}

// ExecutableMemory 将 Memory Structure 转换成现有 VM 可以直接承载的 Memory。
// 不创建新的运行时数据库；Program 与结构状态最终仍随 Memory Growth 状态进入 memory.mem。
func ExecutableMemory(structure *MemoryStructure) (*Memory, error) {
	if structure == nil {
		return nil, errors.New("memory structure unavailable")
	}
	if structure.State != memoryStructureValidatedState {
		return nil, errors.New("memory structure is not validated")
	}
	if len(structure.Program) == 0 {
		return nil, errors.New("memory structure has no executable program")
	}
	state := cloneStringMap(structure.Context)
	if state == nil {
		state = map[string]string{}
	}
	// 将已知的结构事实以普通 State 暴露给现有 VM；不在 Kernel 中增加认知评分字段。
	state["memory_structure_id"] = structure.ID
	state["memory_structure_pattern"] = structure.PatternHash
	return &Memory{
		ID:      structure.ID,
		Layer:   "emergent",
		Tags:    []string{"memory", "memory-structure", "executable"},
		State:   stringMapToAny(state),
		Program: append([]Op(nil), structure.Program...),
	}, nil
}

func stringMapToAny(values map[string]string) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
