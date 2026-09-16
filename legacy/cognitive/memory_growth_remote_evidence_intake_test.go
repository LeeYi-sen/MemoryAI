package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func seedRemoteEvidenceIntakeStructure(t *testing.T, e *Engine, id, kind, evidenceID string, program []Op, expected map[string]string) *MemoryStructure {
	t.Helper()
	experiences := NewMemoryExperienceLedger()
	source, err := experiences.Record(MemoryExperience{
		ID:          "source-" + id,
		Context:     map[string]string{"local_scope": id},
		Observation: map[string]string{"source": "local-memory"},
		Action:      map[string]string{"operation": "consider-remote-evidence"},
		Outcome:     map[string]string{"seed": "ready"},
	})
	if err != nil {
		t.Fatal(err)
	}
	formation := NewMemoryStructureFormation()
	validation := NewMemoryStructureValidationLedger()
	structure := &MemoryStructure{
		ID:                  id,
		CandidateID:         "candidate-" + id,
		PatternHash:         memoryStructurePatternHash(*source),
		SourceExperienceIDs: []string{source.ID},
		Context:             cloneStringMap(source.Context),
		Observation:         cloneStringMap(source.Observation),
		Action: map[string]string{
			"operation":                   "consider-remote-evidence",
			remoteEvidenceIntakeActionKey: "1",
			remoteEvidenceKindActionKey:   kind,
			remoteEvidenceIDActionKey:     evidenceID,
		},
		ExpectedOutcome: cloneStringMap(expected),
		State:           memoryStructureValidatedState,
		ValidationCount: 1,
		SuccessCount:    1,
		Program:         append([]Op(nil), program...),
	}
	validation.structures[structure.CandidateID] = cloneMemoryStructure(structure)
	validation.validated[structure.CandidateID] = cloneMemoryStructure(structure)
	if err := PersistMemoryGrowthState(e, experiences, formation, validation); err != nil {
		t.Fatal(err)
	}
	executable, err := ExecutableMemory(structure)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.upsertExplicitMemoryBounded(executable); err != nil {
		t.Fatal(err)
	}
	if err := e.persistAll(); err != nil {
		t.Fatal(err)
	}
	return structure
}

