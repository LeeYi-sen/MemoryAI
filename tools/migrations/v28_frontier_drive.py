#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, json
from pathlib import Path

EXPECTED_INPUT_SHA256 = '3689e0d596b75eeb58a9d018fe24c75f492928b8a62cad68418013bb92c9642c'
FRONTIERS = {
    'goal.frontier.experience.parent': ('cog.experience.unresolved', 'experience'),
    'goal.frontier.concept.parent': ('cog.concept.candidate', 'concept'),
    'goal.frontier.belief.parent': ('cog.belief.provisional', 'belief'),
    'goal.frontier.revising.parent': ('cog.belief.revising', 'belief-revision'),
    'goal.frontier.dimension.parent': ('cog.dimension.candidate', 'dimension'),
    'goal.frontier.cognition.parent': ('cog.frontier.cognition', 'cognition'),
    'goal.frontier.causal.parent': ('cog.frontier.causal', 'causal'),
}
DRIVE_FACTORS = (
    ('uncertainty','w_uncertainty','1'),
    ('prediction_error','w_prediction_error','0'),
    ('conflict','w_conflict','0'),
    ('curiosity','w_curiosity','0'),
    ('novelty','w_novelty','0'),
    ('goal_pressure','w_goal','1'),
)

def sha256_bytes(b: bytes) -> str: return hashlib.sha256(b).hexdigest()

def frontier_program(tag: str, kind: str, parent: str) -> list[dict]:
    return [
        {'code':'tag_list','a':tag,'b':'fg_fs'},
        {'code':'list_len','a':'fg_fs','b':'fg_n'},
        {'code':'set','a':'fg_i','b':'0'},
        {'code':'set','a':'fg_created','b':'0'},
        {'code':'label','a':'fg_loop'},
        {'code':'cmp_ge','a':'fg_done','b':'{{fg_i}}','c':'{{fg_n}}'},
        {'code':'jump_if','a':'{{fg_done}}','b':'fg_end'},
        {'code':'list_get','a':'fg_fs','b':'{{fg_i}}','c':'fg_f'},
        {'code':'set','a':'fg_keyraw','b':'{{fg_f}}|'+kind},
        {'code':'sha256_text','a':'fg_keyraw','b':'fg_h'},
        {'code':'tag_list','a':'cog.goal.focus.{{fg_h}}','b':'fg_old'},
        {'code':'list_len','a':'fg_old','b':'fg_on'},
        {'code':'cmp_gt','a':'fg_exists','b':'{{fg_on}}','c':'0'},
        {'code':'jump_if','a':'{{fg_exists}}','b':'fg_next'},
        {'code':'memory_new','a':'fg_g','args':{
            'content':'Internal Goal born from a real unresolved Memory frontier.',
            'layer':'emergent','parents':'{{fg_f}},'+parent,
            'tags':'memory,cog.goal,cog.goal.active,cog.goal.focus.{{fg_h}}'}},
        {'code':'state_set','a':'{{fg_g}}','b':'focus_id','c':'{{fg_f}}'},
        {'code':'state_set','a':'{{fg_g}}','b':'frontier_kind','c':kind},
        {'code':'state_set','a':'{{fg_g}}','b':'uncertainty','c':'1'},
        {'code':'state_set','a':'{{fg_g}}','b':'goal_pressure','c':'1'},
        {'code':'state_set','a':'{{fg_g}}','b':'status','c':'active'},
        {'code':'state_set','a':'{{fg_g}}','b':'research_state','c':'ready'},
        {'code':'num_add','a':'fg_created','b':'{{fg_created}}','c':'1'},
        {'code':'emit_event','a':'goal.created','b':'{{fg_g}}'},
        {'code':'label','a':'fg_next'},
        {'code':'num_add','a':'fg_i','b':'{{fg_i}}','c':'1'},
        {'code':'jump','a':'fg_loop'},
        {'code':'label','a':'fg_end'},
    ]

def clamp_program() -> list[dict]:
    p=[{'code':'state_get','a':'policy.drive','b':'min_weight','c':'dw_min'},
       {'code':'var_default','a':'dw_min','b':'0.05'}]
    for i,(_, w, _) in enumerate(DRIVE_FACTORS):
        nxt=f'dw_next_{i}'
        p += [
            {'code':'state_get','a':'policy.drive','b':w,'c':f'dw_w{i}'},
            {'code':'var_default','a':f'dw_w{i}','b':'1'},
            {'code':'cmp_gt','a':f'dw_low{i}','b':'{{dw_min}}','c':'{{dw_w'+str(i)+'}}'},
            {'code':'jump_if','a':'{{dw_low'+str(i)+'}}','b':f'dw_set_{i}'},
            {'code':'jump','a':nxt},
            {'code':'label','a':f'dw_set_{i}'},
            {'code':'state_set','a':'policy.drive','b':w,'c':'{{dw_min}}'},
            {'code':'label','a':nxt},
        ]
    return p

