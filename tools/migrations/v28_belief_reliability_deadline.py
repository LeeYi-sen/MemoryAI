#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, json
from pathlib import Path

EXPECTED_INPUT_SHA256='c79cfe749fa4f274a61a9cea8dc9e8fe7f3bde10f44a4137df579ff1e66b2f5f'

def sha(b:bytes)->str:return hashlib.sha256(b).hexdigest()

def cal_update():
 return {'id':'source.calibration.update.parent','layer':'emergent','generation':29,'revision':1,
 'content':'Update contextual Source reliability from observed prediction outcomes, including negative credit on failures.',
 'tags':['memory','cog.source.calibration','cog.evolvable'],'capabilities':['memory.write'],
 'program':[
  {'code':'cmp_eq','a':'scu_nos','b':'{{source_id}}'},{'code':'jump_if','a':'{{scu_nos}}','b':'scu_end'},
  {'code':'set','a':'scu_raw','b':'{{source_id}}|{{context_key}}'},{'code':'sha256_text','a':'scu_raw','b':'scu_h'},
  {'code':'tag_list','a':'cog.source.calibration.key.{{scu_h}}','b':'scu_ls'},{'code':'list_len','a':'scu_ls','b':'scu_n'},
  {'code':'cmp_gt','a':'scu_have','b':'{{scu_n}}','c':'0'},{'code':'jump_if','a':'{{scu_have}}','b':'scu_reuse'},
  {'code':'memory_new','a':'calibration_id','args':{'layer':'acquired','content':'Contextual Source reliability learned from observed prediction outcomes.','parents':'{{source_id}},source.calibration.update.parent','tags':'memory,cog.source.calibration,cog.source.calibration.key.{{scu_h}}'}},
  {'code':'state_set','a':'{{calibration_id}}','b':'source_id','c':'{{source_id}}'},{'code':'state_set','a':'{{calibration_id}}','b':'context_key','c':'{{context_key}}'},{'code':'jump','a':'scu_write'},
  {'code':'label','a':'scu_reuse'},{'code':'list_get','a':'scu_ls','b':'0','c':'calibration_id'},
  {'code':'label','a':'scu_write'},{'code':'state_num_add','a':'{{calibration_id}}','b':'trials','c':'1','args':{'out':'scu_t'}},
  {'code':'cmp_eq','a':'scu_ok','b':'{{outcome_success}}','c':'1'},{'code':'jump_if','a':'{{scu_ok}}','b':'scu_success'},{'code':'jump','a':'scu_ratio'},
  {'code':'label','a':'scu_success'},{'code':'state_num_add','a':'{{calibration_id}}','b':'successes','c':'1','args':{'out':'scu_s'}},
  {'code':'label','a':'scu_ratio'},{'code':'state_get','a':'{{calibration_id}}','b':'trials','c':'scu_t'},{'code':'state_get','a':'{{calibration_id}}','b':'successes','c':'scu_s'},{'code':'var_default','a':'scu_s','b':'0'},
  {'code':'num_div','a':'source_reliability','b':'{{scu_s}}','c':'{{scu_t}}'},{'code':'state_set','a':'{{calibration_id}}','b':'success_rate','c':'{{source_reliability}}'},
  {'code':'state_get','a':'policy.belief','b':'min_calibration_trials','c':'scu_min'},{'code':'cmp_ge','a':'scu_cal','b':'{{scu_t}}','c':'{{scu_min}}'},
  {'code':'jump_if','a':'{{scu_cal}}','b':'scu_mark'},{'code':'jump','a':'scu_end'},
  {'code':'label','a':'scu_mark'},{'code':'memory_tag_add','a':'{{calibration_id}}','b':'cog.source.calibrated'},
  {'code':'label','a':'scu_end'}]}

