package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func enableMultiRemoteEvidenceInputs(t *testing.T, e *Engine, structure *MemoryStructure, inputs []RemoteEvidenceInput) *MemoryStructure {
	t.Helper()
	raw, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	experiences, formation, validation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	stored := validation.validated[structure.CandidateID]
	if stored == nil {
		t.Fatalf("validated structure missing: %s", structure.CandidateID)
	}
	stored.Action[remoteEvidenceInputsActionKey] = string(raw)
	delete(stored.Action, remoteEvidenceKindActionKey)
	delete(stored.Action, remoteEvidenceIDActionKey)
	validation.structures[structure.CandidateID] = cloneMemoryStructure(stored)
	validation.validated[structure.CandidateID] = cloneMemoryStructure(stored)
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	return cloneMemoryStructure(stored)
}

func TestMultiRemoteEvidenceRunsOneMemoryReasoningEpisode(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	structure := seedRemoteEvidenceIntakeStructure(t, e, "memory-remote-multi", remoteEvidenceExperience, "unused", []Op{
		{Code: "cmp_eq", A: "same", B: "{{remote_evidence.0.remote_experience_outcome.status_code}}", C: "{{remote_evidence.1.remote_experience_outcome.status_code}}"},
		{Code: "jump_if", A: "{{same}}", B: "reject"},
		{Code: "set", A: remoteEvidenceCommitVar, B: "1"},
		{Code: "set", A: remoteEvidenceExperienceOutcomePrefix + "comparison", B: "conflict-preserved"},
		{Code: "set", A: remoteEvidenceExperienceObservationPrefix + "left_node", B: "{{remote_evidence.0.remote_evidence_origin_node}}"},
		{Code: "set", A: remoteEvidenceExperienceObservationPrefix + "right_node", B: "{{remote_evidence.1.remote_evidence_origin_node}}"},
		{Code: "jump", A: "done"},
		{Code: "label", A: "reject"},
		{Code: "set", A: remoteEvidenceCommitVar, B: "0"},
		{Code: "label", A: "done"},
		{Code: "halt"},
	}, nil)
	structure = enableMultiRemoteEvidenceInputs(t, e, structure, []RemoteEvidenceInput{
		{Kind: remoteEvidenceExperience, EvidenceID: "remote-left"},
		{Kind: remoteEvidenceExperience, EvidenceID: "remote-right"},
	})

	reads := []string{}
	reader := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		reads = append(reads, kind+"/"+evidenceID)
		switch evidenceID {
		case "remote-left":
			return remoteExperienceEvidence(t, evidenceID, "node-left", 4, "200"), nil
		case "remote-right":
			return remoteExperienceEvidence(t, evidenceID, "node-right", 9, "500"), nil
		default:
			return nil, fmt.Errorf("unexpected evidence %s", evidenceID)
		}
	}
	result, err := runRemoteEvidenceEpisodeCycle(e, reader)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed || !result.Persisted || result.EvidenceCount != 2 || result.EpisodeDigest == "" {
		t.Fatalf("multi-evidence episode not committed: %#v", result)
	}
	if got := strings.Join(reads, ","); got != "experience/remote-left,experience/remote-right" {
		t.Fatalf("Memory-authored evidence order changed: %s", got)
	}

	experiences, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	local, ok := experiences.Get(result.LocalExperienceID)
	if !ok {
		t.Fatalf("local Experience missing: %s", result.LocalExperienceID)
	}
	if local.Outcome["comparison"] != "conflict-preserved" {
		t.Fatalf("Memory Program did not preserve conflict: %#v", local.Outcome)
	}
	if local.Observation["left_node"] != "node-left" || local.Observation["right_node"] != "node-right" {
		t.Fatalf("one VM episode did not see both origins: %#v", local.Observation)
	}
	if local.Observation["remote_evidence_count"] != "2" || local.Observation["remote_evidence.0.backing_revision"] != "4" || local.Observation["remote_evidence.1.backing_revision"] != "9" {
		t.Fatalf("multi-evidence provenance missing: %#v", local.Observation)
	}
	for key, value := range local.Observation {
		if strings.Contains(key, "payload_json") || strings.Contains(value, "status_code") {
			t.Fatalf("raw remote payload leaked into local Experience: %s=%q", key, value)
		}
	}

	_, factMemory, err := e.resolveLocalFabricMemory(result.EpisodeFactID)
	if err != nil {
		t.Fatal(err)
	}
	if factMemory.State["evidence_count"] != 2 {
		t.Fatalf("intake fact did not preserve evidence count: %#v", factMemory.State)
	}
	if strings.Contains(fmt.Sprint(factMemory.State["evidence_fingerprints"]), "status_code") {
		t.Fatal("intake fact persisted remote payload rather than provenance-only fingerprints")
	}

	second, err := runRemoteEvidenceEpisodeCycle(e, reader)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Skipped || len(reads) != 4 {
		t.Fatalf("dedupe must happen only after two fresh reads: result=%#v reads=%v", second, reads)
	}
	_ = structure
}

func TestMultiRemoteEvidenceMissingMemberForgetsWholeEpisode(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	structure := seedRemoteEvidenceIntakeStructure(t, e, "memory-remote-missing", remoteEvidenceExperience, "unused", []Op{
		{Code: "set", A: remoteEvidenceCommitVar, B: "1"},
		{Code: "set", A: remoteEvidenceExperienceOutcomePrefix + "should_not_exist", B: "1"},
		{Code: "halt"},
	}, nil)
	enableMultiRemoteEvidenceInputs(t, e, structure, []RemoteEvidenceInput{
		{Kind: remoteEvidenceExperience, EvidenceID: "present"},
		{Kind: remoteEvidenceExperience, EvidenceID: "offline"},
	})
	before, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	beforeCount := len(before.Snapshot())
	reader := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		if evidenceID == "present" {
			return remoteExperienceEvidence(t, evidenceID, "node-up", 1, "200"), nil
		}
		return nil, errors.New("node offline")
	}
	result, err := runRemoteEvidenceEpisodeCycle(e, reader)
	if err == nil || !strings.Contains(err.Error(), "node offline") || !result.Skipped {
		t.Fatalf("missing episode member did not fail closed: result=%#v err=%v", result, err)
	}
	after, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(after.Snapshot()); got != beforeCount {
		t.Fatalf("partial remote episode created local Experience: before=%d after=%d", beforeCount, got)
	}
}
