#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, json
from pathlib import Path
EXPECTED_INPUT_SHA256='7d4e512d624c4b13ad619fc051e62342c1feb5e291c98f21f1ad35037b13087c'
def sha(b): return hashlib.sha256(b).hexdigest()

def context_policy():
 return {'id':'policy.context','layer':'emergent','generation':30,'revision':1,'content':'Mutable Context lifecycle policy owned by Memory.','tags':['memory','cog.policy.mutable','cog.evolvable'],'state':{'split_ttl_sec':'86400'}}

def fission_program():
 return [
  {'code':'state_get','a':'{{__subject}}','b':'subject_id','c':'cf_s'},{'code':'state_get','a':'{{__subject}}','b':'predicate_id','c':'cf_p'},
  {'code':'tag_list','a':'cog.belief.family.{{cf_s}}.{{cf_p}}','b':'cf_bs'},{'code':'list_len','a':'cf_bs','b':'cf_n'},{'code':'cmp_ge','a':'cf_two','b':'{{cf_n}}','c':'2'},{'code':'jump_if','a':'{{cf_two}}','b':'cf_scan'},{'code':'jump','a':'cf_end'},
  {'code':'label','a':'cf_scan'},{'code':'list_get','a':'cf_bs','b':'0','c':'cf_a'},{'code':'state_get','a':'{{cf_a}}','b':'object_id','c':'cf_ao'},{'code':'state_get','a':'{{cf_a}}','b':'context_id','c':'cf_ac'},{'code':'set','a':'cf_i','b':'1'},
  {'code':'label','a':'cf_loop'},{'code':'cmp_ge','a':'cf_done','b':'{{cf_i}}','c':'{{cf_n}}'},{'code':'jump_if','a':'{{cf_done}}','b':'cf_end'},
  {'code':'list_get','a':'cf_bs','b':'{{cf_i}}','c':'cf_b'},{'code':'state_get','a':'{{cf_b}}','b':'object_id','c':'cf_bo'},{'code':'state_get','a':'{{cf_b}}','b':'context_id','c':'cf_bc'},
  {'code':'cmp_eq','a':'cf_obj_same','b':'{{cf_ao}}','c':'{{cf_bo}}'},{'code':'jump_if','a':'{{cf_obj_same}}','b':'cf_next'},
  {'code':'cmp_eq','a':'cf_ctx_same','b':'{{cf_ac}}','c':'{{cf_bc}}'},{'code':'jump_if','a':'{{cf_ctx_same}}','b':'cf_conflict'},
  {'code':'set','a':'cf_raw','b':'{{cf_s}}|{{cf_p}}|{{cf_ac}}|{{cf_bc}}'},{'code':'sha256_text','a':'cf_raw','b':'cf_h'},
  {'code':'tag_list','a':'cog.context.split.key.{{cf_h}}','b':'cf_old'},{'code':'list_len','a':'cf_old','b':'cf_on'},{'code':'cmp_gt','a':'cf_have','b':'{{cf_on}}','c':'0'},{'code':'jump_if','a':'{{cf_have}}','b':'cf_next'},
  {'code':'memory_new','a':'cf_sp','args':{'content':'Context split induced because the same conceptual family supports different outcomes in different contexts.','layer':'emergent','parents':'{{cf_a}},{{cf_b}},context.fission.parent','tags':'memory,cog.context.split,cog.context.split.active,cog.context.split.key.{{cf_h}}'}},
  {'code':'state_set','a':'{{cf_sp}}','b':'belief_a','c':'{{cf_a}}'},{'code':'state_set','a':'{{cf_sp}}','b':'belief_b','c':'{{cf_b}}'},{'code':'state_set','a':'{{cf_sp}}','b':'subject_id','c':'{{cf_s}}'},{'code':'state_set','a':'{{cf_sp}}','b':'predicate_id','c':'{{cf_p}}'},{'code':'state_set','a':'{{cf_sp}}','b':'context_a','c':'{{cf_ac}}'},{'code':'state_set','a':'{{cf_sp}}','b':'context_b','c':'{{cf_bc}}'},{'code':'state_set','a':'{{cf_sp}}','b':'status','c':'active'},
  {'code':'time_unix','a':'cf_now'},{'code':'state_get','a':'policy.context','b':'split_ttl_sec','c':'cf_ttl'},{'code':'num_add','a':'cf_exp','b':'{{cf_now}}','c':'{{cf_ttl}}'},{'code':'state_set','a':'{{cf_sp}}','b':'created_unix','c':'{{cf_now}}'},{'code':'state_set','a':'{{cf_sp}}','b':'expires_unix','c':'{{cf_exp}}'},
  {'code':'emit_event','a':'context.fission','b':'{{cf_sp}}'},{'code':'jump','a':'cf_next'},
  {'code':'label','a':'cf_conflict'},{'code':'memory_new','a':'cf_c','args':{'content':'Same-context competing Beliefs preserved for counterevidence research.','layer':'emergent','parents':'{{cf_a}},{{cf_b}},context.fission.parent','tags':'memory,cog.memory.conflict'}},{'code':'emit_event','a':'belief.conflict','b':'{{cf_c}}'},
  {'code':'label','a':'cf_next'},{'code':'num_add','a':'cf_i','b':'{{cf_i}}','c':'1'},{'code':'jump','a':'cf_loop'},{'code':'label','a':'cf_end'}]