def rel_compute():
 return {'id':'source.reliability.compute.parent','layer':'emergent','generation':29,'revision':1,
 'content':'Compute contextual Source reliability from Memory-owned calibration evidence; use a mutable prior until enough trials exist.',
 'tags':['memory','cog.source.reliability','cog.evolvable'],'program':[
  {'code':'state_get','a':'policy.belief','b':'default_source_reliability','c':'source_reliability'},{'code':'var_default','a':'source_reliability','b':'0.5'},{'code':'set','a':'source_calibrated','b':'0'},
  {'code':'cmp_eq','a':'src_nos','b':'{{source_id}}'},{'code':'jump_if','a':'{{src_nos}}','b':'src_end'},
  {'code':'set','a':'src_raw','b':'{{source_id}}|{{context_key}}'},{'code':'sha256_text','a':'src_raw','b':'src_h'},
  {'code':'tag_list','a':'cog.source.calibration.key.{{src_h}}','b':'src_ls'},{'code':'list_len','a':'src_ls','b':'src_n'},
  {'code':'cmp_gt','a':'src_have','b':'{{src_n}}','c':'0'},{'code':'jump_if','a':'{{src_have}}','b':'src_read'},{'code':'jump','a':'src_end'},
  {'code':'label','a':'src_read'},{'code':'list_get','a':'src_ls','b':'0','c':'src_cal'},
  {'code':'state_get','a':'{{src_cal}}','b':'trials','c':'src_t'},{'code':'var_default','a':'src_t','b':'0'},
  {'code':'state_get','a':'{{src_cal}}','b':'successes','c':'src_s'},{'code':'var_default','a':'src_s','b':'0'},
  {'code':'state_get','a':'policy.belief','b':'min_calibration_trials','c':'src_min'},{'code':'cmp_ge','a':'src_enough','b':'{{src_t}}','c':'{{src_min}}'},
  {'code':'jump_if','a':'{{src_enough}}','b':'src_ratio'},{'code':'jump','a':'src_end'},
  {'code':'label','a':'src_ratio'},{'code':'num_div','a':'source_reliability','b':'{{src_s}}','c':'{{src_t}}'},{'code':'set','a':'source_calibrated','b':'1'},
  {'code':'label','a':'src_end'}]}

def belief_update_program(old):
 idx=next(i for i,o in enumerate(old) if o.get('code')=='num_mul' and o.get('a')=='bu_w')
 end=next(i for i,o in enumerate(old[idx:],idx) if o.get('code')=='emit_event' and o.get('a')=='belief.supported')
 repl=[
  {'code':'set','a':'bu_w','b':'0'},{'code':'state_list_len','a':'{{belief_id}}','b':'support_sources','c':'bu_sn'},{'code':'set','a':'bu_i','b':'0'},
  {'code':'label','a':'bu_rel_loop'},{'code':'cmp_ge','a':'bu_rel_done','b':'{{bu_i}}','c':'{{bu_sn}}'},{'code':'jump_if','a':'{{bu_rel_done}}','b':'bu_rel_finish'},
  {'code':'state_list_get','a':'{{belief_id}}','b':'support_sources','c':'bu_rsrc','args':{'index':'{{bu_i}}'}},
  {'code':'copy','a':'source_id','b':'bu_rsrc'},{'code':'copy','a':'context_key','b':'bu_key'},{'code':'call','a':'source.reliability.compute.parent'},
  {'code':'num_add','a':'bu_w','b':'{{bu_w}}','c':'{{source_reliability}}'},{'code':'num_add','a':'bu_i','b':'{{bu_i}}','c':'1'},{'code':'jump','a':'bu_rel_loop'},
  {'code':'label','a':'bu_rel_finish'},{'code':'state_set','a':'{{belief_id}}','b':'support_weight','c':'{{bu_w}}'},
  {'code':'num_div','a':'bu_avg','b':'{{bu_w}}','c':'{{bu_sn}}'},{'code':'state_set','a':'{{belief_id}}','b':'average_source_reliability','c':'{{bu_avg}}'},
  {'code':'emit_event','a':'belief.supported','b':'{{belief_id}}'}]
 return old[:idx]+repl+old[end+1:]

