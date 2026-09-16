package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryExperience 是 Memory 自身持有的经验单元。
// 它不是 Kernel 的认知判断；Kernel 只负责提供时间、存储和执行等物理能力。
// 经验的语义、重要性和后续结构化策略必须由 Memory 自己决定。
type MemoryExperience struct {
	ID              string            `json:"id"`
	CreatedNano     int64             `json:"created_nano"`
	ParentIDs       []string          `json:"parent_ids,omitempty"`
	Context         map[string]string `json:"context,omitempty"`
	Observation     map[string]string `json:"observation,omitempty"`
	Action          map[string]string `json:"action,omitempty"`
	Outcome         map[string]string `json:"outcome,omitempty"`
	PredictionHash  string            `json:"prediction_hash,omitempty"`
	// PredictionError 保存一次真实执行后“预测值 -> 实际值”的差异事实。
	// 它属于 Experience 本身，不由 Kernel 解释差异的意义。
	PredictionError map[string]string `json:"prediction_error,omitempty"`
	ContentHash     string            `json:"content_hash"`
	ValidationCount uint64            `json:"validation_count"`
	SuccessCount    uint64            `json:"success_count"`
	FailureCount    uint64            `json:"failure_count"`
}

// ExperienceValidation 记录一次现实反馈对经验的验证结果。
// Success/Failure 只记录事实，不替 Memory 决定“应该相信什么”。
type ExperienceValidation struct {
	ExperienceID string `json:"experience_id"`
	Success      bool   `json:"success"`
	Note         string `json:"note,omitempty"`
	CreatedNano  int64  `json:"created_nano"`
}

type experienceLedger struct {
	mu          sync.RWMutex
	nextSeq     uint64
	byID        map[string]*MemoryExperience
	order       []string
	children    map[string]map[string]struct{}
	validations map[string][]ExperienceValidation
}

// NewMemoryExperienceLedger 创建 Memory 自己的经验账本。
// 账本只负责保存事实、亲缘和验证历史，不内置目标、奖励、相关性或认知排名。
func NewMemoryExperienceLedger() *experienceLedger {
	return &experienceLedger{
		byID:        map[string]*MemoryExperience{},
		children:    map[string]map[string]struct{}{},
		validations: map[string][]ExperienceValidation{},
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := append([]string(nil), src...)
	sort.Strings(out)
	return out
}

func clonePredictionError(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func canonicalExperiencePayload(e MemoryExperience) []byte {
	type payload struct {
		ParentIDs       []string          `json:"parent_ids,omitempty"`
		Context         map[string]string `json:"context,omitempty"`
		Observation     map[string]string `json:"observation,omitempty"`
		Action          map[string]string `json:"action,omitempty"`
		Outcome         map[string]string `json:"outcome,omitempty"`
		PredictionHash  string            `json:"prediction_hash,omitempty"`
		PredictionError map[string]string `json:"prediction_error,omitempty"`
	}
	b, _ := json.Marshal(payload{
		ParentIDs:       cloneStrings(e.ParentIDs),
		Context:         cloneStringMap(e.Context),
		Observation:     cloneStringMap(e.Observation),
		Action:          cloneStringMap(e.Action),
		Outcome:         cloneStringMap(e.Outcome),
		PredictionHash:  strings.TrimSpace(e.PredictionHash),
		PredictionError: clonePredictionError(e.PredictionError),
	})
	return b
}

func experienceContentHash(e MemoryExperience) string {
	sum := sha256.Sum256(canonicalExperiencePayload(e))
	return hex.EncodeToString(sum[:])
}

func (l *experienceLedger) Record(experience MemoryExperience) (*MemoryExperience, error) {
	if l == nil {
		return nil, errors.New("experience ledger unavailable")
	}
	if len(experience.Observation) == 0 && len(experience.Action) == 0 && len(experience.Outcome) == 0 {
		return nil, errors.New("experience requires observation, action, or outcome")
	}

	experience.ParentIDs = cloneStrings(experience.ParentIDs)
	experience.Context = cloneStringMap(experience.Context)
	experience.Observation = cloneStringMap(experience.Observation)
	experience.Action = cloneStringMap(experience.Action)
	experience.Outcome = cloneStringMap(experience.Outcome)
	experience.PredictionHash = strings.TrimSpace(experience.PredictionHash)
	experience.PredictionError = clonePredictionError(experience.PredictionError)
	experience.ContentHash = experienceContentHash(experience)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.nextSeq++
	if experience.CreatedNano == 0 {
		experience.CreatedNano = time.Now().UnixNano()
	}
	if strings.TrimSpace(experience.ID) == "" {
		experience.ID = fmt.Sprintf("experience-%d-%d", experience.CreatedNano, l.nextSeq)
	}
	if _, exists := l.byID[experience.ID]; exists {
		return nil, fmt.Errorf("experience id already exists: %s", experience.ID)
	}

	for _, parentID := range experience.ParentIDs {
		if _, ok := l.byID[parentID]; !ok {
			return nil, fmt.Errorf("experience parent not found: %s", parentID)
		}
		if l.children[parentID] == nil {
			l.children[parentID] = map[string]struct{}{}
		}
		l.children[parentID][experience.ID] = struct{}{}
	}

	stored := experience
	l.byID[stored.ID] = &stored
	l.order = append(l.order, stored.ID)
	return cloneExperience(&stored), nil
}

func cloneExperience(src *MemoryExperience) *MemoryExperience {
	if src == nil {
		return nil
	}
	out := *src
	out.ParentIDs = cloneStrings(src.ParentIDs)
	out.Context = cloneStringMap(src.Context)
	out.Observation = cloneStringMap(src.Observation)
	out.Action = cloneStringMap(src.Action)
	out.Outcome = cloneStringMap(src.Outcome)
	out.PredictionError = clonePredictionError(src.PredictionError)
	return &out
}

func (l *experienceLedger) Get(id string) (*MemoryExperience, bool) {
	if l == nil {
		return nil, false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	e, ok := l.byID[id]
	if !ok {
		return nil, false
	}
	return cloneExperience(e), true
}

func (l *experienceLedger) Children(id string) []*MemoryExperience {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	ids := make([]string, 0, len(l.children[id]))
	for child := range l.children[id] {
		ids = append(ids, child)
	}
	sort.Strings(ids)
	out := make([]*MemoryExperience, 0, len(ids))
	for _, childID := range ids {
		out = append(out, cloneExperience(l.byID[childID]))
	}
	l.mu.RUnlock()
	return out
}

func (l *experienceLedger) Validate(id string, success bool, note string) (ExperienceValidation, error) {
	if l == nil {
		return ExperienceValidation{}, errors.New("experience ledger unavailable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.byID[id]
	if !ok {
		return ExperienceValidation{}, fmt.Errorf("experience not found: %s", id)
	}
	validation := ExperienceValidation{
		ExperienceID: id,
		Success:      success,
		Note:         strings.TrimSpace(note),
		CreatedNano:  time.Now().UnixNano(),
	}
	e.ValidationCount++
	if success {
		e.SuccessCount++
	} else {
		e.FailureCount++
	}
	l.validations[id] = append(l.validations[id], validation)
	return validation, nil
}

func (l *experienceLedger) ValidationHistory(id string) []ExperienceValidation {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]ExperienceValidation(nil), l.validations[id]...)
}

func (l *experienceLedger) Snapshot() []*MemoryExperience {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*MemoryExperience, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, cloneExperience(l.byID[id]))
	}
	return out
}