def split_apply():
 return {'id':'context.split.apply.parent','layer':'emergent','generation':30,'revision':1,'content':'Apply a Context split to its Beliefs so later Prediction and Research preserve conditional scope.','tags':['memory','cog.context','runtime.trigger','cog.evolvable'],'trigger':['event:context.fission','subject_tag:cog.context.split.active'],'capabilities':['memory.write'],'program':[
  {'code':'state_get','a':'{{__subject}}','b':'belief_a','c':'ca_a'},{'code':'state_get','a':'{{__subject}}','b':'belief_b','c':'ca_b'},{'code':'state_get','a':'{{__subject}}','b':'context_a','c':'ca_ac'},{'code':'state_get','a':'{{__subject}}','b':'context_b','c':'ca_bc'},
  {'code':'state_set','a':'{{ca_a}}','b':'context_split_id','c':'{{__subject}}'},{'code':'state_set','a':'{{ca_a}}','b':'context_scope','c':'{{ca_ac}}'},{'code':'memory_tag_add','a':'{{ca_a}}','b':'cog.belief.context.scoped'},
  {'code':'state_set','a':'{{ca_b}}','b':'context_split_id','c':'{{__subject}}'},{'code':'state_set','a':'{{ca_b}}','b':'context_scope','c':'{{ca_bc}}'},{'code':'memory_tag_add','a':'{{ca_b}}','b':'cog.belief.context.scoped'}]}

def split_expire():
 return {'id':'context.split.expire.parent','layer':'emergent','generation':30,'revision':1,'content':'Expire stale Context splits and release their Belief scoping links.','tags':['memory','cog.context','runtime.trigger','cog.evolvable'],'trigger':['event:idle'],'capabilities':['memory.write'],'program':[
  {'code':'tag_list','a':'cog.context.split.active','b':'cx_ls'},{'code':'list_len','a':'cx_ls','b':'cx_n'},{'code':'set','a':'cx_i','b':'0'},{'code':'time_unix','a':'cx_now'},
  {'code':'label','a':'cx_loop'},{'code':'cmp_ge','a':'cx_done','b':'{{cx_i}}','c':'{{cx_n}}'},{'code':'jump_if','a':'{{cx_done}}','b':'cx_end'},{'code':'list_get','a':'cx_ls','b':'{{cx_i}}','c':'cx_s'},
  {'code':'state_get','a':'{{cx_s}}','b':'expires_unix','c':'cx_exp'},{'code':'cmp_ge','a':'cx_due','b':'{{cx_now}}','c':'{{cx_exp}}'},{'code':'jump_if','a':'{{cx_due}}','b':'cx_expire'},{'code':'jump','a':'cx_next'},
  {'code':'label','a':'cx_expire'},{'code':'state_set','a':'{{cx_s}}','b':'status','c':'expired'},{'code':'memory_tag_remove','a':'{{cx_s}}','b':'cog.context.split.active'},{'code':'memory_tag_add','a':'{{cx_s}}','b':'cog.context.split.expired'},
  {'code':'state_get','a':'{{cx_s}}','b':'belief_a','c':'cx_a'},{'code':'state_get','a':'{{cx_s}}','b':'belief_b','c':'cx_b'},
  {'code':'state_set','a':'{{cx_a}}','b':'context_split_id','c':''},{'code':'state_set','a':'{{cx_a}}','b':'context_scope','c':''},{'code':'memory_tag_remove','a':'{{cx_a}}','b':'cog.belief.context.scoped'},
  {'code':'state_set','a':'{{cx_b}}','b':'context_split_id','c':''},{'code':'state_set','a':'{{cx_b}}','b':'context_scope','c':''},{'code':'memory_tag_remove','a':'{{cx_b}}','b':'cog.belief.context.scoped'},
  {'code':'label','a':'cx_next'},{'code':'num_add','a':'cx_i','b':'{{cx_i}}','c':'1'},{'code':'jump','a':'cx_loop'},{'code':'label','a':'cx_end'}]}

