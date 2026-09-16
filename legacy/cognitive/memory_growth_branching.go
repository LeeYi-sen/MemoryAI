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
	memoryContextSplitTag             = "memory-context-split"
	memoryContextSplitParentTagPrefix = "memory-context-split.parent:"

	memoryContextSplitForming    = "forming"
	memoryContextSplitValidating = "validating"
	memoryContextSplitValidated  = "validated"
	memoryContextSplitContested  = "contested"

	memoryStructureContextSplitState = "context_split"
)

// MemoryContextBranchEvidence 是由重复 Reality Experience 形成的一个上下文分支事实。
// ContextKey/Value 来自 Experience.Context，本结构不定义任何固定语义维度。
type MemoryContextBranchEvidence struct {
	ContextValue           string   `json:"context_value"`
	ObservedValue          string   `json:"observed_value"`
	PatternHash            string   `json:"pattern_hash"`
	CandidateID            string   `json:"candidate_id"`
	FormationExperienceIDs []string `json:"formation_experience_ids"`
}

// MemoryContextSplitState 是普通 Memory 中保存的上下文裂分事实。
// Kernel 只保存“哪些 Context 值稳定对应哪些现实结果”，不计算置信度或语义权重。
type MemoryContextSplitState struct {
	ID                string                        `json:"id"`
	ParentStructureID string                        `json:"parent_structure_id"`
	BeliefID          string                        `json:"belief_id"`
	PredictionKey     string                        `json:"prediction_key"`
	PredictedValue    string                        `json:"predicted_value"`
	ContextKey        string                        `json:"context_key"`
	Branches          []MemoryContextBranchEvidence `json:"branches"`
	Status            string                        `json:"status"`
	CreatedNano       int64                         `json:"created_nano"`
	UpdatedNano       int64                         `json:"updated_nano"`
}

// ContextBranchingCycleResult 报告一次 Belief -> Context Split 生长步骤。
type ContextBranchingCycleResult struct {
	SplitID              string   `json:"split_id,omitempty"`
	ParentStructureID    string   `json:"parent_structure_id,omitempty"`
	ContextKey           string   `json:"context_key,omitempty"`
	CandidateIDs         []string `json:"candidate_ids,omitempty"`
	ValidatedCandidateID string   `json:"validated_candidate_id,omitempty"`
	ValidatedStructureID string   `json:"validated_structure_id,omitempty"`
	ParentRetired        bool     `json:"parent_retired,omitempty"`
	Contested            bool     `json:"contested,omitempty"`
	Persisted            bool     `json:"persisted,omitempty"`
	Skipped              bool     `json:"skipped,omitempty"`
}

var memoryContextBranchingActive sync.Map

func enterMemoryContextBranching(e *Engine) bool {
	root := memoryGrowthRoot(e)
	if root == nil {
		return false
	}
	_, loaded := memoryContextBranchingActive.LoadOrStore(root, struct{}{})
	return !loaded
}

func leaveMemoryContextBranching(e *Engine) {
	if root := memoryGrowthRoot(e); root != nil {
		memoryContextBranchingActive.Delete(root)
	}
}

func memoryContextSplitID(parentID, beliefID, contextKey string) string {
	payload := strings.TrimSpace(parentID) + "\x00" + strings.TrimSpace(beliefID) + "\x00" + strings.TrimSpace(contextKey)
	sum := sha256.Sum256([]byte(payload))
	return "memory-context-split-" + hex.EncodeToString(sum[:16])
}

func memoryContextBranchCandidateID(parentID, predictionKey, contextKey, contextValue, patternHash string) string {
	payload := strings.TrimSpace(parentID) + "\x00" + strings.TrimSpace(predictionKey) + "\x00" + strings.TrimSpace(contextKey) + "\x00" + contextValue + "\x00" + strings.TrimSpace(patternHash)
	sum := sha256.Sum256([]byte(payload))
	return "memory-context-branch-candidate-" + hex.EncodeToString(sum[:16])
}

