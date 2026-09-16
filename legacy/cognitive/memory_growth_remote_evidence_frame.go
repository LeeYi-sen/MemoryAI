package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

const (
	remoteEvidenceFrameKindKey           = "remote_evidence_kind"
	remoteEvidenceFrameIDKey             = "remote_evidence_id"
	remoteEvidenceFrameOriginNodeKey     = "remote_evidence_origin_node"
	remoteEvidenceFrameOriginEndpointKey = "remote_evidence_origin_endpoint"
	remoteEvidenceFrameRequesterNodeKey  = "remote_evidence_requester_node"
	remoteEvidenceFrameBackingIDKey      = "remote_evidence_backing_memory_id"
	remoteEvidenceFrameBackingRevKey     = "remote_evidence_backing_revision"
	remoteEvidenceFrameReadNanoKey       = "remote_evidence_read_nano"
	remoteEvidenceFrameGrantNonceKey     = "remote_evidence_grant_nonce"
	remoteEvidenceFrameGrantExpiryKey    = "remote_evidence_grant_expires_unix"
	remoteEvidenceFrameDigestKey         = "remote_evidence_payload_digest"
	remoteEvidenceFramePayloadKey        = "remote_evidence_payload_json"
)

func setFrameStringMap(f *Frame, prefix string, values map[string]string) {
	if f == nil || len(values) == 0 {
		return
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		f.Vars[prefix+key] = values[key]
	}
}

func setFrameUintMap(f *Frame, prefix string, values map[string]uint64) {
	if f == nil || len(values) == 0 {
		return
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		f.Vars[prefix+key] = strconv.FormatUint(values[key], 10)
	}
}