def context_strategy():
 return [
  {'code':'state_get','a':'{{focus_id}}','b':'context_split_id','c':'rx_split'},{'code':'cmp_eq','a':'rx_nosplit','b':'{{rx_split}}'},{'code':'jump_if','a':'{{rx_nosplit}}','b':'rx_family'},
  {'code':'list_unique_append','a':'research_candidates','b':'{{rx_split}}'},{'code':'state_get','a':'{{rx_split}}','b':'belief_a','c':'rx_a'},{'code':'state_get','a':'{{rx_split}}','b':'belief_b','c':'rx_b'},{'code':'list_unique_append','a':'research_candidates','b':'{{rx_a}}'},{'code':'list_unique_append','a':'research_candidates','b':'{{rx_b}}'},{'code':'jump','a':'rx_end'},
  {'code':'label','a':'rx_family'},{'code':'state_get','a':'{{focus_id}}','b':'family_key','c':'rx_fk'},{'code':'cmp_eq','a':'rx_none','b':'{{rx_fk}}'},{'code':'jump_if','a':'{{rx_none}}','b':'rx_end'},
  {'code':'tag_list','a':'cog.belief.family.{{rx_fk}}','b':'rx_bs'},{'code':'list_len','a':'rx_bs','b':'rx_n'},{'code':'set','a':'rx_i','b':'0'},
  {'code':'label','a':'rx_loop'},{'code':'cmp_ge','a':'rx_done','b':'{{rx_i}}','c':'{{rx_n}}'},{'code':'jump_if','a':'{{rx_done}}','b':'rx_end'},{'code':'list_get','a':'rx_bs','b':'{{rx_i}}','c':'rx_id'},{'code':'list_unique_append','a':'research_candidates','b':'{{rx_id}}'},{'code':'num_add','a':'rx_i','b':'{{rx_i}}','c':'1'},{'code':'jump','a':'rx_loop'},{'code':'label','a':'rx_end'}]

def patch_prediction_context(program):
 out=[]; inserted_read=False; inserted_write=False
 for op in program:
  out.append(op)
  if op.get('code')=='state_get' and op.get('a')=='{{__subject}}' and op.get('b')=='context_id' and not inserted_read:
   out += [{'code':'state_get','a':'{{__subject}}','b':'context_split_id','c':'pb_split'},{'code':'state_get','a':'{{__subject}}','b':'context_scope','c':'pb_scope'}]; inserted_read=True
  if op.get('code')=='state_set' and op.get('a')=='{{pb_m}}' and op.get('b')=='context_id' and not inserted_write:
   out += [{'code':'state_set','a':'{{pb_m}}','b':'context_split_id','c':'{{pb_split}}'},{'code':'state_set','a':'{{pb_m}}','b':'context_scope','c':'{{pb_scope}}'}]; inserted_write=True
 return out

