package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func findValidatedStructureByID(validation *memoryStructureValidationLedger, structureID string) *MemoryStructure {
	if validation == nil || strings.TrimSpace(structureID) == "" {
		return nil
	}
	validation.mu.RLock()
	defer validation.mu.RUnlock()
	for _, structure := range validation.validated {
		if structure != nil && structure.ID == structureID {
			return cloneMemoryStructure(structure)
		}
	}
	return nil
}

func findHistoricalStructureByID(validation *memoryStructureValidationLedger, structureID string) *MemoryStructure {
	if validation == nil || strings.TrimSpace(structureID) == "" {
		return nil
	}
	validation.mu.RLock()
	defer validation.mu.RUnlock()
	for _, structure := range validation.structures {
		if structure != nil && structure.ID == structureID {
			return cloneMemoryStructure(structure)
		}
	}
	return nil
}

func validatedContextSplitsForParent(e *Engine, parentID string) ([]*MemoryContextSplitState, error) {
	root := memoryGrowthRoot(e)
	if root == nil {
		return nil, errors.New("context activation engine unavailable")
	}
	ids, err := root.listTagFabric(memoryContextSplitParentTagPrefix + strings.TrimSpace(parentID))
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
		if state != nil && state.Status == memoryContextSplitValidated {
			out = append(out, state)
		}
	}
	return out, nil
}

func contextBranchStructure(e *Engine, validation *memoryStructureValidationLedger, branch MemoryContextBranchEvidence) (*MemoryStructure, error) {
	if validation == nil {
		return nil, nil
	}
	if structure, ok := validation.GetValidated(branch.CandidateID); ok && structure != nil && structure.State == memoryStructureValidatedState {
		return structure, nil
	}
	validation.mu.RLock()
	historical := cloneMemoryStructure(validation.structures[branch.CandidateID])
	validation.mu.RUnlock()
	if historical == nil || historical.State != memoryStructureContextSplitState {
		return nil, nil
	}
	// 已经继续裂分的子 Structure 可以作为纯血缘/Context 路由中间节点；它本身不会被执行。
	splits, err := validatedContextSplitsForParent(e, historical.ID)
	if err != nil {
		return nil, err
	}
	if len(splits) == 0 {
		return nil, nil
	}
	return historical, nil
}

func exactContextConditionsForLeaf(e *Engine, validation *memoryStructureValidationLedger, leaf *MemoryStructure) (map[string]string, []string, error) {
	conditions := map[string]string{}
	if leaf == nil {
		return conditions, nil, nil
	}
	current := leaf
	chain := []string{leaf.ID}
	visited := map[string]bool{leaf.ID: true}
	for len(current.ParentStructureIDs) == 1 {
		parentID := current.ParentStructureIDs[0]
		if parentID == "" || visited[parentID] {
			break
		}
		visited[parentID] = true
		splits, err := validatedContextSplitsForParent(e, parentID)
		if err != nil {
			return nil, nil, err
		}
		var matched *MemoryContextSplitState
		var contextValue string
		for _, split := range splits {
			for _, branch := range split.Branches {
				if branch.CandidateID == current.CandidateID {
					if matched != nil {
						return nil, nil, fmt.Errorf("context structure %s belongs to multiple validated parent splits", current.ID)
					}
					matched = split
					contextValue = branch.ContextValue
				}
			}
		}
		if matched == nil {
			break
		}
		if previous, exists := conditions[matched.ContextKey]; exists && previous != contextValue {
			return nil, nil, fmt.Errorf("context lineage condition conflict for %s", matched.ContextKey)
		}
		conditions[matched.ContextKey] = contextValue
		chain = append(chain, parentID)
		parent := findHistoricalStructureByID(validation, parentID)
		if parent == nil {
			break
		}
		current = parent
	}
	return conditions, chain, nil
}

func contextualGroundedActionID(parentActionID, structureID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(parentActionID) + "\x00" + strings.TrimSpace(structureID)))
	return "context-action-" + hex.EncodeToString(sum[:16])
}

// refreshContextualGroundedActionIdentity 只更新物理幂等身份，不改变动作语义、adapter 或 autonomous 授权。
// 若 Context 子结构继承了父代已消费的 action id，则为该已验证子结构生成稳定的新物理试验身份。
func refreshContextualGroundedActionIdentity(validation *memoryStructureValidationLedger, structure *MemoryStructure) (*MemoryStructure, bool) {
	if validation == nil || structure == nil || len(structure.ParentStructureIDs) != 1 || structure.Action == nil {
		return structure, false
	}
	if !groundedAutonomousEnabled(structure.Action[groundedAutonomousActionKey]) {
		return structure, false
	}
	currentActionID := strings.TrimSpace(structure.Action[groundedActionIDKey])
	if currentActionID == "" || strings.TrimSpace(structure.Action[groundedParentActionIDKey]) != "" {
		return structure, false
	}
	parent := findHistoricalStructureByID(validation, structure.ParentStructureIDs[0])
	if parent == nil || parent.Action == nil {
		return structure, false
	}
	parentActionID := strings.TrimSpace(parent.Action[groundedActionIDKey])
	if parentActionID == "" || parentActionID != currentActionID {
		return structure, false
	}
	updated := cloneMemoryStructure(structure)
	updated.Action[groundedParentActionIDKey] = parentActionID
	updated.Action[groundedActionIDKey] = contextualGroundedActionID(parentActionID, updated.ID)
	candidateID := candidateIDFromStructure(updated)
	validation.mu.Lock()
	validation.structures[candidateID] = cloneMemoryStructure(updated)
	validation.validated[candidateID] = cloneMemoryStructure(updated)
	validation.mu.Unlock()
	return updated, true
}

func materializeContextConditionsOnExecutable(e *Engine, structure *MemoryStructure, rootID string, conditions map[string]string) error {
	if structure == nil || len(conditions) == 0 {
		return nil
	}
	root := memoryGrowthRoot(e)
	if root == nil {
		return errors.New("context activation engine unavailable")
	}
	_, executable, err := root.resolveLocalFabricMemory(structure.ID)
	if err != nil {
		return err
	}
	if executable == nil || !memoryHasTag(executable, "memory-structure") {
		return fmt.Errorf("contextual executable Memory unavailable: %s", structure.ID)
	}
	updated := copyMemory(executable)
	if updated.State == nil {
		updated.State = map[string]any{}
	}
	changed := false
	for key, value := range conditions {
		stateKey := memoryContextConditionStatePrefix + key
		if fmt.Sprint(updated.State[stateKey]) != value {
			updated.State[stateKey] = value
			changed = true
		}
	}
	if fmt.Sprint(updated.State[memoryContextActivationRootStateKey]) != rootID {
		updated.State[memoryContextActivationRootStateKey] = rootID
		changed = true
	}
	if fmt.Sprint(updated.State[memoryContextActivationLeafStateKey]) != structure.ID {
		updated.State[memoryContextActivationLeafStateKey] = structure.ID
		changed = true
	}
	if !memoryHasTag(updated, "memory-contextual-structure") {
		updated.Tags = append(updated.Tags, "memory-contextual-structure")
		sort.Strings(updated.Tags)
		changed = true
	}
	if !changed {
		return nil
	}
	updated.Revision++
	return root.upsertExplicitMemoryBounded(updated)
}

func validateExactContext(conditions, context map[string]string) (string, string, bool) {
	keys := make([]string, 0, len(conditions))
	for key := range conditions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		expected := conditions[key]
		actual, ok := context[key]
		if !ok {
			return key, "", false
		}
		if actual != expected {
			return key, actual, false
		}
	}
	return "", "", true
}
