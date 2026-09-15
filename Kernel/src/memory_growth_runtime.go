package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

const (
	memoryGrowthKindRecombination       = "recombination"
	memoryGrowthProbeOperation          = "recombination_probe"
	memoryStructureRealityRejectedState = "reality_rejected"
)

// MemoryGrowthCycleResult 是一次由真实 live 活动触发的 Memory 自生长事实摘要。
// 它只报告发生了什么，不包含语义评分、目标或奖励函数。
type MemoryGrowthCycleResult struct {
	CandidateID         string `json:"candidate_id,omitempty"`
	WitnessExperienceID string `json:"witness_experience_id,omitempty"`
	PromotedStructureID string `json:"promoted_structure_id,omitempty"`
	ValidationSuccess   bool   `json:"validation_success,omitempty"`
	RealityRejected     bool   `json:"reality_rejected,omitempty"`
	RecordedFailure     bool   `json:"recorded_failure,omitempty"`
	Persisted           bool   `json:"persisted,omitempty"`
	Skipped             bool   `json:"skipped,omitempty"`
}

// memoryGrowthCycleActive 只是 live 回调的物理重入保护，不保存任何认知状态。
// 真正的生长历史仍全部保存在 memory.mem 的 Growth Memory 中。
var memoryGrowthCycleActive sync.Map

func memoryGrowthRoot(e *Engine) *Engine {
	if e == nil {
		return nil
	}
	if root := fabricRootFor(e); root != nil {
		return root
	}
	return e
}

func enterMemoryGrowthCycle(e *Engine) bool {
	root := memoryGrowthRoot(e)
	if root == nil {
		return false
	}
	_, loaded := memoryGrowthCycleActive.LoadOrStore(root, struct{}{})
	return !loaded
}

func leaveMemoryGrowthCycle(e *Engine) {
	if root := memoryGrowthRoot(e); root != nil {
		memoryGrowthCycleActive.Delete(root)
	}
}

func recombinationPairKey(leftID, rightID string) string {
	return strings.TrimSpace(leftID) + "=>" + strings.TrimSpace(rightID)
}

func hasRecombinationPairExperience(experiences []*MemoryExperience, pairKey string) bool {
	for _, experience := range experiences {
		if experience == nil {
			continue
		}
		if experience.Observation["memory_growth_kind"] != memoryGrowthKindRecombination {
			continue
		}
		if experience.Observation["memory_growth_pair"] == pairKey {
			return true
		}
	}
	return false
}

func firstExistingExperienceID(experiences *experienceLedger, ids []string) string {
	for _, id := range ids {
		if _, ok := experiences.Get(id); ok {
			return id
		}
	}
	return ""
}

func findCandidateSourceExperience(experiences *experienceLedger, candidate *MemoryStructureCandidate) (*MemoryExperience, error) {
	if experiences == nil || candidate == nil {
		return nil, errors.New("candidate source lookup unavailable")
	}
	for _, id := range candidate.SourceExperienceIDs {
		experience, ok := experiences.Get(id)
		if !ok || experience == nil {
			continue
		}
		if memoryStructurePatternHash(*experience) == candidate.PatternHash {
			return experience, nil
		}
	}
	return nil, fmt.Errorf("candidate %s has no matching source experience", candidate.ID)
}

func selectPendingRecombinationCandidate(formation *memoryStructureFormation, validation *memoryStructureValidationLedger) *MemoryStructureCandidate {
	if formation == nil || validation == nil {
		return nil
	}
	type pending struct {
		candidate *MemoryStructureCandidate
		attempts  int
	}
	var selected *pending
	for _, candidate := range formation.Snapshot() {
		if candidate == nil || candidate.State != memoryStructureCandidateState {
			continue
		}
		if len(candidate.ParentStructureIDs) != 2 || len(candidate.Program) == 0 {
			continue
		}
		if _, ok := validation.GetValidated(candidate.ID); ok {
			continue
		}
		item := &pending{candidate: candidate, attempts: len(validation.ValidationHistory(candidate.ID))}
		if selected == nil || item.attempts < selected.attempts || (item.attempts == selected.attempts && item.candidate.ID < selected.candidate.ID) {
			selected = item
		}
	}
	if selected == nil {
		return nil
	}
	return selected.candidate
}

// rejectRecombinationCandidate 将一次独立现实否决固化到候选本身。
// 失败证据不会删除；候选只是退出自动复验队列，避免相同 live 条件下无限机械重试。
func rejectRecombinationCandidate(formation *memoryStructureFormation, candidateID string) error {
	if formation == nil {
		return errors.New("memory structure formation unavailable")
	}
	candidateID = strings.TrimSpace(candidateID)
	if candidateID == "" {
		return errors.New("memory structure candidate id unavailable")
	}
	formation.mu.Lock()
	defer formation.mu.Unlock()
	candidate := formation.candidates[candidateID]
	if candidate == nil {
		return fmt.Errorf("memory structure candidate not found: %s", candidateID)
	}
	if candidate.State != memoryStructureCandidateState {
		return fmt.Errorf("memory structure candidate is not pending: %s", candidate.State)
	}
	candidate.State = memoryStructureRealityRejectedState
	return nil
}

