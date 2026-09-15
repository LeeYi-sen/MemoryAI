package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const memoryStructureValidatedState = "validated"

// MemoryStructureValidation 记录候选结构接受一次新的现实 Experience 验证的事实。
// 验证证据必须来自候选形成之后的独立 Experience，避免用形成结构的原始证据自证。
type MemoryStructureValidation struct {
	CandidateID  string `json:"candidate_id"`
	ExperienceID string `json:"experience_id"`
	Success      bool   `json:"success"`
	CreatedNano  int64  `json:"created_nano"`
	Note         string `json:"note,omitempty"`
}

// MemoryStructure 是经过独立现实证据验证后的第一类可复用记忆结构。
// 它仍然是 Memory-owned 数据，不是 Kernel 的认知策略或 Skill 模块。
type MemoryStructure struct {
	ID                       string            `json:"id"`
	PatternHash              string            `json:"pattern_hash"`
	SourceExperienceIDs      []string          `json:"source_experience_ids"`
	ValidationExperienceIDs  []string          `json:"validation_experience_ids"`
	Context                  map[string]string `json:"context,omitempty"`
	Observation              map[string]string `json:"observation,omitempty"`
	Action                   map[string]string `json:"action,omitempty"`
	ExpectedOutcome          map[string]string `json:"expected_outcome,omitempty"`
	PredictionHash           string            `json:"prediction_hash,omitempty"`
	State                    string            `json:"state"`
	ValidationCount          uint64            `json:"validation_count"`
	SuccessCount             uint64            `json:"success_count"`
	FailureCount             uint64            `json:"failure_count"`
}

type memoryStructureValidationLedger struct {
	mu        sync.RWMutex
	history   map[string][]MemoryStructureValidation
	structures map[string]*MemoryStructure
	validated map[string]*MemoryStructure
}

// NewMemoryStructureValidationLedger 创建候选结构的现实验证账本。
func NewMemoryStructureValidationLedger() *memoryStructureValidationLedger {
	return &memoryStructureValidationLedger{
		history:    map[string][]MemoryStructureValidation{},
		structures: map[string]*MemoryStructure{},
		validated:  map[string]*MemoryStructure{},
	}
}

func cloneMemoryStructure(src *MemoryStructure) *MemoryStructure {
	if src == nil {
		return nil
	}
	out := *src
	out.SourceExperienceIDs = cloneStrings(src.SourceExperienceIDs)
	out.ValidationExperienceIDs = cloneStrings(src.ValidationExperienceIDs)
	out.Context = cloneStringMap(src.Context)
	out.Observation = cloneStringMap(src.Observation)
	out.Action = cloneStringMap(src.Action)
	out.ExpectedOutcome = cloneStringMap(src.ExpectedOutcome)
	return &out
}

func cloneMemoryStructureValidations(src []MemoryStructureValidation) []MemoryStructureValidation {
	if len(src) == 0 {
		return nil
	}
	return append([]MemoryStructureValidation(nil), src...)
}