def resolve_program():
 return [
  {'code':'state_get','a':'{{__subject}}','b':'focus_id','c':'rr_f'},{'code':'list_len','a':'research_candidates','b':'rr_total'},{'code':'set','a':'rr_i','b':'0'},{'code':'set','a':'rr_new','b':'0'},
  {'code':'label','a':'rr_loop'},{'code':'cmp_ge','a':'rr_done','b':'{{rr_i}}','c':'{{rr_total}}'},{'code':'jump_if','a':'{{rr_done}}','b':'rr_score'},
  {'code':'list_get','a':'research_candidates','b':'{{rr_i}}','c':'rr_c'},{'code':'set','a':'rr_pair','b':'{{__subject}}|{{rr_c}}'},{'code':'sha256_text','a':'rr_pair','b':'rr_h'},{'code':'tag_list','a':'cog.research.evidence.unique.{{rr_h}}','b':'rr_seen'},{'code':'list_len','a':'rr_seen','b':'rr_sn'},{'code':'cmp_gt','a':'rr_dup','b':'{{rr_sn}}','c':'0'},{'code':'jump_if','a':'{{rr_dup}}','b':'rr_next'},
  {'code':'memory_new','a':'rr_link','args':{'content':'Novel structural evidence link discovered for this Goal.','layer':'emergent','parents':'{{__subject}},{{rr_c}},research.resolve.parent','tags':'memory,cog.research.internal.evidence,cog.research.evidence.unique.{{rr_h}}'}},{'code':'state_set','a':'{{rr_link}}','b':'goal_id','c':'{{__subject}}'},{'code':'state_set','a':'{{rr_link}}','b':'candidate_id','c':'{{rr_c}}'},{'code':'num_add','a':'rr_new','b':'{{rr_new}}','c':'1'},
  {'code':'label','a':'rr_next'},{'code':'num_add','a':'rr_i','b':'{{rr_i}}','c':'1'},{'code':'jump','a':'rr_loop'},
  {'code':'label','a':'rr_score'},{'code':'cmp_gt','a':'rr_have','b':'{{rr_total}}','c':'0'},{'code':'jump_if','a':'{{rr_have}}','b':'rr_gain_calc'},{'code':'set','a':'rr_gain','b':'0'},{'code':'jump','a':'rr_cost'},
  {'code':'label','a':'rr_gain_calc'},{'code':'num_div','a':'rr_gain','b':'{{rr_new}}','c':'{{rr_total}}'},
  {'code':'label','a':'rr_cost'},{'code':'time_unix_nano','a':'rr_now'},{'code':'state_get','a':'{{__subject}}','b':'research_started_ns','c':'rr_start'},{'code':'var_default','a':'rr_start','b':'{{rr_now}}'},{'code':'num_sub','a':'rr_ns','b':'{{rr_now}}','c':'{{rr_start}}'},{'code':'num_div','a':'rr_us','b':'{{rr_ns}}','c':'1000'},{'code':'num_div','a':'rr_sec','b':'{{rr_us}}','c':'1000000'},
  {'code':'memory_new','a':'rr_ev','args':{'content':'SELF_ONLY measured research evidence and real elapsed cost.','layer':'emergent','parents':'{{__subject}},{{strategy_id}},research.resolve.parent','tags':'memory,cog.research.feedback'}},{'code':'state_set','a':'{{rr_ev}}','b':'goal_id','c':'{{__subject}}'},{'code':'state_set','a':'{{rr_ev}}','b':'focus_id','c':'{{rr_f}}'},{'code':'state_set','a':'{{rr_ev}}','b':'structure_id','c':'{{strategy_id}}'},{'code':'state_set','a':'{{rr_ev}}','b':'information_gain','c':'{{rr_gain}}'},{'code':'state_set','a':'{{rr_ev}}','b':'physical_cost_us','c':'{{rr_us}}'},{'code':'state_set','a':'{{rr_ev}}','b':'evidence_count','c':'{{rr_new}}'},{'code':'emit_event','a':'research.feedback','b':'{{rr_ev}}'},
  {'code':'state_get','a':'policy.research','b':'max_cost_sec','c':'rr_maxsec'},{'code':'cmp_ge','a':'rr_overcost','b':'{{rr_sec}}','c':'{{rr_maxsec}}'},{'code':'jump_if','a':'{{rr_overcost}}','b':'rr_cost_limit'},
  {'code':'state_get','a':'policy.research','b':'min_expected_gain','c':'rr_min'},{'code':'cmp_ge','a':'rr_enough','b':'{{rr_gain}}','c':'{{rr_min}}'},{'code':'jump_if','a':'{{rr_enough}}','b':'rr_reduce'},{'code':'emit_event','a':'research.external.residual','b':'{{__subject}}'},{'code':'jump','a':'rr_end'},
  {'code':'label','a':'rr_cost_limit'},{'code':'state_set','a':'{{__subject}}','b':'research_state','c':'cost-limit'},{'code':'state_set','a':'{{__subject}}','b':'last_information_gain','c':'{{rr_gain}}'},{'code':'state_set','a':'{{__subject}}','b':'last_research_cost_sec','c':'{{rr_sec}}'},{'code':'jump','a':'rr_end'},
  {'code':'label','a':'rr_reduce'},{'code':'state_get','a':'{{__subject}}','b':'uncertainty','c':'rr_u'},{'code':'var_default','a':'rr_u','b':'1'},{'code':'state_get','a':'policy.research','b':'gain_uncertainty_scale','c':'rr_scale'},{'code':'num_mul','a':'rr_drop','b':'{{rr_gain}}','c':'{{rr_scale}}'},{'code':'num_sub','a':'rr_nu','b':'{{rr_u}}','c':'{{rr_drop}}'},{'code':'state_set','a':'{{__subject}}','b':'uncertainty','c':'{{rr_nu}}'},
  {'code':'state_get','a':'{{rr_f}}','b':'validation_count','c':'rr_valid'},{'code':'var_default','a':'rr_valid','b':'0'},{'code':'state_get','a':'policy.research','b':'min_validation_count','c':'rr_minvalid'},{'code':'cmp_ge','a':'rr_vok','b':'{{rr_valid}}','c':'{{rr_minvalid}}'},{'code':'state_get','a':'policy.research','b':'satisfaction_uncertainty','c':'rr_sat'},{'code':'cmp_le','a':'rr_uok','b':'{{rr_nu}}','c':'{{rr_sat}}'},{'code':'jump_if','a':'{{rr_vok}}','b':'rr_vgate'},{'code':'emit_event','a':'research.external.residual','b':'{{__subject}}'},{'code':'jump','a':'rr_end'},{'code':'label','a':'rr_vgate'},{'code':'jump_if','a':'{{rr_uok}}','b':'rr_satisfy'},{'code':'emit_event','a':'research.external.residual','b':'{{__subject}}'},{'code':'jump','a':'rr_end'},
  {'code':'label','a':'rr_satisfy'},{'code':'state_set','a':'{{__subject}}','b':'status','c':'satisfied'},{'code':'memory_tag_remove','a':'{{__subject}}','b':'cog.goal.active'},{'code':'memory_tag_add','a':'{{__subject}}','b':'cog.goal.satisfied'},{'code':'emit_event','a':'goal.satisfied','b':'{{__subject}}'},{'code':'label','a':'rr_end'}]

