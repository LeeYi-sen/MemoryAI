package main

import (
	"errors"
	"fmt"
	"strings"
)

func failContextResolution(e *Engine, fact *MemoryContextActivationFact, status, detail string, candidates []string) (*ContextualExecutionResolution, error) {
	fact.Status = status
	fact.Detail = detail
	fact.CandidateIDs = cloneStrings(candidates)
	if err := persistContextActivationFact(e, fact); err != nil {
		return nil, err
	}
	return &ContextualExecutionResolution{RequestedID: fact.RequestedID, ActivationFactID: fact.ID, Contextual: true}, errors.New(detail)
}

// ResolveContextualExecutionTarget 将一个粗粒度/上下文 Structure ID 沿已经独立验证的 split 逐层解析为唯一 leaf。
// Kernel 只做 exact condition equality；缺失、无分支或多分支同时可用时一律记录事实并拒绝猜测。
func ResolveContextualExecutionTarget(e *Engine, requestedID string, context map[string]string) (*ContextualExecutionResolution, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("context activation engine unavailable")
	}
	requestedID = strings.TrimSpace(requestedID)
	if requestedID == "" {
		return nil, errors.New("context activation requires requested Memory id")
	}
	experiences, formation, validation, err := LoadMemoryGrowthState(root)
	if err != nil {
		return nil, err
	}

	// 非 Context Structure/tag 继续走既有执行路径，不把物理 tag 解析偷换成认知选择。
	initialSplits, err := validatedContextSplitsForParent(root, requestedID)
	if err != nil {
		return nil, err
	}
	initialStructure := findValidatedStructureByID(validation, requestedID)
	leafConditions := map[string]string{}
	leafChain := []string(nil)
	if len(initialSplits) == 0 && initialStructure != nil {
		leafConditions, leafChain, err = exactContextConditionsForLeaf(root, validation, initialStructure)
		if err != nil {
			return nil, err
		}
	}
	if len(initialSplits) == 0 && len(leafConditions) == 0 {
		return &ContextualExecutionResolution{RequestedID: requestedID, ResolvedID: requestedID}, nil
	}

	fact := newContextActivationFact(requestedID, memoryContextActivationMatched, context)
	conditions := map[string]string{}
	traversal := []string{requestedID}
	currentID := requestedID
	currentStructure := initialStructure
	visited := map[string]bool{}
	contextual := false

	for {
		if visited[currentID] {
			fact.Traversal = traversal
			return failContextResolution(root, fact, memoryContextActivationCycleDetected, "validated context split cycle detected at "+currentID, traversal)
		}
		visited[currentID] = true
		splits, err := validatedContextSplitsForParent(root, currentID)
		if err != nil {
			return nil, err
		}
		if len(splits) > 1 {
			ids := make([]string, 0, len(splits))
			for _, split := range splits {
				ids = append(ids, split.ID)
			}
			fact.Traversal = traversal
			return failContextResolution(root, fact, memoryContextActivationAmbiguous, "multiple validated context splits exist for "+currentID, ids)
		}
		if len(splits) == 0 {
			break
		}
		contextual = true
		split := splits[0]
		value, ok := context[split.ContextKey]
		if !ok {
			candidates := make([]string, 0, len(split.Branches))
			for _, branch := range split.Branches {
				candidates = append(candidates, branch.CandidateID)
			}
			fact.Traversal = traversal
			fact.MatchedConditions = cloneStringMap(conditions)
			return failContextResolution(root, fact, memoryContextActivationMissing, "missing required context key "+split.ContextKey, candidates)
		}
		matches := make([]*MemoryStructure, 0, 1)
		candidateIDs := make([]string, 0, 1)
		for _, branch := range split.Branches {
			if branch.ContextValue != value {
				continue
			}
			child, childErr := contextBranchStructure(root, validation, branch)
			if childErr != nil {
				return nil, childErr
			}
			if child != nil {
				matches = append(matches, child)
				candidateIDs = append(candidateIDs, branch.CandidateID)
			}
		}
		if len(matches) == 0 {
			fact.Traversal = traversal
			fact.MatchedConditions = cloneStringMap(conditions)
			return failContextResolution(root, fact, memoryContextActivationNoBranch, fmt.Sprintf("no validated context branch for %s=%s", split.ContextKey, value), candidateIDs)
		}
		if len(matches) > 1 {
			fact.Traversal = traversal
			fact.MatchedConditions = cloneStringMap(conditions)
			return failContextResolution(root, fact, memoryContextActivationAmbiguous, fmt.Sprintf("multiple validated branches match %s=%s", split.ContextKey, value), candidateIDs)
		}
		conditions[split.ContextKey] = value
		currentStructure = matches[0]
		currentID = currentStructure.ID
		traversal = append(traversal, currentID)
	}

	if currentStructure == nil {
		currentStructure = findValidatedStructureByID(validation, currentID)
	}
	if currentStructure == nil {
		fact.Traversal = traversal
		return failContextResolution(root, fact, memoryContextActivationNoBranch, "resolved contextual structure is not validated: "+currentID, nil)
	}
	inherited, _, err := exactContextConditionsForLeaf(root, validation, currentStructure)
	if err != nil {
		return nil, err
	}
	for key, value := range inherited {
		if previous, exists := conditions[key]; exists && previous != value {
			fact.Traversal = traversal
			return failContextResolution(root, fact, memoryContextActivationAmbiguous, "context lineage disagrees on key "+key, nil)
		}
		conditions[key] = value
	}
	if len(leafChain) > 0 && !contextual {
		traversal = append(traversal[:0], leafChain...)
		contextual = true
	}
	if key, actual, ok := validateExactContext(conditions, context); !ok {
		fact.Traversal = traversal
		fact.MatchedConditions = cloneStringMap(conditions)
		if actual == "" {
			return failContextResolution(root, fact, memoryContextActivationMissing, "missing required context key "+key, nil)
		}
		return failContextResolution(root, fact, memoryContextActivationMismatch, fmt.Sprintf("context mismatch %s: expected=%s actual=%s", key, conditions[key], actual), nil)
	}

	updatedStructure, refreshed := refreshContextualGroundedActionIdentity(validation, currentStructure)
	if refreshed {
		currentStructure = updatedStructure
		if err := PersistMemoryGrowthState(root, experiences, formation, validation); err != nil {
			return nil, err
		}
	}
	if err := materializeContextConditionsOnExecutable(root, currentStructure, requestedID, conditions); err != nil {
		return nil, err
	}
	fact.ResolvedStructureID = currentStructure.ID
	fact.MatchedConditions = cloneStringMap(conditions)
	fact.Traversal = traversal
	fact.Status = memoryContextActivationMatched
	if err := persistContextActivationFact(root, fact); err != nil {
		return nil, err
	}
	return &ContextualExecutionResolution{
		RequestedID:         requestedID,
		ResolvedID:          currentStructure.ID,
		Structure:           cloneMemoryStructure(currentStructure),
		Conditions:          cloneStringMap(conditions),
		ActivationFactID:    fact.ID,
		Contextual:          contextual,
		GroundedActionFresh: refreshed,
	}, nil
}