func cloneMemoryContextSplitState(src *MemoryContextSplitState) *MemoryContextSplitState {
	if src == nil {
		return nil
	}
	out := *src
	out.Branches = make([]MemoryContextBranchEvidence, len(src.Branches))
	copy(out.Branches, src.Branches)
	for i := range out.Branches {
		out.Branches[i].FormationExperienceIDs = cloneStrings(src.Branches[i].FormationExperienceIDs)
	}
	return &out
}

func memoryContextSplitMemory(state *MemoryContextSplitState, revision uint64) (*Memory, error) {
	if state == nil || strings.TrimSpace(state.ID) == "" {
		return nil, errors.New("memory context split unavailable")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:       state.ID,
		Layer:    "emergent",
		Tags:     []string{"memory", memoryContextSplitTag, memoryContextSplitParentTagPrefix + state.ParentStructureID},
		Content:  "Memory-native contextual Structure split evidence.",
		Revision: revision,
		State: map[string]any{
			"parent_structure_id": state.ParentStructureID,
			"belief_id":           state.BeliefID,
			"prediction_key":      state.PredictionKey,
			"context_key":         state.ContextKey,
			"status":              state.Status,
			"split_state":         string(raw),
		},
	}, nil
}

func decodeMemoryContextSplitState(raw any) (*MemoryContextSplitState, error) {
	var payload []byte
	switch value := raw.(type) {
	case string:
		payload = []byte(value)
	case []byte:
		payload = append([]byte(nil), value...)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}
	var state MemoryContextSplitState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func loadMemoryContextSplit(e *Engine, splitID string) (*MemoryContextSplitState, uint64, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, 0, errors.New("memory context split engine unavailable")
	}
	_, memory, err := root.resolveLocalFabricMemory(strings.TrimSpace(splitID))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	if memory == nil || !memoryHasTag(memory, memoryContextSplitTag) {
		return nil, 0, fmt.Errorf("Memory id %s collides with non-context-split Memory", splitID)
	}
	state, err := decodeMemoryContextSplitState(memory.State["split_state"])
	if err != nil {
		return nil, 0, err
	}
	return state, memory.Revision, nil
}

func upsertMemoryContextSplit(e *Engine, state *MemoryContextSplitState, revision uint64) error {
	root := memoryGrowthRoot(e)
	if root == nil {
		return errors.New("memory context split engine unavailable")
	}
	memory, err := memoryContextSplitMemory(state, revision)
	if err != nil {
		return err
	}
	return root.upsertExplicitMemoryBounded(memory)
}

func listMemoryContextSplits(e *Engine) ([]*MemoryContextSplitState, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("memory context split engine unavailable")
	}
	ids, err := root.listTagFabric(memoryContextSplitTag)
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	out := make([]*MemoryContextSplitState, 0, len(ids))
	for _, id := range ids {
		state, _, err := loadMemoryContextSplit(root, id)
		if err != nil {
			return nil, err
		}
		if state != nil {
			out = append(out, state)
		}
	}
	return out, nil
}

type contextPatternGroup struct {
	PatternHash   string
	Context       map[string]string
	ObservedValue string
	ExperienceIDs []string
}

func beliefExperiencesInLedgerOrder(experiences *experienceLedger, belief *MemoryBeliefState) []*MemoryExperience {
	if experiences == nil || belief == nil {
		return nil
	}
	allowed := make(map[string]struct{}, len(belief.EvidenceExperienceIDs))
	for _, id := range belief.EvidenceExperienceIDs {
		allowed[id] = struct{}{}
	}
	out := make([]*MemoryExperience, 0, len(allowed))
	for _, experience := range experiences.Snapshot() {
		if experience == nil {
			continue
		}
		if _, ok := allowed[experience.ID]; ok {
			out = append(out, experience)
		}
	}
	return out
}