def external_program():
 return [
  {'code':'state_get','a':'policy.research','b':'external_after_internal_miss','c':'re_allow'},{'code':'cmp_eq','a':'re_yes','b':'{{re_allow}}','c':'1'},{'code':'jump_if','a':'{{re_yes}}','b':'re_sources'},{'code':'jump','a':'re_end'},
  {'code':'label','a':'re_sources'},{'code':'tag_list','a':'source.adapter.enabled','b':'re_as'},{'code':'list_len','a':'re_as','b':'re_n'},{'code':'state_get','a':'policy.research','b':'max_parallel_sources','c':'re_max'},{'code':'set','a':'re_k','b':'0'},
  {'code':'state_get','a':'{{__subject}}','b':'focus_id','c':'re_f'},{'code':'state_get','a':'{{re_f}}','b':'preferred_surface','c':'re_surface'},{'code':'cmp_eq','a':'re_nos','b':'{{re_surface}}'},{'code':'jump_if','a':'{{re_nos}}','b':'re_content'},{'code':'jump','a':'re_prompt'},{'code':'label','a':'re_content'},{'code':'field_get','a':'{{re_f}}','b':'content','c':'re_surface'},{'code':'label','a':'re_prompt'},
  {'code':'set','a':'re_stim','b':'Observe evidence relevant to this raw surface without classifying ontology. Return JSON only: {"observations":["raw observation"]}. Surface: {{re_surface}}'},
  {'code':'label','a':'re_select_loop'},{'code':'cmp_ge','a':'re_kdone','b':'{{re_k}}','c':'{{re_max}}'},{'code':'jump_if','a':'{{re_kdone}}','b':'re_after'},{'code':'cmp_ge','a':'re_allsel','b':'{{re_k}}','c':'{{re_n}}'},{'code':'jump_if','a':'{{re_allsel}}','b':'re_after'},
  {'code':'set','a':'re_best','b':''},{'code':'set','a':'re_bestscore','b':'-1000000000'},{'code':'set','a':'re_i','b':'0'},
  {'code':'label','a':'re_scan'},{'code':'cmp_ge','a':'re_sdone','b':'{{re_i}}','c':'{{re_n}}'},{'code':'jump_if','a':'{{re_sdone}}','b':'re_take_best'},{'code':'list_get','a':'re_as','b':'{{re_i}}','c':'re_try'},{'code':'list_contains','a':'re_selected','b':'{{re_try}}','args':{'out':'re_used'}},{'code':'jump_if','a':'{{re_used}}','b':'re_next'},
  {'code':'copy','a':'source_id','b':'re_try'},{'code':'set','a':'context_key','b':'external-research'},{'code':'call','a':'source.reliability.compute.parent'},
  {'code':'state_get','a':'{{re_try}}','b':'expected_gain','c':'re_gain'},{'code':'state_get','a':'policy.research','b':'default_external_gain','c':'re_defgain'},{'code':'var_default','a':'re_gain','b':'{{re_defgain}}'},
  {'code':'state_get','a':'{{re_try}}','b':'cost_estimate','c':'re_cost'},{'code':'var_default','a':'re_cost','b':'0'},{'code':'state_get','a':'{{re_try}}','b':'avg_latency_sec','c':'re_lat'},{'code':'state_get','a':'policy.research','b':'default_external_latency_sec','c':'re_deflat'},{'code':'var_default','a':'re_lat','b':'{{re_deflat}}'},
  {'code':'state_get','a':'policy.research','b':'source_reliability_weight','c':'re_wr'},{'code':'state_get','a':'policy.research','b':'source_gain_weight','c':'re_wg'},{'code':'state_get','a':'policy.research','b':'source_cost_weight','c':'re_wc'},{'code':'state_get','a':'policy.research','b':'source_latency_weight','c':'re_wl'},
  {'code':'num_mul','a':'re_sr','b':'{{source_reliability}}','c':'{{re_wr}}'},{'code':'num_mul','a':'re_sg','b':'{{re_gain}}','c':'{{re_wg}}'},{'code':'num_mul','a':'re_sc','b':'{{re_cost}}','c':'{{re_wc}}'},{'code':'num_mul','a':'re_sl','b':'{{re_lat}}','c':'{{re_wl}}'},{'code':'num_add','a':'re_score','b':'{{re_sr}}','c':'{{re_sg}}'},{'code':'num_sub','a':'re_score','b':'{{re_score}}','c':'{{re_sc}}'},{'code':'num_sub','a':'re_score','b':'{{re_score}}','c':'{{re_sl}}'},
  {'code':'cmp_gt','a':'re_better','b':'{{re_score}}','c':'{{re_bestscore}}'},{'code':'jump_if','a':'{{re_better}}','b':'re_best_set'},{'code':'jump','a':'re_next'},{'code':'label','a':'re_best_set'},{'code':'copy','a':'re_best','b':'re_try'},{'code':'copy','a':'re_bestscore','b':'re_score'},
  {'code':'label','a':'re_next'},{'code':'num_add','a':'re_i','b':'{{re_i}}','c':'1'},{'code':'jump','a':'re_scan'},
  {'code':'label','a':'re_take_best'},{'code':'cmp_eq','a':'re_nobest','b':'{{re_best}}'},{'code':'jump_if','a':'{{re_nobest}}','b':'re_after'},{'code':'list_append','a':'re_selected','b':'{{re_best}}'},
  {'code':'memory_new','a':'re_req','args':{'content':'Optional external evidence request. External source is evidence, not the AI or answer.','layer':'acquired','parents':'{{__subject}},research.external.parent','tags':'memory,io.external.request.pending,cog.research.external.request'}},{'code':'state_set','a':'{{re_req}}','b':'source_adapter_id','c':'{{re_best}}'},{'code':'state_set','a':'{{re_req}}','b':'source_id','c':'{{re_best}}'},{'code':'state_set','a':'{{re_req}}','b':'goal_id','c':'{{__subject}}'},{'code':'state_set','a':'{{re_req}}','b':'stimulus_surface','c':'{{re_stim}}'},{'code':'state_set','a':'{{re_req}}','b':'request_kind','c':'evidence'},{'code':'state_set','a':'{{re_req}}','b':'io.status','c':'pending'},{'code':'num_add','a':'re_k','b':'{{re_k}}','c':'1'},{'code':'jump','a':'re_select_loop'},
  {'code':'label','a':'re_after'},{'code':'list_len','a':'re_selected','b':'re_selected_n'},{'code':'cmp_gt','a':'re_any','b':'{{re_selected_n}}','c':'0'},{'code':'jump_if','a':'{{re_any}}','b':'re_wait'},{'code':'jump','a':'re_end'},{'code':'label','a':'re_wait'},{'code':'state_set','a':'{{__subject}}','b':'research_state','c':'awaiting-external'},{'code':'label','a':'re_end'}]