func eligibleRecombinationParent(structure *MemoryStructure, requireOutcome bool) bool {
	if structure == nil || structure.State != memoryStructureValidatedState || len(structure.Program) < 2 {
		return false
	}
	if requireOutcome && len(structure.ExpectedOutcome) == 0 {
		return false
	}
	return strings.TrimSpace(structure.ID) != ""
}

// latestSuccessfulRecombinationFrontier 从已经写入 Memory 的 Reality Validation 历史中
// 找到最近一次真实验证成功的新生 Structure。这里没有权重或语义评分，只有“已验证成功”事实和时间顺序。
func latestSuccessfulRecombinationFrontier(validation *memoryStructureValidationLedger) *MemoryStructure {
	if validation == nil {
		return nil
	}
	var selected *MemoryStructure
	var selectedNano int64
	for _, structure := range validation.Snapshot() {
		if structure == nil || len(structure.ParentStructureIDs) != 2 || strings.TrimSpace(structure.CandidateID) == "" {
			continue
		}
		for _, fact := range validation.ValidationHistory(structure.CandidateID) {
			if !fact.Success {
				continue
			}
			if selected == nil || fact.CreatedNano > selectedNano || (fact.CreatedNano == selectedNano && structure.ID < selected.ID) {
				selected = structure
				selectedNano = fact.CreatedNano
			}
		}
	}
	return cloneMemoryStructure(selected)
}

// selectUnattemptedRecombinationParents 只使用 Memory 已经持有的血缘、成功验证和尝试历史。
// 若存在刚被现实验证成功的新生 Structure，优先让该生长前沿参与下一代尚未尝试的组合；
// 当前沿没有可用组合时，退回稳定 ID 顺序遍历。没有加权分数、奖励函数或语义排名。
func selectUnattemptedRecombinationParents(validation *memoryStructureValidationLedger, experiences *experienceLedger) (*MemoryStructure, *MemoryStructure, string) {
	if validation == nil || experiences == nil {
		return nil, nil, ""
	}
	structures := validation.Snapshot()
	sort.Slice(structures, func(i, j int) bool { return structures[i].ID < structures[j].ID })
	history := experiences.Snapshot()

	if frontier := latestSuccessfulRecombinationFrontier(validation); eligibleRecombinationParent(frontier, true) {
		for _, right := range structures {
			if right == nil || right.ID == frontier.ID || !eligibleRecombinationParent(right, false) {
				continue
			}
			pairKey := recombinationPairKey(frontier.ID, right.ID)
			if hasRecombinationPairExperience(history, pairKey) {
				continue
			}
			return frontier, right, pairKey
		}
	}

	for i, left := range structures {
		if !eligibleRecombinationParent(left, true) {
			continue
		}
		for j, right := range structures {
			if i == j || !eligibleRecombinationParent(right, false) {
				continue
			}
			pairKey := recombinationPairKey(left.ID, right.ID)
			if hasRecombinationPairExperience(history, pairKey) {
				continue
			}
			return left, right, pairKey
		}
	}
	return nil, nil, ""
}

func probeMemoryFromCandidate(candidate *MemoryStructureCandidate, context map[string]string) (*Memory, error) {
	if candidate == nil || strings.TrimSpace(candidate.ID) == "" || len(candidate.Program) == 0 {
		return nil, errors.New("recombination probe candidate unavailable")
	}
	state := cloneStringMap(context)
	if state == nil {
		state = map[string]string{}
	}
	state["memory_growth_candidate_id"] = candidate.ID
	return &Memory{
		ID:      candidate.ID,
		Layer:   "emergent",
		Tags:    []string{"memory", "memory-growth-probe"},
		State:   stringMapToAny(state),
		Program: append([]Op(nil), candidate.Program...),
	}, nil
}