func repeatedBeliefPatternGroups(experiences *experienceLedger, belief *MemoryBeliefState) []*contextPatternGroup {
	ordered := beliefExperiencesInLedgerOrder(experiences, belief)
	groupsByHash := map[string]*contextPatternGroup{}
	order := make([]string, 0)
	for _, experience := range ordered {
		actual, present := experience.Outcome[belief.PredictionKey]
		if !present {
			continue
		}
		patternHash := memoryStructurePatternHash(*experience)
		group := groupsByHash[patternHash]
		if group == nil {
			group = &contextPatternGroup{
				PatternHash:   patternHash,
				Context:       cloneStringMap(experience.Context),
				ObservedValue: actual,
			}
			groupsByHash[patternHash] = group
			order = append(order, patternHash)
		}
		group.ExperienceIDs = append(group.ExperienceIDs, experience.ID)
	}
	out := make([]*contextPatternGroup, 0, len(order))
	for _, hash := range order {
		group := groupsByHash[hash]
		if group != nil && len(group.ExperienceIDs) >= 2 {
			out = append(out, group)
		}
	}
	return out
}

// discriminatingContextKey 寻找一个由现实证据完全区分结果的动态 Context 键。
// 选择只采用可证明的分离关系和稳定字典序；没有分数、权重或预置语义维度。
func discriminatingContextKey(groups []*contextPatternGroup, predictedValue string) string {
	if len(groups) < 2 {
		return ""
	}
	keySet := map[string]struct{}{}
	for _, group := range groups {
		for key := range group.Context {
			keySet[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		outcomeByContext := map[string]string{}
		hasSupport := false
		hasConflict := false
		valid := true
		for _, group := range groups {
			contextValue, present := group.Context[key]
			if !present {
				valid = false
				break
			}
			if previous, exists := outcomeByContext[contextValue]; exists && previous != group.ObservedValue {
				valid = false
				break
			}
			outcomeByContext[contextValue] = group.ObservedValue
			if group.ObservedValue == predictedValue {
				hasSupport = true
			} else {
				hasConflict = true
			}
		}
		if valid && len(outcomeByContext) >= 2 && hasSupport && hasConflict {
			return key
		}
	}
	return ""
}

func contextSplitBranches(parent *MemoryStructure, belief *MemoryBeliefState, groups []*contextPatternGroup, contextKey string) []MemoryContextBranchEvidence {
	byContext := map[string]*contextPatternGroup{}
	for _, group := range groups {
		value, ok := group.Context[contextKey]
		if !ok {
			continue
		}
		current := byContext[value]
		if current == nil || group.PatternHash < current.PatternHash {
			byContext[value] = group
		}
	}
	values := make([]string, 0, len(byContext))
	for value := range byContext {
		values = append(values, value)
	}
	sort.Strings(values)
	branches := make([]MemoryContextBranchEvidence, 0, len(values))
	for _, value := range values {
		group := byContext[value]
		if group == nil || len(group.ExperienceIDs) < 2 {
			continue
		}
		branches = append(branches, MemoryContextBranchEvidence{
			ContextValue:           value,
			ObservedValue:          group.ObservedValue,
			PatternHash:            group.PatternHash,
			CandidateID:            memoryContextBranchCandidateID(parent.ID, belief.PredictionKey, contextKey, value, group.PatternHash),
			FormationExperienceIDs: cloneStrings(group.ExperienceIDs[:2]),
		})
	}
	return branches
}

func proposeMemoryContextSplit(parent *MemoryStructure, belief *MemoryBeliefState, experiences *experienceLedger) *MemoryContextSplitState {
	if parent == nil || belief == nil || experiences == nil || len(parent.Program) == 0 {
		return nil
	}
	if belief.StructureID != parent.ID || len(belief.SupportingExperienceIDs) < 2 || len(belief.ConflictingExperienceIDs) < 2 {
		return nil
	}
	groups := repeatedBeliefPatternGroups(experiences, belief)
	contextKey := discriminatingContextKey(groups, belief.PredictedValue)
	if contextKey == "" {
		return nil
	}
	branches := contextSplitBranches(parent, belief, groups, contextKey)
	if len(branches) < 2 {
		return nil
	}
	now := time.Now().UnixNano()
	return &MemoryContextSplitState{
		ID:                memoryContextSplitID(parent.ID, belief.ID, contextKey),
		ParentStructureID: parent.ID,
		BeliefID:          belief.ID,
		PredictionKey:     belief.PredictionKey,
		PredictedValue:    belief.PredictedValue,
		ContextKey:        contextKey,
		Branches:          branches,
		Status:            memoryContextSplitForming,
		CreatedNano:       now,
		UpdatedNano:       now,
	}
}

func adoptContextSplitCandidates(formation *memoryStructureFormation, parent *MemoryStructure, split *MemoryContextSplitState) error {
	if formation == nil || parent == nil || split == nil {
		return errors.New("context split candidate adoption unavailable")
	}
	for _, branch := range split.Branches {
		candidate := &MemoryStructureCandidate{
			ID:                  branch.CandidateID,
			PatternHash:         branch.PatternHash,
			SourceExperienceIDs: cloneStrings(branch.FormationExperienceIDs),
			State:               memoryStructureCandidateState,
			ParentStructureIDs:  []string{parent.ID},
			Program:             append([]Op(nil), parent.Program...),
		}
		if err := formation.AdoptCandidate(candidate); err != nil {
			return err
		}
	}
	return nil
}

func contextSplitEvidenceStillSeparated(split *MemoryContextSplitState, belief *MemoryBeliefState, experiences *experienceLedger) bool {
	if split == nil || belief == nil || experiences == nil || split.ContextKey == "" {
		return false
	}
	outcomeByContext := map[string]string{}
	hasSupport := false
	hasConflict := false
	for _, experience := range beliefExperiencesInLedgerOrder(experiences, belief) {
		actual, present := experience.Outcome[split.PredictionKey]
		if !present {
			continue
		}
		contextValue, present := experience.Context[split.ContextKey]
		if !present {
			return false
		}
		if previous, exists := outcomeByContext[contextValue]; exists && previous != actual {
			return false
		}
		outcomeByContext[contextValue] = actual
		if actual == split.PredictedValue {
			hasSupport = true
		} else {
			hasConflict = true
		}
	}
	return len(outcomeByContext) >= 2 && hasSupport && hasConflict
}

func findIndependentContextBranchWitness(experiences *experienceLedger, candidate *MemoryStructureCandidate) *MemoryExperience {
	if experiences == nil || candidate == nil {
		return nil
	}
	for _, experience := range experiences.Snapshot() {
		if experience == nil || containsString(candidate.SourceExperienceIDs, experience.ID) {
			continue
		}
		if memoryStructurePatternHash(*experience) == candidate.PatternHash {
			return experience
		}
	}
	return nil
}

func installValidatedContextBranch(e *Engine, validation *memoryStructureValidationLedger, candidate *MemoryStructureCandidate, structure *MemoryStructure) error {
	if e == nil || validation == nil || candidate == nil || structure == nil {
		return errors.New("validated context branch unavailable")
	}
	if err := BindExecutableProgram(structure, candidate.Program); err != nil {
		return err
	}
	executable, err := ExecutableMemory(structure)
	if err != nil {
		return err
	}
	root := memoryGrowthRoot(e)
	if err := root.upsertExplicitMemoryBounded(executable); err != nil {
		return err
	}
	validation.mu.Lock()
	validation.structures[candidate.ID] = cloneMemoryStructure(structure)
	validation.validated[candidate.ID] = cloneMemoryStructure(structure)
	validation.mu.Unlock()
	return nil
}

func allContextSplitBranchesValidated(validation *memoryStructureValidationLedger, split *MemoryContextSplitState) bool {
	if validation == nil || split == nil || len(split.Branches) < 2 {
		return false
	}
	for _, branch := range split.Branches {
		if _, ok := validation.GetValidated(branch.CandidateID); !ok {
			return false
		}
	}
	return true
}

func retireContextSplitParent(e *Engine, validation *memoryStructureValidationLedger, parentID string) error {
	if e == nil || validation == nil || strings.TrimSpace(parentID) == "" {
		return errors.New("context split parent unavailable")
	}
	validation.mu.Lock()
	for candidateID, structure := range validation.structures {
		if structure == nil || structure.ID != parentID {
			continue
		}
		structure.State = memoryStructureContextSplitState
		delete(validation.validated, candidateID)
		break
	}
	validation.mu.Unlock()
	root := memoryGrowthRoot(e)
	if _, _, err := root.resolveLocalFabricMemory(parentID); err == nil {
		if err := root.deleteExplicitMemoryBounded(parentID); err != nil {
			return err
		}
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func persistContextBranchingState(e *Engine, experiences *experienceLedger, formation *memoryStructureFormation, validation *memoryStructureValidationLedger) error {
	root := memoryGrowthRoot(e)
	if root == nil {
		return errors.New("context branching engine unavailable")
	}
	if err := PersistMemoryGrowthState(root, experiences, formation, validation); err != nil {
		return err
	}
	return root.persistAll()
}

func processExistingContextSplit(e *Engine, split *MemoryContextSplitState, experiences *experienceLedger, formation *memoryStructureFormation, validation *memoryStructureValidationLedger) (*ContextBranchingCycleResult, bool, error) {
	if split == nil || split.Status == memoryContextSplitValidated || split.Status == memoryContextSplitContested {
		return nil, false, nil
	}
	result := &ContextBranchingCycleResult{
		SplitID:           split.ID,
		ParentStructureID: split.ParentStructureID,
		ContextKey:        split.ContextKey,
	}
	belief, _, err := loadMemoryBeliefState(e, split.BeliefID)
	if err != nil {
		return result, true, err
	}
	if belief == nil || !contextSplitEvidenceStillSeparated(split, belief, experiences) {
		state, revision, loadErr := loadMemoryContextSplit(e, split.ID)
		if loadErr != nil {
			return result, true, loadErr
		}
		if state == nil {
			return result, true, errors.New("context split disappeared during contest update")
		}
		state.Status = memoryContextSplitContested
		state.UpdatedNano = time.Now().UnixNano()
		if err := upsertMemoryContextSplit(e, state, revision+1); err != nil {
			return result, true, err
		}
		if err := memoryGrowthRoot(e).persistAll(); err != nil {
			return result, true, err
		}
		result.Contested = true
		result.Persisted = true
		return result, true, nil
	}

	// 若上一次提交在“全部分支已验证”之后、父代退役之前中断，重启时从同一 Memory 状态收敛完成。
	if allContextSplitBranchesValidated(validation, split) {
		state, revision, loadErr := loadMemoryContextSplit(e, split.ID)
		if loadErr != nil {
			return result, true, loadErr
		}
		if state == nil {
			return result, true, errors.New("context split disappeared during completion recovery")
		}
		if err := retireContextSplitParent(e, validation, state.ParentStructureID); err != nil {
			return result, true, err
		}
		state.Status = memoryContextSplitValidated
		state.UpdatedNano = time.Now().UnixNano()
		if err := upsertMemoryContextSplit(e, state, revision+1); err != nil {
			return result, true, err
		}
		if err := persistContextBranchingState(e, experiences, formation, validation); err != nil {
			return result, true, err
		}
		result.ParentRetired = true
		result.Persisted = true
		return result, true, nil
	}

	for _, branch := range split.Branches {
		if _, ok := validation.GetValidated(branch.CandidateID); ok {
			continue
		}
		candidate, ok := formation.Get(branch.CandidateID)
		if !ok || candidate == nil || candidate.State != memoryStructureCandidateState {
			continue
		}
		witness := findIndependentContextBranchWitness(experiences, candidate)
		if witness == nil {
			continue
		}
		source, ok := experiences.Get(candidate.SourceExperienceIDs[0])
		if !ok || source == nil {
			return result, true, fmt.Errorf("context branch source experience missing: %s", candidate.SourceExperienceIDs[0])
		}
		validationFact, promoted, err := validation.ValidateCandidate(candidate, source, witness, 1)
		if err != nil {
			return result, true, err
		}
		if !validationFact.Success || promoted == nil {
			return result, true, errors.New("matching context branch witness failed deterministic validation")
		}
		if err := installValidatedContextBranch(e, validation, candidate, promoted); err != nil {
			return result, true, err
		}
		result.ValidatedCandidateID = candidate.ID
		result.ValidatedStructureID = promoted.ID

		state, revision, err := loadMemoryContextSplit(e, split.ID)
		if err != nil {
			return result, true, err
		}
		if state == nil {
			return result, true, errors.New("context split unavailable after branch validation")
		}
		state.Status = memoryContextSplitValidating
		state.UpdatedNano = time.Now().UnixNano()
		if allContextSplitBranchesValidated(validation, state) {
			if err := retireContextSplitParent(e, validation, state.ParentStructureID); err != nil {
				return result, true, err
			}
			state.Status = memoryContextSplitValidated
			result.ParentRetired = true
		}
		if err := upsertMemoryContextSplit(e, state, revision+1); err != nil {
			return result, true, err
		}
		if err := persistContextBranchingState(e, experiences, formation, validation); err != nil {
			return result, true, err
		}
		result.Persisted = true
		return result, true, nil
	}
	return result, false, nil
}

// RunAutonomousContextBranchingCycle 执行至多一个 Memory-native Context Split 生长步骤：
// 1) Belief 必须同时存在重复 support 与 conflict；
// 2) Context 键必须由重复现实事实完全区分不同 outcome；
// 3) 每个 branch candidate 用两条重复 Experience 形成，必须再取得第三条独立 Experience 才晋升；
// 4) 所有分支都验证后，粗粒度父 Structure 才退出 validated/executable 状态。
func RunAutonomousContextBranchingCycle(e *Engine) (*ContextBranchingCycleResult, error) {
	result := &ContextBranchingCycleResult{}
	root := memoryGrowthRoot(e)
	if root == nil {
		return result, errors.New("engine unavailable")
	}
	if !enterMemoryContextBranching(root) {
		result.Skipped = true
		return result, nil
	}
	defer leaveMemoryContextBranching(root)

	experiences, formation, validation, err := LoadMemoryGrowthState(root)
	if err != nil {
		return result, err
	}

	splits, err := listMemoryContextSplits(root)
	if err != nil {
		return result, err
	}
	for _, split := range splits {
		processed, handled, err := processExistingContextSplit(root, split, experiences, formation, validation)
		if err != nil {
			return processed, err
		}
		if handled {
			return processed, nil
		}
	}

	structures := validation.Snapshot()
	sort.Slice(structures, func(i, j int) bool { return structures[i].ID < structures[j].ID })
	for _, parent := range structures {
		if parent == nil || parent.State != memoryStructureValidatedState || len(parent.Program) == 0 {
			continue
		}
		beliefs, err := MemoryBeliefsForStructure(root, parent.ID)
		if err != nil {
			return result, err
		}
		for _, belief := range beliefs {
			proposal := proposeMemoryContextSplit(parent, belief, experiences)
			if proposal == nil {
				continue
			}
			if existing, _, err := loadMemoryContextSplit(root, proposal.ID); err != nil {
				return result, err
			} else if existing != nil {
				continue
			}
			if err := adoptContextSplitCandidates(formation, parent, proposal); err != nil {
				return result, err
			}
			if err := upsertMemoryContextSplit(root, proposal, 1); err != nil {
				return result, err
			}
			if err := persistContextBranchingState(root, experiences, formation, validation); err != nil {
				return result, err
			}
			candidateIDs := make([]string, 0, len(proposal.Branches))
			for _, branch := range proposal.Branches {
				candidateIDs = append(candidateIDs, branch.CandidateID)
			}
			result.SplitID = proposal.ID
			result.ParentStructureID = parent.ID
			result.ContextKey = proposal.ContextKey
			result.CandidateIDs = candidateIDs
			result.Persisted = true
			return result, nil
		}
	}

	result.Skipped = true
	return result, nil
}