def ingest_program():
 return [
  {'code':'copy','a':'xe_raw','b':'__event.normalized_payload'},{'code':'copy','a':'xe_src','b':'__event.source_id'},{'code':'copy','a':'xe_goal','b':'__event.goal_id'},{'code':'json_array_strings','a':'{{xe_raw}}','b':'observations','c':'xe_obs'},{'code':'list_len','a':'xe_obs','b':'xe_n'},{'code':'set','a':'xe_i','b':'0'},{'code':'set','a':'xe_new','b':'0'},
  {'code':'label','a':'xe_loop'},{'code':'cmp_ge','a':'xe_done','b':'{{xe_i}}','c':'{{xe_n}}'},{'code':'jump_if','a':'{{xe_done}}','b':'xe_feedback'},{'code':'list_get','a':'xe_obs','b':'{{xe_i}}','c':'xe_s'},{'code':'sha256_text','a':'xe_s','b':'xe_h'},{'code':'tag_list','a':'cog.experience.surface.{{xe_h}}','b':'xe_old'},{'code':'list_len','a':'xe_old','b':'xe_on'},{'code':'cmp_gt','a':'xe_dup','b':'{{xe_on}}','c':'0'},{'code':'jump_if','a':'{{xe_dup}}','b':'xe_next'},
  {'code':'memory_new','a':'xe_e','args':{'content':'Raw external observation; no external ontology labels are trusted.','layer':'acquired','parents':'{{__subject}},external.evidence.ingest.parent','tags':'memory,cog.experience.raw,cog.experience.unresolved,cog.experience.modality.text,cog.experience.surface.{{xe_h}}'}},{'code':'state_set','a':'{{xe_e}}','b':'surface','c':'{{xe_s}}'},{'code':'state_set','a':'{{xe_e}}','b':'surface_hash','c':'{{xe_h}}'},{'code':'state_set','a':'{{xe_e}}','b':'source_id','c':'{{xe_src}}'},{'code':'state_set','a':'{{xe_e}}','b':'modality','c':'text'},{'code':'time_unix','a':'xe_now'},{'code':'state_set','a':'{{xe_e}}','b':'observed_unix','c':'{{xe_now}}'},{'code':'num_add','a':'xe_new','b':'{{xe_new}}','c':'1'},{'code':'emit_event','a':'experience.created','b':'{{xe_e}}'},
  {'code':'label','a':'xe_next'},{'code':'num_add','a':'xe_i','b':'{{xe_i}}','c':'1'},{'code':'jump','a':'xe_loop'},
  {'code':'label','a':'xe_feedback'},{'code':'cmp_gt','a':'xe_have','b':'{{xe_n}}','c':'0'},{'code':'jump_if','a':'{{xe_have}}','b':'xe_gain_calc'},{'code':'set','a':'xe_gain','b':'0'},{'code':'jump','a':'xe_fbmake'},{'code':'label','a':'xe_gain_calc'},{'code':'num_div','a':'xe_gain','b':'{{xe_new}}','c':'{{xe_n}}'},
  {'code':'label','a':'xe_fbmake'},{'code':'memory_new','a':'xe_fb','args':{'content':'Observed external evidence novelty/gain/cost record.','layer':'acquired','parents':'{{__subject}},external.evidence.ingest.parent','tags':'memory,cog.research.feedback'}},{'code':'state_set','a':'{{xe_fb}}','b':'goal_id','c':'{{xe_goal}}'},{'code':'state_set','a':'{{xe_fb}}','b':'information_gain','c':'{{xe_gain}}'},{'code':'state_set','a':'{{xe_fb}}','b':'received_count','c':'{{xe_n}}'},{'code':'state_set','a':'{{xe_fb}}','b':'novel_count','c':'{{xe_new}}'},{'code':'copy','a':'xe_us','b':'__event.elapsed_us'},{'code':'state_set','a':'{{xe_fb}}','b':'physical_cost_us','c':'{{xe_us}}'},
  {'code':'state_num_add','a':'{{xe_src}}','b':'research_trials','c':'1','args':{'out':'xe_trials'}},{'code':'state_num_add','a':'{{xe_src}}','b':'research_gain_total','c':'{{xe_gain}}','args':{'out':'xe_gtotal'}},{'code':'state_num_add','a':'{{xe_src}}','b':'research_elapsed_us_total','c':'{{xe_us}}','args':{'out':'xe_utotal'}},{'code':'num_div','a':'xe_egain','b':'{{xe_gtotal}}','c':'{{xe_trials}}'},{'code':'num_div','a':'xe_avgus','b':'{{xe_utotal}}','c':'{{xe_trials}}'},{'code':'num_div','a':'xe_avgsec','b':'{{xe_avgus}}','c':'1000000'},{'code':'state_set','a':'{{xe_src}}','b':'expected_gain','c':'{{xe_egain}}'},{'code':'state_set','a':'{{xe_src}}','b':'avg_latency_sec','c':'{{xe_avgsec}}'},
  {'code':'emit_event','a':'research.feedback','b':'{{xe_fb}}'}]

