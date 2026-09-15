package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	memoryContextActivationFactTag       = "memory-context-activation-fact"
	memoryContextConditionStatePrefix    = "memory_context_condition:"
	memoryContextActivationRootStateKey  = "memory_context_activation_root"
	memoryContextActivationLeafStateKey  = "memory_context_activation_leaf"
	groundedParentActionIDKey            = "grounded_parent_action_id"
	memoryContextActivationMatched       = "matched"
	memoryContextActivationMissing       = "missing_context"
	memoryContextActivationNoBranch      = "no_branch"
	memoryContextActivationAmbiguous     = "ambiguous"
	memoryContextActivationMismatch      = "condition_mismatch"
	memoryContextActivationCycleDetected = "cycle_detected"
)

// MemoryContextActivationFact 是一次 Context 执行解析的事实记录。
// 它只保存请求 Context、精确匹配链和失败原因，不计算相关性、优先级或语义分数。
type MemoryContextActivationFact struct {
	ID                  string            `json:"id"`
	RequestedID         string            `json:"requested_id"`
	ResolvedStructureID string            `json:"resolved_structure_id,omitempty"`
	Context             map[string]string `json:"context,omitempty"`
	MatchedConditions   map[string]string `json:"matched_conditions,omitempty"`
	Traversal           []string          `json:"traversal,omitempty"`
	CandidateIDs        []string          `json:"candidate_ids,omitempty"`
	Status              string            `json:"status"`
	Detail              string            `json:"detail,omitempty"`
	CreatedNano         int64             `json:"created_nano"`
}

// ContextualExecutionResolution 报告一次精确 Context 解析结果。
type ContextualExecutionResolution struct {
	RequestedID         string
	ResolvedID          string
	Structure           *MemoryStructure
	Conditions          map[string]string
	ActivationFactID    string
	Contextual          bool
	GroundedActionFresh bool
}

func cloneContextActivationFact(src *MemoryContextActivationFact) *MemoryContextActivationFact {
	if src == nil {
		return nil
	}
	out := *src
	out.Context = cloneStringMap(src.Context)
	out.MatchedConditions = cloneStringMap(src.MatchedConditions)
	out.Traversal = append([]string(nil), src.Traversal...)
	out.CandidateIDs = cloneStrings(src.CandidateIDs)
	return &out
}

func contextActivationFactID(requestedID string, createdNano int64, context map[string]string) string {
	keys := make([]string, 0, len(context))
	for key := range context {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(strings.TrimSpace(requestedID))
	b.WriteByte(0)
	b.WriteString(fmt.Sprint(createdNano))
	for _, key := range keys {
		b.WriteByte(0)
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(context[key])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "memory-context-activation-fact-" + hex.EncodeToString(sum[:16])
}

func memoryContextActivationFactMemory(fact *MemoryContextActivationFact) (*Memory, error) {
	if fact == nil || strings.TrimSpace(fact.ID) == "" {
		return nil, errors.New("context activation fact unavailable")
	}
	raw, err := json.Marshal(fact)
	if err != nil {
		return nil, err
	}
	return &Memory{
		ID:       fact.ID,
		Layer:    "emergent",
		Tags:     []string{"memory", memoryContextActivationFactTag},
		Content:  "Memory-native exact contextual execution fact.",
		Revision: 1,
		State: map[string]any{
			"requested_id":    fact.RequestedID,
			"resolved_id":     fact.ResolvedStructureID,
			"status":          fact.Status,
			"activation_fact": string(raw),
		},
	}, nil
}

func persistContextActivationFact(e *Engine, fact *MemoryContextActivationFact) error {
	root := memoryGrowthRoot(e)
	if root == nil {
		return errors.New("context activation engine unavailable")
	}
	memory, err := memoryContextActivationFactMemory(fact)
	if err != nil {
		return err
	}
	if err := root.upsertExplicitMemoryBounded(memory); err != nil {
		return err
	}
	return root.persistAll()
}

func newContextActivationFact(requestedID, status string, context map[string]string) *MemoryContextActivationFact {
	now := time.Now().UnixNano()
	return &MemoryContextActivationFact{
		ID:          contextActivationFactID(requestedID, now, context),
		RequestedID: strings.TrimSpace(requestedID),
		Context:     cloneStringMap(context),
		Status:      status,
		CreatedNano: now,
	}
}