func remoteEvidenceDecisionFrame(evidence *RemoteMemoryEvidence, structureID string) (*Frame, error) {
	if evidence == nil {
		return nil, fmt.Errorf("remote evidence unavailable")
	}
	f := newFrame()
	if f.Vars == nil {
		f.Vars = map[string]string{}
	}
	if f.Lists == nil {
		f.Lists = map[string][]string{}
	}
	f.Vars["__subject"] = structureID
	f.Vars[remoteEvidenceFrameKindKey] = evidence.Kind
	f.Vars[remoteEvidenceFrameIDKey] = evidence.EvidenceID
	f.Vars[remoteEvidenceFrameOriginNodeKey] = evidence.OriginNode
	f.Vars[remoteEvidenceFrameOriginEndpointKey] = evidence.OriginEndpoint
	f.Vars[remoteEvidenceFrameRequesterNodeKey] = evidence.RequesterNode
	f.Vars[remoteEvidenceFrameBackingIDKey] = evidence.BackingMemoryID
	f.Vars[remoteEvidenceFrameBackingRevKey] = strconv.FormatUint(evidence.BackingRevision, 10)
	f.Vars[remoteEvidenceFrameReadNanoKey] = strconv.FormatInt(evidence.ReadNano, 10)
	f.Vars[remoteEvidenceFrameGrantNonceKey] = evidence.GrantNonce
	f.Vars[remoteEvidenceFrameGrantExpiryKey] = strconv.FormatInt(evidence.GrantExpiresUnix, 10)
	f.Vars[remoteEvidenceFrameDigestKey] = evidence.PayloadDigest
	f.Vars[remoteEvidenceFramePayloadKey] = string(evidence.Payload)

	switch evidence.Kind {
	case remoteEvidenceExperience:
		var state MemoryExperience
		if err := json.Unmarshal(evidence.Payload, &state); err != nil {
			return nil, err
		}
		f.Vars["remote_experience_id"] = state.ID
		f.Vars["remote_experience_created_nano"] = strconv.FormatInt(state.CreatedNano, 10)
		f.Vars["remote_experience_prediction_hash"] = state.PredictionHash
		f.Vars["remote_experience_content_hash"] = state.ContentHash
		f.Vars["remote_experience_validation_count"] = strconv.FormatUint(state.ValidationCount, 10)
		f.Vars["remote_experience_success_count"] = strconv.FormatUint(state.SuccessCount, 10)
		f.Vars["remote_experience_failure_count"] = strconv.FormatUint(state.FailureCount, 10)
		f.Lists["remote_experience_parent_ids"] = cloneStrings(state.ParentIDs)
		setFrameStringMap(f, "remote_experience_context.", state.Context)
		setFrameStringMap(f, "remote_experience_observation.", state.Observation)
		setFrameStringMap(f, "remote_experience_action.", state.Action)
		setFrameStringMap(f, "remote_experience_outcome.", state.Outcome)
		setFrameStringMap(f, "remote_experience_prediction_error.", state.PredictionError)
	case remoteEvidenceBelief:
		var state MemoryBeliefState
		if err := json.Unmarshal(evidence.Payload, &state); err != nil {
			return nil, err
		}
		f.Vars["remote_belief_id"] = state.ID
		f.Vars["remote_belief_structure_id"] = state.StructureID
		f.Vars["remote_belief_prediction_key"] = state.PredictionKey
		f.Vars["remote_belief_predicted_value"] = state.PredictedValue
		f.Vars["remote_belief_last_experience_id"] = state.LastExperienceID
		f.Vars["remote_belief_last_updated_nano"] = strconv.FormatInt(state.LastUpdatedNano, 10)
		f.Lists["remote_belief_evidence_experience_ids"] = cloneStrings(state.EvidenceExperienceIDs)
		f.Lists["remote_belief_supporting_experience_ids"] = cloneStrings(state.SupportingExperienceIDs)
		f.Lists["remote_belief_conflicting_experience_ids"] = cloneStrings(state.ConflictingExperienceIDs)
		f.Lists["remote_belief_missing_experience_ids"] = cloneStrings(state.MissingExperienceIDs)
		setFrameUintMap(f, "remote_belief_observed_value.", state.ObservedValues)
	case remoteEvidenceContextSplit:
		var state MemoryContextSplitState
		if err := json.Unmarshal(evidence.Payload, &state); err != nil {
			return nil, err
		}
		f.Vars["remote_context_split_id"] = state.ID
		f.Vars["remote_context_split_parent_structure_id"] = state.ParentStructureID
		f.Vars["remote_context_split_belief_id"] = state.BeliefID
		f.Vars["remote_context_split_prediction_key"] = state.PredictionKey
		f.Vars["remote_context_split_predicted_value"] = state.PredictedValue
		f.Vars["remote_context_split_context_key"] = state.ContextKey
		f.Vars["remote_context_split_status"] = state.Status
		f.Vars["remote_context_split_created_nano"] = strconv.FormatInt(state.CreatedNano, 10)
		f.Vars["remote_context_split_updated_nano"] = strconv.FormatInt(state.UpdatedNano, 10)
		branches := make([]string, 0, len(state.Branches))
		for _, branch := range state.Branches {
			raw, err := json.Marshal(branch)
			if err != nil {
				return nil, err
			}
			branches = append(branches, string(raw))
		}
		f.Lists["remote_context_split_branches_json"] = branches
	case remoteEvidenceActionInstance:
		var state GroundedActionInstanceState
		if err := json.Unmarshal(evidence.Payload, &state); err != nil {
			return nil, err
		}
		f.Vars["remote_action_instance_id"] = state.ID
		f.Vars["remote_action_instance_action_id"] = state.ActionID
		f.Vars["remote_action_instance_series_id"] = state.SeriesID
		f.Vars["remote_action_instance_structure_id"] = state.StructureID
		f.Vars["remote_action_instance_adapter_id"] = state.AdapterID
		f.Vars["remote_action_instance_template_action_id"] = state.TemplateActionID
		f.Vars["remote_action_instance_generation"] = strconv.FormatUint(state.Generation, 10)
		f.Vars["remote_action_instance_parent_instance_id"] = state.ParentInstanceID
		f.Vars["remote_action_instance_parent_action_id"] = state.ParentActionID
		f.Vars["remote_action_instance_parent_experience_id"] = state.ParentExperienceID
		f.Vars["remote_action_instance_created_nano"] = strconv.FormatInt(state.CreatedNano, 10)
		setFrameStringMap(f, "remote_action_instance_context.", state.Context)
	default:
		return nil, fmt.Errorf("unsupported remote evidence kind %q", evidence.Kind)
	}
	return f, nil
}

func framePrefixedStringMap(vars map[string]string, prefix string) map[string]string {
	if len(vars) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range vars {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			out[key[len(prefix):]] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