def drive_program() -> list[dict]:
    p=[{'code':'call','a':'drive.weight.clamp.parent'}]
    for i,(state,w,default) in enumerate(DRIVE_FACTORS):
        p += [
            {'code':'state_get','a':'{{__subject}}','b':state,'c':f'gd_x{i}'},
            {'code':'var_default','a':f'gd_x{i}','b':default},
            {'code':'state_get','a':'policy.drive','b':w,'c':f'gd_w{i}'},
            {'code':'num_mul','a':f'gd_t{i}','b':'{{gd_x'+str(i)+'}}','c':'{{gd_w'+str(i)+'}}'},
        ]
    p += [{'code':'set','a':'gd_score','b':'0'}]
    for i in range(len(DRIVE_FACTORS)):
        p.append({'code':'num_add','a':'gd_score','b':'{{gd_score}}','c':'{{gd_t'+str(i)+'}}'})
    p += [
        {'code':'state_set','a':'{{__subject}}','b':'drive_score','c':'{{gd_score}}'},
        {'code':'state_set','a':'{{__subject}}','b':'drive_uncertainty','c':'{{gd_t0}}'},
        {'code':'state_set','a':'{{__subject}}','b':'drive_prediction_error','c':'{{gd_t1}}'},
        {'code':'state_set','a':'{{__subject}}','b':'drive_conflict','c':'{{gd_t2}}'},
        {'code':'state_set','a':'{{__subject}}','b':'drive_curiosity','c':'{{gd_t3}}'},
        {'code':'state_set','a':'{{__subject}}','b':'drive_novelty','c':'{{gd_t4}}'},
        {'code':'state_set','a':'{{__subject}}','b':'drive_goal','c':'{{gd_t5}}'},
    ]
    return p

def extend_feedback(program: list[dict]) -> list[dict]:
    out=[]; i=0
    while i < len(program):
        op=program[i]
        if op.get('code')=='state_get' and op.get('a')=='policy.drive' and op.get('b')=='learning_rate':
            out.append(op); i += 1
            while i < len(program) and not (program[i].get('code')=='label' and program[i].get('a')=='pa_end'):
                i += 1
            for j,(state,w,default) in enumerate(DRIVE_FACTORS):
                out += [
                    {'code':'state_get','a':'{{pa_g}}','b':state,'c':f'pa_x{j}'},
                    {'code':'var_default','a':f'pa_x{j}','b':default},
                    {'code':'num_mul','a':f'pa_dx{j}','b':'{{pa_net}}','c':'{{pa_lr}}'},
                    {'code':'num_mul','a':f'pa_dx{j}','b':'{{pa_dx'+str(j)+'}}','c':'{{pa_x'+str(j)+'}}'},
                    {'code':'state_num_add','a':'policy.drive','b':w,'c':'{{pa_dx'+str(j)+'}}'},
                ]
            out.append({'code':'call','a':'drive.weight.clamp.parent'})
            continue
        out.append(op); i += 1
    return out

def migrate(doc: dict) -> dict:
    ms=doc['memories']; by={m['id']:m for m in ms}
    for mid,(tag,kind) in FRONTIERS.items():
        m=by[mid]; m['program']=frontier_program(tag,kind,mid); m['revision']=int(m.get('revision',0))+1
    m=by['goal.drive.parent']; m['program']=drive_program(); m['revision']=int(m.get('revision',0))+1
    m=by['policy.feedback.adapt.parent']; m['program']=extend_feedback(m['program']); m['revision']=int(m.get('revision',0))+1
    if 'drive.weight.clamp.parent' not in by:
        ms.append({
            'id':'drive.weight.clamp.parent','content':'Memory-owned lower-bound enforcement for mutable Drive weights.',
            'tags':['memory','cog.drive','cog.policy','cog.evolvable'], 'parents':['policy.drive'],
            'layer':'emergent','generation':1,'revision':1,'executable':True,
            'capabilities':['memory.write'],'program':clamp_program(),
            'input_pattern':{'policy':'policy.drive'},'output_effect':{'weights':'clamped-to-min_weight'},
            'state':{'policy_owner':'memory'}
        })
    doc['version']='28.4-frontier-drive'
    return doc

def audit(doc: dict) -> None:
    by={m['id']:m for m in doc['memories']}
    for mid in FRONTIERS:
        p=by[mid]['program']
        if any(op.get('code')=='list_get' and op.get('b')=='0' for op in p): raise RuntimeError(mid+' still index-0 only')
        if not any(op.get('code')=='jump' and op.get('a')=='fg_loop' for op in p): raise RuntimeError(mid+' no full loop')
    p=by['goal.drive.parent']['program']; s=json.dumps(p)
    for state,w,_ in DRIVE_FACTORS:
        if state not in s or w not in s: raise RuntimeError('drive factor missing '+state+'/'+w)
    if 'drive.weight.clamp.parent' not in by: raise RuntimeError('clamp structure missing')
    if 'min_weight' not in json.dumps(by['drive.weight.clamp.parent']): raise RuntimeError('min_weight unused')

def main():
    ap=argparse.ArgumentParser(); ap.add_argument('input',type=Path); ap.add_argument('output',type=Path); ap.add_argument('--report',type=Path); a=ap.parse_args()
    raw=a.input.read_bytes(); got=sha256_bytes(raw)
    if got != EXPECTED_INPUT_SHA256: raise RuntimeError(f'refusing non-v28.3 input {got}')
    doc=migrate(json.loads(raw)); audit(doc)
    enc=(json.dumps(doc,ensure_ascii=False,indent=2,sort_keys=True)+'\n').encode(); a.output.write_bytes(enc)
    report={'input_sha256':got,'output_sha256':sha256_bytes(enc),'memory_count':len(doc['memories']),'frontiers':len(FRONTIERS),'drive_factors':len(DRIVE_FACTORS)}
    if a.report: a.report.write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
    print(json.dumps(report,indent=2,sort_keys=True))
if __name__=='__main__': main()
