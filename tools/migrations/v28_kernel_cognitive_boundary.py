#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, subprocess
from pathlib import Path
EXPECTED_INPUT_SHA256='8b43be53777e2964297a4f0c1f1df061c06d2a7f8e515636ff2367681420018e'
EXPECTED_OUTPUT_SHA256='a0ff31f5d5ed648c2dd74d924804bd22f28ada4359699201822f6c5bbb0fbfa7'
COGNITIVE_FIELDS=('Reuse','Trials','Successes','Reward','Cost','Stability')
def sha256(data:bytes)->str:return hashlib.sha256(data).hexdigest()
def replace_once(text,old,new,label):
    c=text.count(old)
    if c!=1: raise RuntimeError(f'{label}: expected exactly one match, got {c}')
    return text.replace(old,new,1)
def migrate(source:str)->str:
    source=replace_once(source,
        '\tReuse         uint64         `json:"reuse_count,omitempty"`\n\tTrials        uint64         `json:"real_trials,omitempty"`\n\tSuccesses     uint64         `json:"successes,omitempty"`\n\tReward        float64        `json:"total_reward,omitempty"`\n\tCost          float64        `json:"total_cost,omitempty"`\n\tStability     float64        `json:"stability,omitempty"`\n',
        '\tInputPattern     map[string]any `json:"input_pattern,omitempty"`\n\tOutputEffect     map[string]any `json:"output_effect,omitempty"`\n\tRuntimeExecCount uint64         `json:"runtime_exec_count,omitempty"`\n', 'memory cognitive fields')
    source=replace_once(source,'\t\tm.Reuse++\n','\t\tm.RuntimeExecCount++\n','reuse counter')
    start=source.find('\tcase "metric_add":'); end=source.find('\tcase "cache_list":',start)
    if start<0 or end<0: raise RuntimeError('metric_add case block not found')
    source=source[:start]+source[end:]
    source=replace_once(source,', Stability: .1, Revision: 1}',', Revision: 1}','new memory stability default')
    for line in ('\t\tchild.Reuse = 0\n','\t\tchild.Trials = 0\n','\t\tchild.Successes = 0\n','\t\tchild.Reward = 0\n','\t\tchild.Cost = 0\n'):
        source=replace_once(source,line,'',f'remove {line.strip()}')
    source=replace_once(source,
        '\tcase "reuse":\n\t\treturn strconv.FormatUint(m.Reuse, 10)\n\tcase "trials":\n\t\treturn strconv.FormatUint(m.Trials, 10)\n\tcase "successes":\n\t\treturn strconv.FormatUint(m.Successes, 10)\n\tcase "reward":\n\t\treturn ff(m.Reward)\n\tcase "cost":\n\t\treturn ff(m.Cost)\n\tcase "stability":\n\t\treturn ff(m.Stability)\n',
        '\tcase "runtime_exec_count":\n\t\treturn strconv.FormatUint(m.RuntimeExecCount, 10)\n','fieldString cognitive metrics')
    start=source.find('func metricAdd('); end=source.find('func num(',start)
    if start<0 or end<0: raise RuntimeError('metricAdd helper block not found')
    source=source[:start]+source[end:]
    fn='func mergeSameIdentity(dst, src *Memory) {'; start=source.find(fn); marker='\tif dst.State == nil {'; pos=source.find(marker,start)
    if start<0 or pos<0: raise RuntimeError('mergeSameIdentity cognitive merge block not found')
    source=source[:start]+fn+'\n'+source[pos:]
    return source
def audit(text:str):
    if 'case "metric_add"' in text or 'func metricAdd(' in text: raise RuntimeError('metric_add remains in Kernel')
    for f in COGNITIVE_FIELDS:
        if f'm.{f}' in text or f'src.{f}' in text or f'dst.{f}' in text: raise RuntimeError(f'Kernel still interprets cognitive field {f}')
    for j in ('reuse_count','real_trials','successes','total_reward','total_cost','stability'):
        if f'json:"{j}' in text: raise RuntimeError(f'Kernel struct still declares cognitive JSON field {j}')
    if 'InputPattern' not in text or 'OutputEffect' not in text or 'RuntimeExecCount' not in text: raise RuntimeError('required v28 fields missing')
def main():
    ap=argparse.ArgumentParser(); ap.add_argument('input',type=Path); ap.add_argument('output',type=Path); a=ap.parse_args()
    raw=a.input.read_bytes(); actual=sha256(raw)
    if actual!=EXPECTED_INPUT_SHA256: raise RuntimeError(f'refusing non-v27 kernel.go: {actual}')
    out=migrate(raw.decode()); audit(out); a.output.write_text(out)
    subprocess.run(['gofmt','-w',str(a.output)], check=True)
    got=sha256(a.output.read_bytes())
    if got!=EXPECTED_OUTPUT_SHA256: raise RuntimeError(f'unexpected output hash {got}, expected {EXPECTED_OUTPUT_SHA256}')
    print(f'ok input={actual} output={got}')
if __name__=='__main__': main()