func remoteExperienceEvidence(t *testing.T, id, origin string, revision uint64, status string) *RemoteMemoryEvidence {
	t.Helper()
	payload, err := json.Marshal(MemoryExperience{
		ID:          id,
		CreatedNano: 123,
		Context:     map[string]string{"region": "east"},
		Observation: map[string]string{"sensor": "remote"},
		Action:      map[string]string{"operation": "probe"},
		Outcome:     map[string]string{"status_code": status},
		ContentHash: "remote-content-hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &RemoteMemoryEvidence{
		Kind: remoteEvidenceExperience, EvidenceID: id, OriginNode: origin, OriginEndpoint: "https://node.invalid",
		RequesterNode: "requester", BackingMemoryID: memoryGrowthStateRecordID, BackingRevision: revision,
		ReadNano: 456, GrantNonce: "grant-nonce", GrantExpiresUnix: 9999999999,
		PayloadDigest: fmt.Sprintf("digest-%d-%s", revision, status), Payload: payload,
	}
}

func TestRemoteEvidenceMemoryProgramCommitsSelectedLocalExperience(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	structure := seedRemoteEvidenceIntakeStructure(t, e, "memory-remote-intake-a", remoteEvidenceExperience, "remote-exp-a", []Op{
		{Code: "set", A: remoteEvidenceCommitVar, B: "1"},
		{Code: "set", A: remoteEvidenceExperienceContextPrefix + "learned_region", B: "{{remote_experience_context.region}}"},
		{Code: "set", A: remoteEvidenceExperienceOutcomePrefix + "status_code", B: "{{remote_experience_outcome.status_code}}"},
		{Code: "set", A: remoteEvidenceReasonVar, B: "Memory program selected exact remote fact"},
		{Code: "halt"},
	}, map[string]string{"status_code": "200"})

	readCalls := 0
	reader := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		readCalls++
		if kind != remoteEvidenceExperience || evidenceID != "remote-exp-a" {
			t.Fatalf("unexpected reader request kind=%q id=%q", kind, evidenceID)
		}
		return remoteExperienceEvidence(t, evidenceID, "node-a", 7, "200"), nil
	}
	result, err := runRemoteEvidenceIntakeCycle(e, reader)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed || !result.Persisted || result.LocalExperienceID == "" || readCalls != 1 {
		t.Fatalf("remote evidence did not become Memory-authored local Experience: result=%#v reads=%d", result, readCalls)
	}

	experiences, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	local, ok := experiences.Get(result.LocalExperienceID)
	if !ok {
		t.Fatalf("local Experience missing: %s", result.LocalExperienceID)
	}
	if local.Context["learned_region"] != "east" || local.Outcome["status_code"] != "200" {
		t.Fatalf("Memory-selected evidence projection missing: %#v", local)
	}
	if local.Observation["remote_evidence_origin_node"] != "node-a" || local.Observation["remote_evidence_backing_revision"] != "7" || local.Observation["remote_evidence_payload_digest"] == "" {
		t.Fatalf("remote provenance missing from local Experience: %#v", local.Observation)
	}
	if local.ParentIDs[0] != structure.SourceExperienceIDs[0] {
		t.Fatalf("local Experience lost local Memory lineage: %#v", local.ParentIDs)
	}
	if _, exists := local.Observation[remoteEvidenceFramePayloadKey]; exists {
		t.Fatal("Kernel copied raw remote payload into local Experience")
	}
	_, factMemory, err := e.resolveLocalFabricMemory(result.IntakeFactID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(factMemory.State), "status_code") || strings.Contains(fmt.Sprint(factMemory.State), string(remoteExperienceEvidence(t, "remote-exp-a", "node-a", 7, "200").Payload)) {
		t.Fatal("intake fact persisted remote payload instead of provenance fingerprint")
	}
	beliefs, err := MemoryBeliefsForStructure(e, structure.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(beliefs) != 1 || len(beliefs[0].SupportingExperienceIDs) != 1 {
		t.Fatalf("Memory-authored local Experience did not feed existing Belief path: %#v", beliefs)
	}
}

func TestRemoteEvidenceRejectedByMemoryProgramDoesNotCreateExperience(t *testing.T) {
	e, _ := newMemoryGrowthRuntimeTestEngine(t)
	defer e.close()
	seedRemoteEvidenceIntakeStructure(t, e, "memory-remote-intake-reject", remoteEvidenceExperience, "remote-exp-reject", []Op{
		{Code: "set", A: remoteEvidenceCommitVar, B: "0"},
		{Code: "set", A: remoteEvidenceReasonVar, B: "Memory declined this evidence"},
		{Code: "halt"},
	}, nil)

	before, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	beforeCount := len(before.Snapshot())
	reads := 0
	reader := func(kind, evidenceID string) (*RemoteMemoryEvidence, error) {
		reads++
		return remoteExperienceEvidence(t, evidenceID, "node-r", 1, "500"), nil
	}
	first, err := runRemoteEvidenceIntakeCycle(e, reader)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Rejected || !first.Persisted || first.LocalExperienceID != "" {
		t.Fatalf("rejected evidence was not preserved as decision-only fact: %#v", first)
	}
	after, _, _, err := LoadMemoryGrowthState(e)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(after.Snapshot()); got != beforeCount {
		t.Fatalf("Kernel auto-imported rejected remote evidence: before=%d after=%d", beforeCount, got)
	}

	second, err := runRemoteEvidenceIntakeCycle(e, reader)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Skipped || reads != 2 {
		t.Fatalf("same evidence/contract should be decision-deduped only after a fresh remote read: result=%#v reads=%d", second, reads)
	}
}