func executeRecombinationProbe(
	e *Engine,
	candidate *MemoryStructureCandidate,
	experiences *experienceLedger,
	context map[string]string,
	observation map[string]string,
	action map[string]string,
	expectedOutcome map[string]string,
	predictionHash string,
	pairKey string,
	parentExperienceID string,
) (*MemoryExperience, error) {
	if e == nil || experiences == nil {
		return nil, errors.New("recombination probe runtime unavailable")
	}
	if len(expectedOutcome) == 0 {
		return nil, errors.New("recombination probe requires an observable expected outcome")
	}
	probe, err := probeMemoryFromCandidate(candidate, context)
	if err != nil {
		return nil, err
	}
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("memory growth root unavailable")
	}
	if _, existing, resolveErr := root.resolveLocalFabricMemory(probe.ID); resolveErr == nil {
		if existing == nil || !memoryHasTag(existing, "memory-growth-probe") {
			return nil, fmt.Errorf("recombination probe id collides with existing Memory: %s", probe.ID)
		}
		if err := root.deleteExplicitMemoryBounded(probe.ID); err != nil {
			return nil, err
		}
	} else if !errors.Is(resolveErr, io.EOF) {
		return nil, resolveErr
	}
	if err := root.upsertExplicitMemoryBounded(probe); err != nil {
		return nil, err
	}

	frame := newFrame()
	runErr := globalTxnScheduler.canonical(root, probe.ID, frame)
	deleteErr := root.deleteExplicitMemoryBounded(probe.ID)
	if runErr != nil {
		if deleteErr != nil {
			return nil, fmt.Errorf("probe execution failed: %v; cleanup failed: %w", runErr, deleteErr)
		}
		return nil, fmt.Errorf("probe execution failed: %w", runErr)
	}
	if deleteErr != nil {
		return nil, fmt.Errorf("probe cleanup failed: %w", deleteErr)
	}

	actual := captureActualOutcome(frame.Vars, expectedOutcome)
	predictionError := calculatePredictionError(expectedOutcome, actual)
	obs := cloneStringMap(observation)
	if obs == nil {
		obs = map[string]string{}
	}
	obs["memory_growth_kind"] = memoryGrowthKindRecombination
	obs["memory_growth_pair"] = pairKey
	obs["memory_growth_candidate_id"] = candidate.ID
	act := cloneStringMap(action)
	if act == nil {
		act = map[string]string{}
	}
	act["memory_growth_operation"] = memoryGrowthProbeOperation

	parentIDs := []string(nil)
	if strings.TrimSpace(parentExperienceID) != "" {
		if _, ok := experiences.Get(parentExperienceID); !ok {
			return nil, fmt.Errorf("recombination probe parent experience not found: %s", parentExperienceID)
		}
		parentIDs = []string{parentExperienceID}
	}
	return experiences.Record(MemoryExperience{
		ParentIDs:       parentIDs,
		Context:         cloneStringMap(context),
		Observation:     obs,
		Action:          act,
		Outcome:         actual,
		PredictionHash:  strings.TrimSpace(predictionHash),
		PredictionError: predictionError,
	})
}

func recordRecombinationFailure(experiences *experienceLedger, pairKey, candidateID, parentExperienceID, reason string) (*MemoryExperience, error) {
	if experiences == nil {
		return nil, errors.New("experience ledger unavailable")
	}
	parentIDs := []string(nil)
	if strings.TrimSpace(parentExperienceID) != "" {
		if _, ok := experiences.Get(parentExperienceID); ok {
			parentIDs = []string{parentExperienceID}
		}
	}
	observation := map[string]string{
		"memory_growth_kind": memoryGrowthKindRecombination,
		"memory_growth_pair": pairKey,
	}
	if strings.TrimSpace(candidateID) != "" {
		observation["memory_growth_candidate_id"] = candidateID
	}
	return experiences.Record(MemoryExperience{
		ParentIDs:   parentIDs,
		Observation: observation,
		Action:      map[string]string{"memory_growth_operation": memoryGrowthProbeOperation},
		Outcome: map[string]string{
			"status": "execution_error",
			"reason": strings.TrimSpace(reason),
		},
	})
}

func commitFinalizedRecombinedStructure(e *Engine, validation *memoryStructureValidationLedger, candidate *MemoryStructureCandidate, structure *MemoryStructure) (*MemoryStructure, error) {
	finalized, err := FinalizeRecombinedStructure(structure, candidate)
	if err != nil {
		return nil, err
	}
	executable, err := ExecutableMemory(finalized)
	if err != nil {
		return nil, err
	}
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("memory growth root unavailable")
	}
	if err := root.upsertExplicitMemoryBounded(executable); err != nil {
		return nil, err
	}
	validation.mu.Lock()
	validation.structures[candidate.ID] = cloneMemoryStructure(finalized)
	validation.validated[candidate.ID] = cloneMemoryStructure(finalized)
	validation.mu.Unlock()
	return finalized, nil
}

func persistMemoryGrowthCycle(e *Engine, experiences *experienceLedger, formation *memoryStructureFormation, validation *memoryStructureValidationLedger) error {
	root := memoryGrowthRoot(e)
	if root == nil {
		return errors.New("memory growth root unavailable")
	}
	if err := PersistMemoryGrowthState(root, experiences, formation, validation); err != nil {
		return err
	}
	return root.persistAll()
}