def reassess_program():
 return [
  {'code':'state_list_len','a':'{{belief_id}}','b':'support_sources','c':'br_n'},{'code':'set','a':'br_i','b':'0'},{'code':'set','a':'br_cal','b':'0'},{'code':'set','a':'br_relcal','b':'0'},{'code':'set','a':'br_weight','b':'0'},
  {'code':'state_get','a':'{{belief_id}}','b':'relation_key','c':'br_ctx'},{'code':'state_get','a':'policy.belief','b':'min_source_success_rate','c':'br_minrate'},
  {'code':'label','a':'br_loop'},{'code':'cmp_ge','a':'br_done','b':'{{br_i}}','c':'{{br_n}}'},{'code':'jump_if','a':'{{br_done}}','b':'br_eval'},
  {'code':'state_list_get','a':'{{belief_id}}','b':'support_sources','c':'br_src','args':{'index':'{{br_i}}'}},{'code':'copy','a':'source_id','b':'br_src'},{'code':'copy','a':'context_key','b':'br_ctx'},{'code':'call','a':'source.reliability.compute.parent'},
  {'code':'num_add','a':'br_weight','b':'{{br_weight}}','c':'{{source_reliability}}'},
  {'code':'cmp_eq','a':'br_iscal','b':'{{source_calibrated}}','c':'1'},{'code':'jump_if','a':'{{br_iscal}}','b':'br_cal_inc'},{'code':'jump','a':'br_next'},
  {'code':'label','a':'br_cal_inc'},{'code':'num_add','a':'br_cal','b':'{{br_cal}}','c':'1'},{'code':'cmp_ge','a':'br_reliable','b':'{{source_reliability}}','c':'{{br_minrate}}'},{'code':'jump_if','a':'{{br_reliable}}','b':'br_rel_inc'},{'code':'jump','a':'br_next'},
  {'code':'label','a':'br_rel_inc'},{'code':'num_add','a':'br_relcal','b':'{{br_relcal}}','c':'1'},
  {'code':'label','a':'br_next'},{'code':'num_add','a':'br_i','b':'{{br_i}}','c':'1'},{'code':'jump','a':'br_loop'},
  {'code':'label','a':'br_eval'},{'code':'state_set','a':'{{belief_id}}','b':'calibrated_sources','c':'{{br_cal}}'},{'code':'state_set','a':'{{belief_id}}','b':'reliable_calibrated_sources','c':'{{br_relcal}}'},{'code':'state_set','a':'{{belief_id}}','b':'reliability_weight','c':'{{br_weight}}'},
  {'code':'cmp_gt','a':'br_has','b':'{{br_n}}','c':'0'},{'code':'jump_if','a':'{{br_has}}','b':'br_avg'},{'code':'set','a':'br_avgval','b':'0'},{'code':'jump','a':'br_gate'},
  {'code':'label','a':'br_avg'},{'code':'num_div','a':'br_avgval','b':'{{br_weight}}','c':'{{br_n}}'},
  {'code':'label','a':'br_gate'},{'code':'state_set','a':'{{belief_id}}','b':'average_source_reliability','c':'{{br_avgval}}'},
  {'code':'state_get','a':'{{belief_id}}','b':'independent_sources','c':'br_ind'},{'code':'state_get','a':'policy.belief','b':'stable_sources','c':'br_mini'},{'code':'state_get','a':'policy.belief','b':'stable_calibrated_sources','c':'br_minc'},{'code':'state_get','a':'policy.belief','b':'stable_avg_reliability','c':'br_minavg'},
  {'code':'cmp_ge','a':'br_iok','b':'{{br_ind}}','c':'{{br_mini}}'},{'code':'cmp_ge','a':'br_cok','b':'{{br_relcal}}','c':'{{br_minc}}'},{'code':'cmp_ge','a':'br_aok','b':'{{br_avgval}}','c':'{{br_minavg}}'},
  {'code':'jump_if','a':'{{br_iok}}','b':'br_cgate'},{'code':'jump','a':'br_end'},{'code':'label','a':'br_cgate'},{'code':'jump_if','a':'{{br_cok}}','b':'br_agate'},{'code':'jump','a':'br_end'},{'code':'label','a':'br_agate'},{'code':'jump_if','a':'{{br_aok}}','b':'br_stable'},{'code':'jump','a':'br_end'},
  {'code':'label','a':'br_stable'},{'code':'state_set','a':'{{belief_id}}','b':'status','c':'stable'},{'code':'memory_tag_remove','a':'{{belief_id}}','b':'cog.belief.supported'},{'code':'memory_tag_remove','a':'{{belief_id}}','b':'cog.belief.revising'},{'code':'memory_tag_add','a':'{{belief_id}}','b':'cog.belief.stable'},{'code':'emit_event','a':'belief.stable','b':'{{belief_id}}'},{'code':'label','a':'br_end'}]

