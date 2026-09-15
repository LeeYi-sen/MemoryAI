package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const memoryStructureRecombinationCandidateState = "recombination_candidate"

// RecombineValidatedStructures 从两个已验证结构的真实 Program 中做确定性的单点重组。
// 不猜测语义；只在已有 VM Op 边界上交换一个非终止 Op，结果必须进入后续现实执行与验证。
func RecombineValidatedStructures(left, right *MemoryStructure) (*MemoryStructureCandidate, error) {
	if left == nil || right == nil {
		return nil, errors.New("parent memory structure unavailable")
	}
	if left.State != memoryStructureValidatedState || right.State != memoryStructureValidatedState {
		return nil, errors.New("both parent memory structures must be validated")
	}
	if strings.TrimSpace(left.ID) == "" || strings.TrimSpace(right.ID) == "" {
		return nil, errors.New("parent memory structure id unavailable")
	}
	if len(left.Program) == 0 || len(right.Program) == 0 {
		return nil, errors.New("both parent memory structures need executable programs")
	}
	limit := len(left.Program)
	if len(right.Program) < limit {
		limit = len(right.Program)
	}
	if limit < 2 {
		return nil, errors.New("recombination requires at least two executable operations")
	}
	index := recombinationIndex(left.ID, right.ID, limit-1)
	if sameOp(left.Program[index], right.Program[index]) {
		return nil, errors.New("parent programs have no deterministic mutation point")
	}

	program := append([]Op(nil), left.Program...)
	program[index] = right.Program[index]
	patternHash := recombinedPatternHash(left, right, program, index)
	return &MemoryStructureCandidate{
		ID:                  fmt.Sprintf("memory-structure-recombination-%s", patternHash[:16]),
		PatternHash:         patternHash,
		SourceExperienceIDs: mergeSortedUnique(left.SourceExperienceIDs, right.SourceExperienceIDs),
		State:               memoryStructureRecombinationCandidateState,
		ParentStructureIDs:  uniqueSortedStrings([]string{left.ID, right.ID}),
		Program:             program,
		MutationIndex:       index,
	}, nil
}

func sameOp(left, right Op) bool {
	return left.Code == right.Code && left.A == right.A && left.B == right.B && left.C == right.C
}

func recombinationIndex(leftID, rightID string, limit int) int {
	sum := sha256.Sum256([]byte(leftID + "\x00" + rightID))
	value := uint64(0)
	for i := 0; i < 8; i++ {
		value = (value << 8) | uint64(sum[i])
	}
	return int(value % uint64(limit))
}

func recombinedPatternHash(left, right *MemoryStructure, program []Op, index int) string {
	payload := struct {
		LeftPattern  string
		RightPattern string
		Program      []Op
		Index        int
	}{left.PatternHash, right.PatternHash, program, index}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// MaterializeRecombinationCandidate 在真实执行产生 Experience 后，把它转成普通 candidate。
// 这样既保留父结构血缘，又确保既有 ValidateCandidate 能继续作为唯一现实验证入口。
func MaterializeRecombinationCandidate(candidate *MemoryStructureCandidate, experience *MemoryExperience) (*MemoryStructureCandidate, error) {
	if candidate == nil || candidate.State != memoryStructureRecombinationCandidateState {
		return nil, errors.New("recombination candidate unavailable")
	}
	if experience == nil || strings.TrimSpace(experience.ID) == "" {
		return nil, errors.New("execution experience unavailable")
	}
	out := cloneMemoryStructureCandidate(candidate)
	out.State = memoryStructureCandidateState
	out.PatternHash = memoryStructurePatternHash(*experience)
	out.SourceExperienceIDs = mergeSortedUnique(out.SourceExperienceIDs, []string{experience.ID})
	return out, nil
}