// RunAutonomousMemoryGrowthCycle 执行一次 Memory-native 自生长闭环。
// 它由真实 live 活动触发，不创建独立后台调度器：
// 1) 优先让已经 materialized 的重组候选再次进入真实 VM，形成独立 witness；
// 2) 独立现实失败会保留证据并暂停该候选，避免无限机械复验；
// 3) 没有待复验候选时，优先延续最近一次现实验证成功的新生 Structure 血缘；
// 4) 所有成功/失败事实回写 Experience、Validation 和 memory.mem。
func RunAutonomousMemoryGrowthCycle(e *Engine) (*MemoryGrowthCycleResult, error) {
	result := &MemoryGrowthCycleResult{}
	root := memoryGrowthRoot(e)
	if root == nil {
		return result, errors.New("engine unavailable")
	}
	if !enterMemoryGrowthCycle(root) {
		result.Skipped = true
		return result, nil
	}
	defer leaveMemoryGrowthCycle(root)

	experiences, formation, validation, err := LoadMemoryGrowthState(root)
	if err != nil {
		return result, err
	}

	if candidate := selectPendingRecombinationCandidate(formation, validation); candidate != nil {
		result.CandidateID = candidate.ID
		source, err := findCandidateSourceExperience(experiences, candidate)
		if err != nil {
			return result, err
		}
		pairKey := source.Observation["memory_growth_pair"]
		if strings.TrimSpace(pairKey) == "" && len(candidate.ParentStructureIDs) == 2 {
			pairKey = recombinationPairKey(candidate.ParentStructureIDs[0], candidate.ParentStructureIDs[1])
		}
		witness, probeErr := executeRecombinationProbe(
			root,
			candidate,
			experiences,
			source.Context,
			source.Observation,
			source.Action,
			source.Outcome,
			source.PredictionHash,
			pairKey,
			source.ID,
		)
		if probeErr != nil {
			witness, err = recordRecombinationFailure(experiences, pairKey, candidate.ID, source.ID, probeErr.Error())
			if err != nil {
				return result, err
			}
			result.RecordedFailure = true
		}
		result.WitnessExperienceID = witness.ID
		validationFact, promoted, err := validation.ValidateCandidate(candidate, source, witness, 1)
		if err != nil {
			return result, err
		}
		result.ValidationSuccess = validationFact.Success
		if !validationFact.Success {
			if err := rejectRecombinationCandidate(formation, candidate.ID); err != nil {
				return result, err
			}
			result.RealityRejected = true
		}
		if promoted != nil {
			finalized, err := commitFinalizedRecombinedStructure(root, validation, candidate, promoted)
			if err != nil {
				return result, err
			}
			result.PromotedStructureID = finalized.ID
		}
		if err := persistMemoryGrowthCycle(root, experiences, formation, validation); err != nil {
			return result, err
		}
		result.Persisted = true
		return result, nil
	}

	left, right, pairKey := selectUnattemptedRecombinationParents(validation, experiences)
	if left == nil || right == nil {
		result.Skipped = true
		return result, nil
	}
	candidate, err := RecombineValidatedStructures(left, right)
	if err != nil {
		if _, recordErr := recordRecombinationFailure(experiences, pairKey, "", "", err.Error()); recordErr != nil {
			return result, recordErr
		}
		result.RecordedFailure = true
		if err := persistMemoryGrowthCycle(root, experiences, formation, validation); err != nil {
			return result, err
		}
		result.Persisted = true
		return result, nil
	}
	result.CandidateID = candidate.ID
	parentExperienceID := firstExistingExperienceID(experiences, left.SourceExperienceIDs)
	firstProbe, probeErr := executeRecombinationProbe(
		root,
		candidate,
		experiences,
		left.Context,
		left.Observation,
		left.Action,
		left.ExpectedOutcome,
		left.PredictionHash,
		pairKey,
		parentExperienceID,
	)
	if probeErr != nil {
		if _, recordErr := recordRecombinationFailure(experiences, pairKey, candidate.ID, parentExperienceID, probeErr.Error()); recordErr != nil {
			return result, recordErr
		}
		result.RecordedFailure = true
		if err := persistMemoryGrowthCycle(root, experiences, formation, validation); err != nil {
			return result, err
		}
		result.Persisted = true
		return result, nil
	}
	materialized, err := MaterializeRecombinationCandidate(candidate, firstProbe)
	if err != nil {
		return result, err
	}
	if err := formation.AdoptCandidate(materialized); err != nil {
		return result, err
	}
	result.CandidateID = materialized.ID
	result.WitnessExperienceID = firstProbe.ID
	if err := persistMemoryGrowthCycle(root, experiences, formation, validation); err != nil {
		return result, err
	}
	result.Persisted = true
	return result, nil
}
