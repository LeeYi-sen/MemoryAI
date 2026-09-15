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
)

// MemoryStructureCandidate 是由重复 Experience 事实形成的候选记忆结构。
// 它只携带来源、可重建的模式指纹和证据计数，不包含 Kernel 认知排名。
type MemoryStructureCandidate struct {
	ID                  string   `json:"id"`
	PatternHash         string   `json:"pattern_hash"`
	SourceExperienceIDs []string `json:"source_experience_ids"`
	State               string   `json:"state"`
	// ParentStructureIDs 保留重组结构的真实父结构血缘。
	ParentStructureIDs []string `json:"parent_structure_ids,omitempty"`
	// Program 是重组候选明确携带的 VM 程序；它必须来自已验证父结构，不由 Kernel 猜测。
	Program []Op `json:"program,omitempty"`
	// MutationIndex 记录本次确定性单点变异发生的位置，便于 Memory 追踪结构谱系。
	MutationIndex int `json:"mutation_index,omitempty"`
}

const memoryStructureCandidateState = "candidate"

// memoryStructureFormation 负责把重复出现的 Experience 事实收敛成候选结构。
// 这是 Memory-owned 的生长原语：相似性仅指规范化事实完全一致，不做语义推断。
type memoryStructureFormation struct {
	mu         sync.RWMutex
	candidates map[string]*MemoryStructureCandidate
}

func NewMemoryStructureFormation() *memoryStructureFormation {
	return &memoryStructureFormation{
		candidates: map[string]*MemoryStructureCandidate{},
	}
}

// memoryStructurePatternPayload 去除 Experience 的身份、时间、验证计数和亲缘信息。
// 这样同一事实模式即使来自不同时间或不同父经验，也能被稳定重建。
func memoryStructurePatternPayload(e MemoryExperience) []byte {
	type payload struct {
		Context        map[string]string `json:"context,omitempty"`
		Observation    map[string]string `json:"observation,omitempty"`
		Action         map[string]string `json:"action,omitempty"`
		Outcome        map[string]string `json:"outcome,omitempty"`
		PredictionHash string            `json:"prediction_hash,omitempty"`
	}
	b, _ := json.Marshal(payload{
		Context:        cloneStringMap(e.Context),
		Observation:    cloneStringMap(e.Observation),
		Action:         cloneStringMap(e.Action),
		Outcome:        cloneStringMap(e.Outcome),
		PredictionHash: strings.TrimSpace(e.PredictionHash),
	})
	return b
}

func memoryStructurePatternHash(e MemoryExperience) string {
	sum := sha256.Sum256(memoryStructurePatternPayload(e))
	return hex.EncodeToString(sum[:])
}

func cloneMemoryStructureCandidate(src *MemoryStructureCandidate) *MemoryStructureCandidate {
	if src == nil {
		return nil
	}
	out := *src
	out.SourceExperienceIDs = cloneStrings(src.SourceExperienceIDs)
	out.ParentStructureIDs = cloneStrings(src.ParentStructureIDs)
	out.Program = append([]Op(nil), src.Program...)
	return &out
}

// FormCandidates 根据最小重复证据量形成候选结构。
// minRecurrence 必须 >= 2，避免单次 Experience 被错误提升为稳定结构。
func (f *memoryStructureFormation) FormCandidates(experiences []*MemoryExperience, minRecurrence int) ([]*MemoryStructureCandidate, error) {
	if f == nil {
		return nil, errors.New("memory structure formation unavailable")
	}
	if minRecurrence < 2 {
		return nil, errors.New("minimum recurrence must be at least 2")
	}

	groups := make(map[string][]string)
	for _, experience := range experiences {
		if experience == nil {
			continue
		}
		if len(experience.Observation) == 0 && len(experience.Action) == 0 && len(experience.Outcome) == 0 {
			continue
		}
		patternHash := memoryStructurePatternHash(*experience)
		groups[patternHash] = append(groups[patternHash], experience.ID)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for patternHash, ids := range groups {
		ids = uniqueSortedStrings(ids)
		if len(ids) < minRecurrence {
			continue
		}
		candidateID := fmt.Sprintf("memory-structure-candidate-%s", patternHash[:16])
		candidate, exists := f.candidates[candidateID]
		if !exists {
			candidate = &MemoryStructureCandidate{
				ID:                  candidateID,
				PatternHash:         patternHash,
				SourceExperienceIDs: ids,
				State:               memoryStructureCandidateState,
			}
			f.candidates[candidateID] = candidate
			continue
		}
		candidate.SourceExperienceIDs = mergeSortedUnique(candidate.SourceExperienceIDs, ids)
	}

	out := make([]*MemoryStructureCandidate, 0, len(f.candidates))
	for _, candidate := range f.candidates {
		out = append(out, cloneMemoryStructureCandidate(candidate))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// AdoptCandidate 将 Memory 已经生成的重组候选纳入同一个候选账本。
// 不新增第二套候选数据库；重启后仍由 memory.mem 中的统一 Growth 快照恢复。
func (f *memoryStructureFormation) AdoptCandidate(candidate *MemoryStructureCandidate) error {
	if f == nil {
		return errors.New("memory structure formation unavailable")
	}
	if candidate == nil || strings.TrimSpace(candidate.ID) == "" {
		return errors.New("memory structure candidate unavailable")
	}
	if candidate.State != memoryStructureCandidateState {
		return errors.New("only materialized candidate can be adopted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.candidates[candidate.ID] = cloneMemoryStructureCandidate(candidate)
	return nil
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeSortedUnique(left, right []string) []string {
	merged := make([]string, 0, len(left)+len(right))
	merged = append(merged, left...)
	merged = append(merged, right...)
	return uniqueSortedStrings(merged)
}

func (f *memoryStructureFormation) Get(id string) (*MemoryStructureCandidate, bool) {
	if f == nil {
		return nil, false
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	candidate, ok := f.candidates[id]
	if !ok {
		return nil, false
	}
	return cloneMemoryStructureCandidate(candidate), true
}

func (f *memoryStructureFormation) Snapshot() []*MemoryStructureCandidate {
	if f == nil {
		return nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]*MemoryStructureCandidate, 0, len(f.candidates))
	for _, candidate := range f.candidates {
		out = append(out, cloneMemoryStructureCandidate(candidate))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
