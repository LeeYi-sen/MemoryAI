package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRemoteEvidenceDisconnectStillForgetsAfterLocalDecisionFact(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	seedRemoteEvidenceIntakeStructure(t, e, "memory-remote-intake-disconnect", remoteEvidenceExperience, "remote-exp-disconnect", []Op{
		{Code: "set", A: remoteEvidenceCommitVar, B: "0"},
		{Code: "halt"},
	}, nil)

	firstReader := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		return remoteExperienceEvidence(t, evidenceID, "node-down", 1, "200"), nil
	}
	if _, err := runRemoteEvidenceIntakeCycle(e, firstReader); err != nil {
		t.Fatal(err)
	}
	disconnected := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		return nil, errors.New("origin unreachable")
	}
	result, err := runRemoteEvidenceIntakeCycle(e, disconnected)
	if err == nil || !strings.Contains(err.Error(), "origin unreachable") || !result.Skipped {
		t.Fatalf("local decision fingerprint incorrectly substituted for live remote evidence: result=%#v err=%v", result, err)
	}
}

func TestRemoteEvidenceRevisionOrContractChangeCanFormNewLocalExperience(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	structure := seedRemoteEvidenceIntakeStructure(t, e, "memory-remote-intake-revision", remoteEvidenceExperience, "remote-exp-revision", []Op{
		{Code: "set", A: remoteEvidenceCommitVar, B: "1"},
		{Code: "set", A: remoteEvidenceExperienceOutcomePrefix + "remote_status", B: "{{remote_experience_outcome.status_code}}"},
		{Code: "halt"},
	}, nil)

	currentRevision := uint64(1)
	currentStatus := "200"
	reader := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		return remoteExperienceEvidence(t, evidenceID, "node-v", currentRevision, currentStatus), nil
	}
	first, err := runRemoteEvidenceIntakeCycle(e, reader)
	if err != nil || !first.Committed {
		t.Fatalf("first remote evidence revision not committed: %#v err=%v", first, err)
	}
	currentRevision = 2
	currentStatus = "500"
	second, err := runRemoteEvidenceIntakeCycle(e, reader)
	if err != nil || !second.Committed || second.LocalExperienceID == first.LocalExperienceID {
		t.Fatalf("changed remote revision did not become distinct Memory experience: first=%#v second=%#v err=%v", first, second, err)
	}

	experiences, _, validation, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := experiences.Get(first.LocalExperienceID); !ok {
		t.Fatal("first local remote-evidence Experience disappeared")
	}
	if _, ok := experiences.Get(second.LocalExperienceID); !ok {
		t.Fatal("second local remote-evidence Experience missing")
	}
	stored, ok := validation.GetValidated(structure.CandidateID)
	if !ok || stored.ID != structure.ID {
		t.Fatalf("intake mutated validated Structure identity: %#v", stored)
	}
}
