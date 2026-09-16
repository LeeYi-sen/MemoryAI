package main

import "testing"

func TestMultiRemoteEvidenceKernelDoesNotMajorityVote(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	structure := seedRemoteEvidenceIntakeStructure(t, e, "memory-remote-no-vote", remoteEvidenceExperience, "unused", []Op{
		// Two remote sources will say 200 and one will say 500. The Memory Program
		// explicitly rejects the episode; Kernel must not overrule it by majority.
		{Code: "set", A: remoteEvidenceCommitVar, B: "0"},
		{Code: "set", A: remoteEvidenceReasonVar, B: "Memory keeps conflict transient"},
		{Code: "halt"},
	}, nil)
	enableMultiRemoteEvidenceInputs(t, e, structure, []RemoteEvidenceInput{
		{Kind: remoteEvidenceExperience, EvidenceID: "a"},
		{Kind: remoteEvidenceExperience, EvidenceID: "b"},
		{Kind: remoteEvidenceExperience, EvidenceID: "c"},
	})
	statuses := map[string]string{"a": "200", "b": "200", "c": "500"}
	reader := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		return remoteExperienceEvidence(t, evidenceID, "node-"+evidenceID, 1, statuses[evidenceID]), nil
	}
	result, err := runRemoteEvidenceEpisodeCycle(e, reader)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Rejected || result.Committed || result.LocalExperienceID != "" || result.EvidenceCount != 3 {
		t.Fatalf("Kernel overrode Memory decision or lost episode facts: %#v", result)
	}
}

func TestMultiRemoteEvidenceChangedMemberRevisionCreatesNewEpisode(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	structure := seedRemoteEvidenceIntakeStructure(t, e, "memory-remote-revision", remoteEvidenceExperience, "unused", []Op{
		{Code: "set", A: remoteEvidenceCommitVar, B: "1"},
		{Code: "set", A: remoteEvidenceExperienceOutcomePrefix + "accepted", B: "1"},
		{Code: "halt"},
	}, nil)
	enableMultiRemoteEvidenceInputs(t, e, structure, []RemoteEvidenceInput{
		{Kind: remoteEvidenceExperience, EvidenceID: "left"},
		{Kind: remoteEvidenceExperience, EvidenceID: "right"},
	})
	rightRevision := uint64(1)
	reader := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		if evidenceID == "left" {
			return remoteExperienceEvidence(t, evidenceID, "node-left", 1, "200"), nil
		}
		return remoteExperienceEvidence(t, evidenceID, "node-right", rightRevision, "500"), nil
	}
	first, err := runRemoteEvidenceEpisodeCycle(e, reader)
	if err != nil || !first.Committed {
		t.Fatalf("first multi episode failed: result=%#v err=%v", first, err)
	}
	rightRevision = 2
	second, err := runRemoteEvidenceEpisodeCycle(e, reader)
	if err != nil || !second.Committed {
		t.Fatalf("changed remote member was not reconsidered: result=%#v err=%v", second, err)
	}
	if first.EpisodeFactID == second.EpisodeFactID || first.LocalExperienceID == second.LocalExperienceID || first.EpisodeDigest == second.EpisodeDigest {
		t.Fatalf("remote revision change did not form a new episode: first=%#v second=%#v", first, second)
	}
}