// ValidateCandidate 使用新的 Experience 验证候选结构。
// Success 只表示该新 Experience 与候选的完整事实模式再次一致；它不替 Memory 做语义判断。
// minSuccess 达到后才允许从 candidate 晋升为 validated Memory Structure。
func (l *memoryStructureValidationLedger) ValidateCandidate(candidate *MemoryStructureCandidate, source *MemoryExperience, witness *MemoryExperience, minSuccess int) (MemoryStructureValidation, *MemoryStructure, error) {
	if l == nil {
		return MemoryStructureValidation{}, nil, errors.New("memory structure validation ledger unavailable")
	}
	if candidate == nil {
		return MemoryStructureValidation{}, nil, errors.New("memory structure candidate unavailable")
	}
	if candidate.State != memoryStructureCandidateState {
		return MemoryStructureValidation{}, nil, fmt.Errorf("candidate is not in candidate state: %s", candidate.State)
	}
	if source == nil {
		return MemoryStructureValidation{}, nil, errors.New("source experience unavailable")
	}
	if witness == nil {
		return MemoryStructureValidation{}, nil, errors.New("validation experience unavailable")
	}
	if minSuccess < 1 {
		return MemoryStructureValidation{}, nil, errors.New("minimum validation success must be at least 1")
	}
	if strings.TrimSpace(candidate.PatternHash) == "" {
		return MemoryStructureValidation{}, nil, errors.New("candidate pattern hash unavailable")
	}
	if memoryStructurePatternHash(*source) != candidate.PatternHash {
		return MemoryStructureValidation{}, nil, errors.New("source experience does not match candidate pattern")
	}
	if !containsString(candidate.SourceExperienceIDs, source.ID) {
		return MemoryStructureValidation{}, nil, errors.New("source experience is not part of candidate provenance")
	}
	if containsString(candidate.SourceExperienceIDs, witness.ID) {
		return MemoryStructureValidation{}, nil, errors.New("validation experience must be independent of source provenance")
	}
	if strings.TrimSpace(witness.ID) == "" {
		return MemoryStructureValidation{}, nil, errors.New("validation experience id unavailable")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	for _, previous := range l.history[candidate.ID] {
		if previous.ExperienceID == witness.ID {
			return MemoryStructureValidation{}, nil, fmt.Errorf("validation experience already recorded: %s", witness.ID)
		}
	}

	success := memoryStructurePatternHash(*witness) == candidate.PatternHash
	validation := MemoryStructureValidation{
		CandidateID:  candidate.ID,
		ExperienceID: witness.ID,
		Success:      success,
		CreatedNano:  time.Now().UnixNano(),
	}
	if success {
		validation.Note = "independent experience matches candidate pattern"
	} else {
		validation.Note = "independent experience does not match candidate pattern"
	}
	l.history[candidate.ID] = append(l.history[candidate.ID], validation)

	structure := l.structures[candidate.ID]
	if structure == nil {
		structure = &MemoryStructure{
			ID:                  fmt.Sprintf("memory-structure-%s", candidate.PatternHash[:16]),
			PatternHash:         candidate.PatternHash,
			SourceExperienceIDs: cloneStrings(candidate.SourceExperienceIDs),
			Context:             cloneStringMap(source.Context),
			Observation:         cloneStringMap(source.Observation),
			Action:              cloneStringMap(source.Action),
			ExpectedOutcome:     cloneStringMap(source.Outcome),
			PredictionHash:      strings.TrimSpace(source.PredictionHash),
			State:               memoryStructureValidatedState,
		}
		l.structures[candidate.ID] = structure
	}
	structure.ValidationCount++
	if success {
		structure.SuccessCount++
		structure.ValidationExperienceIDs = mergeSortedUnique(structure.ValidationExperienceIDs, []string{witness.ID})
	} else {
		structure.FailureCount++
	}
	if structure.SuccessCount >= uint64(minSuccess) {
		l.validated[candidate.ID] = structure
		return validation, cloneMemoryStructure(structure), nil
	}

	return validation, nil, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// GetValidated 返回已经达到晋升阈值的 Memory Structure。
func (l *memoryStructureValidationLedger) GetValidated(candidateID string) (*MemoryStructure, bool) {
	if l == nil {
		return nil, false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	structure, ok := l.validated[candidateID]
	if !ok {
		return nil, false
	}
	return cloneMemoryStructure(structure), true
}

// ValidationHistory 返回候选结构全部独立验证事实，包括失败证据。
func (l *memoryStructureValidationLedger) ValidationHistory(candidateID string) []MemoryStructureValidation {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return cloneMemoryStructureValidations(l.history[candidateID])
}

// Snapshot 返回全部已经晋升的 Memory Structure，并保持稳定排序。
func (l *memoryStructureValidationLedger) Snapshot() []*MemoryStructure {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*MemoryStructure, 0, len(l.validated))
	for _, structure := range l.validated {
		out = append(out, cloneMemoryStructure(structure))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
