package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// memoryGrowthStateRecordID 是 Memory Growth Core 在 Memory.mem 中使用的唯一状态记录。
// 它不是第二个数据库，也不是旁路账本；所有经验、候选结构、验证历史和已验证结构
// 都作为一个普通 Memory 的 State 持久化到同一个 memory.mem 物理容器中。
const memoryGrowthStateRecordID = "__memoryai.growth.core"

const memoryGrowthStateVersion = 1

// memoryGrowthPersistedState 是 Memory Growth Core 的可恢复状态快照。
// 该结构只负责序列化，不承担任何认知决策。
type memoryGrowthPersistedState struct {
	Version                   int                                      `json:"version"`
	ExperienceNextSeq         uint64                                   `json:"experience_next_seq"`
	ExperienceOrder           []string                                 `json:"experience_order"`
	Experiences               []*MemoryExperience                      `json:"experiences"`
	ExperienceValidations     map[string][]ExperienceValidation        `json:"experience_validations"`
	Candidates                []*MemoryStructureCandidate              `json:"candidates"`
	StructureValidationHistory map[string][]MemoryStructureValidation  `json:"structure_validation_history"`
	PendingStructures         []*MemoryStructure                         `json:"pending_structures"`
	ValidatedCandidateIDs     []string                                 `json:"validated_candidate_ids"`
}

func newMemoryGrowthPersistedState() memoryGrowthPersistedState {
	return memoryGrowthPersistedState{
		Version:                    memoryGrowthStateVersion,
		ExperienceValidations:      map[string][]ExperienceValidation{},
		StructureValidationHistory: map[string][]MemoryStructureValidation{},
	}
}

// snapshotMemoryGrowthState 将三个 Memory-owned 生长账本收敛成一个可持久化快照。
// 这里没有任何独立文件路径，因此部署时仍然只有启动程序 + memory.mem。
func snapshotMemoryGrowthState(experiences *experienceLedger, formation *memoryStructureFormation, validation *memoryStructureValidationLedger) memoryGrowthPersistedState {
	state := newMemoryGrowthPersistedState()
	if experiences != nil {
		experiences.mu.RLock()
		state.ExperienceNextSeq = experiences.nextSeq
		state.ExperienceOrder = append([]string(nil), experiences.order...)
		state.Experiences = make([]*MemoryExperience, 0, len(experiences.order))
		for _, id := range experiences.order {
			state.Experiences = append(state.Experiences, cloneExperience(experiences.byID[id]))
		}
		for id, history := range experiences.validations {
			state.ExperienceValidations[id] = append([]ExperienceValidation(nil), history...)
		}
		experiences.mu.RUnlock()
	}
	if formation != nil {
		formation.mu.RLock()
		state.Candidates = make([]*MemoryStructureCandidate, 0, len(formation.candidates))
		for _, candidate := range formation.candidates {
			state.Candidates = append(state.Candidates, cloneMemoryStructureCandidate(candidate))
		}
		formation.mu.RUnlock()
		sort.Slice(state.Candidates, func(i, j int) bool { return state.Candidates[i].ID < state.Candidates[j].ID })
	}
	if validation != nil {
		validation.mu.RLock()
		for id, history := range validation.history {
			state.StructureValidationHistory[id] = cloneMemoryStructureValidations(history)
		}
		state.PendingStructures = make([]*MemoryStructure, 0, len(validation.structures))
		for _, structure := range validation.structures {
			state.PendingStructures = append(state.PendingStructures, cloneMemoryStructure(structure))
		}
		for id := range validation.validated {
			state.ValidatedCandidateIDs = append(state.ValidatedCandidateIDs, id)
		}
		validation.mu.RUnlock()
		sort.Slice(state.PendingStructures, func(i, j int) bool { return state.PendingStructures[i].ID < state.PendingStructures[j].ID })
		sort.Strings(state.ValidatedCandidateIDs)
	}
	return state
}