def outcome_program():
 return [
  {'code':'copy','a':'oc_pred','b':'__event.prediction_id'},{'code':'copy','a':'oc_success','b':'__event.outcome_success'},{'code':'copy','a':'oc_outsrc','b':'__event.outcome_source_id'},
  {'code':'state_get','a':'{{oc_pred}}','b':'belief_id','c':'oc_b'},{'code':'state_set','a':'{{oc_pred}}','b':'status','c':'resolved'},{'code':'state_set','a':'{{oc_pred}}','b':'success','c':'{{oc_success}}'},{'code':'state_set','a':'{{oc_pred}}','b':'outcome_source_id','c':'{{oc_outsrc}}'},
  {'code':'memory_tag_remove','a':'{{oc_pred}}','b':'cog.prediction.pending'},{'code':'memory_tag_remove','a':'{{oc_pred}}','b':'cog.prediction.pending.belief.{{oc_b}}'},{'code':'memory_tag_add','a':'{{oc_pred}}','b':'cog.prediction.resolved'},
  {'code':'state_get','a':'{{oc_b}}','b':'relation_key','c':'oc_ctx'},{'code':'state_list_len','a':'{{oc_pred}}','b':'support_sources_snapshot','c':'oc_n'},{'code':'set','a':'oc_i','b':'0'},
  {'code':'label','a':'oc_loop'},{'code':'cmp_ge','a':'oc_done','b':'{{oc_i}}','c':'{{oc_n}}'},{'code':'jump_if','a':'{{oc_done}}','b':'oc_after_credit'},
  {'code':'state_list_get','a':'{{oc_pred}}','b':'support_sources_snapshot','c':'oc_src','args':{'index':'{{oc_i}}'}},{'code':'cmp_eq','a':'oc_self','b':'{{oc_src}}','c':'{{oc_outsrc}}'},{'code':'jump_if','a':'{{oc_self}}','b':'oc_next'},
  {'code':'copy','a':'source_id','b':'oc_src'},{'code':'copy','a':'context_key','b':'oc_ctx'},{'code':'copy','a':'outcome_success','b':'oc_success'},{'code':'call','a':'source.calibration.update.parent'},
  {'code':'label','a':'oc_next'},{'code':'num_add','a':'oc_i','b':'{{oc_i}}','c':'1'},{'code':'jump','a':'oc_loop'},
  {'code':'label','a':'oc_after_credit'},{'code':'cmp_eq','a':'oc_ok','b':'{{oc_success}}','c':'1'},{'code':'jump_if','a':'{{oc_ok}}','b':'oc_reassess'},
  {'code':'memory_tag_add','a':'{{oc_pred}}','b':'cog.prediction.failed'},{'code':'state_set','a':'{{oc_b}}','b':'status','c':'revising'},{'code':'state_set','a':'{{oc_b}}','b':'uncertainty','c':'1'},{'code':'memory_tag_remove','a':'{{oc_b}}','b':'cog.belief.stable'},{'code':'memory_tag_remove','a':'{{oc_b}}','b':'cog.belief.supported'},{'code':'memory_tag_add','a':'{{oc_b}}','b':'cog.belief.revising'},
  {'code':'memory_new','a':'oc_conf','args':{'content':'Prediction failure reopens a Belief; contradiction is preserved for new research.','layer':'emergent','parents':'{{oc_b}},{{oc_pred}},outcome.credit.parent','tags':'memory,cog.memory.conflict'}},{'code':'state_set','a':'{{oc_conf}}','b':'belief_id','c':'{{oc_b}}'},{'code':'emit_event','a':'belief.reopened','b':'{{oc_b}}'},{'code':'emit_event','a':'belief.conflict','b':'{{oc_conf}}'},{'code':'jump','a':'oc_transition'},
  {'code':'label','a':'oc_reassess'},{'code':'copy','a':'belief_id','b':'oc_b'},{'code':'call','a':'belief.reassess.parent'},
  {'code':'label','a':'oc_transition'},{'code':'copy','a':'transition_prediction_id','b':'oc_pred'},{'code':'copy','a':'transition_success','b':'oc_success'},{'code':'copy','a':'transition_observed_object_id','b':'__event.observed_object_id'},{'code':'copy','a':'transition_intervention','b':'__event.outcome_intervention'},{'code':'emit_event','a':'transition.outcome','b':'{{oc_pred}}','args':{'vars':'transition_prediction_id,transition_success,transition_observed_object_id,transition_intervention'}},{'code':'label','a':'oc_end'}]