def audit(d):
 by={m['id']:m for m in d['memories']}
 for x in ('policy.context','context.split.apply.parent','context.split.expire.parent'):
  if x not in by: raise RuntimeError(x+' missing')
 if 'max_cost_sec' not in json.dumps(by['research.resolve.parent.impl.base']['program']): raise RuntimeError('research max cost dead')
 if 'validation_count' not in json.dumps(by['research.resolve.parent.impl.base']['program']): raise RuntimeError('research can still satisfy without validation')
 if 're_selected' not in json.dumps(by['research.external.parent']['program']): raise RuntimeError('external source top-k absent')
 if 'novel_count' not in json.dumps(by['external.evidence.ingest.parent']['program']): raise RuntimeError('external novelty dedup absent')

def main():
 ap=argparse.ArgumentParser();ap.add_argument('input',type=Path);ap.add_argument('output',type=Path);ap.add_argument('--report',type=Path);a=ap.parse_args();raw=a.input.read_bytes();h=sha(raw)
 if h!=EXPECTED_INPUT_SHA256: raise RuntimeError(f'refusing unexpected input {h}')
 d=json.loads(raw);by={m['id']:m for m in d['memories']}
 rs=by['policy.research']; st=dict(rs.get('state') or {}); st.update({'gain_uncertainty_scale':'0.25','satisfaction_uncertainty':'0.2','min_validation_count':'1','max_parallel_sources':'3','default_external_gain':'0.5','default_external_latency_sec':'0.1','source_reliability_weight':'1','source_gain_weight':'1','source_cost_weight':'1','source_latency_weight':'0.1'});rs['state']=st;rs['revision']=int(rs.get('revision',0))+1
 d['memories'] += [context_policy(),split_apply(),split_expire()]
 cf=by['context.fission.parent'];cf['program']=fission_program();cf['revision']=int(cf.get('revision',0))+1
 rc=by['research.strategy.context'];rc['program']=context_strategy();rc['revision']=int(rc.get('revision',0))+1
 for mid in ('prediction.birth.parent','prediction.audit.parent'):
  m=by[mid];m['program']=patch_prediction_context(m['program']);m['revision']=int(m.get('revision',0))+1
 rr=by['research.resolve.parent.impl.base'];rr['program']=resolve_program();rr['revision']=int(rr.get('revision',0))+1
 re=by['research.external.parent'];re['program']=external_program();re['revision']=int(re.get('revision',0))+1
 xe=by['external.evidence.ingest.parent'];xe['program']=ingest_program();xe['revision']=int(xe.get('revision',0))+1
 oc=by['outcome.credit.parent']; p=[]
 for op in oc['program']:
  p.append(op)
  if op.get('code')=='label' and op.get('a')=='oc_reassess':
   p.append({'code':'state_num_add','a':'{{oc_b}}','b':'validation_count','c':'1'})
 oc['program']=p;oc['revision']=int(oc.get('revision',0))+1
 d['version']='28.6-context-research-sources';audit(d);enc=(json.dumps(d,ensure_ascii=False,indent=2,sort_keys=True)+'\n').encode();a.output.write_bytes(enc)
 rep={'input_sha256':h,'output_sha256':sha(enc),'memory_count':len(d['memories'])}
 if a.report:a.report.write_text(json.dumps(rep,indent=2,sort_keys=True)+'\n')
 print(json.dumps(rep,indent=2,sort_keys=True))
if __name__=='__main__':main()