func restoreMemoryGrowthState(state memoryGrowthPersistedState) (*experienceLedger, *memoryStructureFormation, *memoryStructureValidationLedger, error) {
	if state.Version != 0 && state.Version != memoryGrowthStateVersion {
		return nil, nil, nil, fmt.Errorf("unsupported memory growth state version: %d", state.Version)
	}
	experiences := NewMemoryExperienceLedger()
	experiences.nextSeq = state.ExperienceNextSeq
	experiences.order = append([]string(nil), state.ExperienceOrder...)
	for _, experience := range state.Experiences {
		if experience == nil || strings.TrimSpace(experience.ID) == "" {
			continue
		}
		experiences.byID[experience.ID] = cloneExperience(experience)
	}
	if len(experiences.order) == 0 {
		for id := range experiences.byID {
			experiences.order = append(experiences.order, id)
		}
		sort.Strings(experiences.order)
	}
	for _, experience := range experiences.byID {
		for _, parentID := range experience.ParentIDs {
			if experiences.children[parentID] == nil {
				experiences.children[parentID] = map[string]struct{}{}
			}
			experiences.children[parentID][experience.ID] = struct{}{}
		}
	}
	for id, history := range state.ExperienceValidations {
		experiences.validations[id] = append([]ExperienceValidation(nil), history...)
	}

	formation := NewMemoryStructureFormation()
	for _, candidate := range state.Candidates {
		if candidate == nil || strings.TrimSpace(candidate.ID) == "" {
			continue
		}
		formation.candidates[candidate.ID] = cloneMemoryStructureCandidate(candidate)
	}

	validation := NewMemoryStructureValidationLedger()
	for id, history := range state.StructureValidationHistory {
		validation.history[id] = cloneMemoryStructureValidations(history)
	}
	for _, structure := range state.PendingStructures {
		if structure == nil || strings.TrimSpace(structure.ID) == "" {
			continue
		}
		candidateID := candidateIDFromStructure(structure)
		validation.structures[candidateID] = cloneMemoryStructure(structure)
	}
	for _, candidateID := range state.ValidatedCandidateIDs {
		if structure, ok := validation.structures[candidateID]; ok {
			validation.validated[candidateID] = cloneMemoryStructure(structure)
		}
	}
	return experiences, formation, validation, nil
}

func candidateIDFromStructure(structure *MemoryStructure) string {
	if structure == nil {
		return ""
	}
	if len(structure.PatternHash) >= 16 {
		return fmt.Sprintf("memory-structure-candidate-%s", structure.PatternHash[:16])
	}
	return structure.ID
}

func encodeMemoryGrowthState(state memoryGrowthPersistedState) ([]byte, error) {
	if state.Version == 0 {
		state.Version = memoryGrowthStateVersion
	}
	return json.Marshal(state)
}

func decodeMemoryGrowthState(raw []byte) (memoryGrowthPersistedState, error) {
	state := newMemoryGrowthPersistedState()
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if state.Version == 0 {
		state.Version = memoryGrowthStateVersion
	}
	if state.Version != memoryGrowthStateVersion {
		return state, fmt.Errorf("unsupported memory growth state version: %d", state.Version)
	}
	return state, nil
}

func readMemoryGrowthStateFromEngine(e *Engine) (memoryGrowthPersistedState, bool, error) {
	if e == nil {
		return memoryGrowthPersistedState{}, false, errors.New("engine unavailable")
	}
	m, err := e.resolveIDLocal(memoryGrowthStateRecordID)
	if err != nil {
		return newMemoryGrowthPersistedState(), false, nil
	}
	if m.State == nil {
		return newMemoryGrowthPersistedState(), true, nil
	}
	value, ok := m.State["memory_growth_state"]
	if !ok {
		return newMemoryGrowthPersistedState(), true, nil
	}
	var raw []byte
	switch v := value.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = append([]byte(nil), v...)
	default:
		var marshalErr error
		raw, marshalErr = json.Marshal(v)
		if marshalErr != nil {
			return memoryGrowthPersistedState{}, true, marshalErr
		}
	}
	state, err := decodeMemoryGrowthState(raw)
	return state, true, err
}

// LoadMemoryGrowthState 从同一个 memory.mem 中恢复 Memory Growth Core。
// 不存在专用数据库或旁路文件；没有该记录时返回全新的空 Memory 状态。
func LoadMemoryGrowthState(e *Engine) (*experienceLedger, *memoryStructureFormation, *memoryStructureValidationLedger, error) {
	state, _, err := readMemoryGrowthStateFromEngine(e)
	if err != nil {
		return nil, nil, nil, err
	}
	return restoreMemoryGrowthState(state)
}

// PersistMemoryGrowthState 将 Memory Growth Core 的全部状态写回一个普通 Memory 记录。
// 后续由既有 saveBody / persistEngineIncremental 把该记录落到 memory.mem，绝不产生第二个持久化文件。
func PersistMemoryGrowthState(e *Engine, experiences *experienceLedger, formation *memoryStructureFormation, validation *memoryStructureValidationLedger) error {
	if e == nil {
		return errors.New("engine unavailable")
	}
	state := snapshotMemoryGrowthState(experiences, formation, validation)
	raw, err := encodeMemoryGrowthState(state)
	if err != nil {
		return err
	}

	e.dataMu.Lock()
	defer e.dataMu.Unlock()
	m := e.cache[memoryGrowthStateRecordID]
	if m == nil {
		m = &Memory{ID: memoryGrowthStateRecordID, Layer: "emergent", Tags: []string{"memory-growth"}, State: map[string]any{}}
		e.cache[memoryGrowthStateRecordID] = m
		e.newIDs[memoryGrowthStateRecordID] = true
	}
	if m.State == nil {
		m.State = map[string]any{}
	}
	m.State["memory_growth_state"] = string(raw)
	m.Revision++
	e.dirtyIDs[memoryGrowthStateRecordID] = true
	e.dirty = true
	return nil
}