def deadline_memory():
 return {'id':'prediction.deadline.parent','layer':'emergent','generation':29,'revision':1,
 'content':'Consume pending Prediction deadlines and turn missing outcomes into explicit uncertainty and retry/research pressure.',
 'tags':['memory','cog.prediction','runtime.trigger','cog.evolvable'],'trigger':['event:idle'],
 'capabilities':['event.emit','memory.write'],'program':[
  {'code':'tag_list','a':'cog.prediction.pending','b':'pd_ps'},{'code':'list_len','a':'pd_ps','b':'pd_n'},{'code':'set','a':'pd_i','b':'0'},{'code':'time_unix','a':'pd_now'},
  {'code':'label','a':'pd_loop'},{'code':'cmp_ge','a':'pd_done','b':'{{pd_i}}','c':'{{pd_n}}'},{'code':'jump_if','a':'{{pd_done}}','b':'pd_end'},
  {'code':'list_get','a':'pd_ps','b':'{{pd_i}}','c':'pd_p'},{'code':'state_get','a':'{{pd_p}}','b':'status','c':'pd_st'},{'code':'cmp_eq','a':'pd_pending','b':'{{pd_st}}','c':'pending'},{'code':'jump_if','a':'{{pd_pending}}','b':'pd_deadline'},{'code':'jump','a':'pd_next'},
  {'code':'label','a':'pd_deadline'},{'code':'state_get','a':'{{pd_p}}','b':'deadline_unix','c':'pd_d'},{'code':'cmp_ge','a':'pd_due','b':'{{pd_now}}','c':'{{pd_d}}'},{'code':'jump_if','a':'{{pd_due}}','b':'pd_overdue'},{'code':'jump','a':'pd_next'},
  {'code':'label','a':'pd_overdue'},{'code':'state_set','a':'{{pd_p}}','b':'status','c':'overdue'},{'code':'state_num_add','a':'{{pd_p}}','b':'retry_count','c':'1','args':{'out':'pd_retry'}},{'code':'memory_tag_remove','a':'{{pd_p}}','b':'cog.prediction.pending'},{'code':'memory_tag_add','a':'{{pd_p}}','b':'cog.prediction.overdue'},
  {'code':'state_get','a':'{{pd_p}}','b':'belief_id','c':'pd_b'},{'code':'state_set','a':'{{pd_b}}','b':'status','c':'revising'},{'code':'state_set','a':'{{pd_b}}','b':'uncertainty','c':'1'},{'code':'memory_tag_remove','a':'{{pd_b}}','b':'cog.belief.stable'},{'code':'memory_tag_remove','a':'{{pd_b}}','b':'cog.belief.supported'},{'code':'memory_tag_add','a':'{{pd_b}}','b':'cog.belief.revising'},
  {'code':'emit_event','a':'prediction.overdue','b':'{{pd_p}}'},{'code':'emit_event','a':'belief.reopened','b':'{{pd_b}}'},
  {'code':'label','a':'pd_next'},{'code':'num_add','a':'pd_i','b':'{{pd_i}}','c':'1'},{'code':'jump','a':'pd_loop'},{'code':'label','a':'pd_end'}]}

def audit(d):
 by={m['id']:m for m in d['memories']}
 for x in ('source.calibration.update.parent','source.reliability.compute.parent','prediction.deadline.parent'):
  if x not in by: raise RuntimeError(x+' missing')
 if 'source.reliability.compute.parent' not in json.dumps(by['belief.update.parent']['program']): raise RuntimeError('belief update not reliability weighted')
 if 'min_source_success_rate' not in json.dumps(by['belief.reassess.parent.impl.base']['program']): raise RuntimeError('belief stable gate not reliability aware')
 if 'source.calibration.update.parent' not in json.dumps(by['outcome.credit.parent']['program']): raise RuntimeError('prediction outcomes do not calibrate source')
 if 'deadline_unix' not in json.dumps(by['prediction.deadline.parent']['program']): raise RuntimeError('deadline not consumed')

def main():
 ap=argparse.ArgumentParser();ap.add_argument('input',type=Path);ap.add_argument('output',type=Path);ap.add_argument('--report',type=Path);a=ap.parse_args();raw=a.input.read_bytes();h=sha(raw)
 if h!=EXPECTED_INPUT_SHA256:raise RuntimeError(f'refusing unexpected input {h}')
 d=json.loads(raw);by={m['id']:m for m in d['memories']}
 pol=by['policy.belief'];s=dict(pol.get('state') or {});s.update({'default_source_reliability':'0.5','min_calibration_trials':'2','min_source_success_rate':'0.6','stable_avg_reliability':'0.6'});pol['state']=s;pol['revision']=int(pol.get('revision',0))+1
 d['memories'] += [cal_update(),rel_compute(),deadline_memory()]
 bu=by['belief.update.parent'];bu['program']=belief_update_program(bu['program']);bu['revision']=int(bu.get('revision',0))+1
 br=by['belief.reassess.parent.impl.base'];br['program']=reassess_program();br['revision']=int(br.get('revision',0))+1
 oc=by['outcome.credit.parent'];oc['program']=outcome_program();oc['revision']=int(oc.get('revision',0))+1
 d['version']='28.5-belief-reliability-deadline';audit(d);enc=(json.dumps(d,ensure_ascii=False,indent=2,sort_keys=True)+'\n').encode();a.output.write_bytes(enc)
 report={'input_sha256':h,'output_sha256':sha(enc),'memory_count':len(d['memories'])}
 if a.report:a.report.write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
 print(json.dumps(report,indent=2,sort_keys=True))
if __name__=='__main__':main()
