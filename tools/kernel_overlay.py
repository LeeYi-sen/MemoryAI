#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import re

V27_KERNEL_SHA256 = "8b43be53777e2964297a4f0c1f1df061c06d2a7f8e515636ff2367681420018e"


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"kernel overlay {label}: expected exactly one match, got {count}")
    return text.replace(old, new, 1)


def _sub_once(text: str, pattern: str, repl: str, label: str) -> str:
    out, count = re.subn(pattern, repl, text, count=1, flags=re.S)
    if count != 1:
        raise RuntimeError(f"kernel overlay {label}: expected exactly one match, got {count}")
    return out


def apply_kernel_overlay(raw: bytes) -> bytes:
    """Apply current ABI/boundary fixes to the exact v27 bootstrap.

    Historical bootstrap bytes remain immutable and hash-verifiable. This
    overlay is the auditable forward delta used to reconstruct current Kernel
    source. Cognitive metrics remain passive legacy JSON compatibility data;
    Kernel never mutates, ranks, merges or exposes them as execution policy.
    """
    got = hashlib.sha256(raw).hexdigest()
    if got != V27_KERNEL_SHA256:
        raise RuntimeError(
            f"kernel overlay requires exact v27 source {V27_KERNEL_SHA256}, got {got}"
        )

    text = raw.decode("utf-8")

    text = _replace_once(
        text,
        'const imageVersion = "27.0.0-memory-fabric-sovereign"',
        'const imageVersion = "28.8.0-memory-fabric-sovereign"',
        "image version",
    )

    old_memory_struct = '''type Memory struct {
\tID            string         `json:"id"`
\tLayer         string         `json:"layer"`
\tGeneration    int            `json:"generation"`
\tParents       []string       `json:"parents,omitempty"`
\tTags          []string       `json:"tags,omitempty"`
\tContent       string         `json:"content,omitempty"`
\tTrigger       []string       `json:"trigger,omitempty"`
\tCapabilities  []string       `json:"capabilities,omitempty"`
\tCapabilitySig string         `json:"capability_sig,omitempty"`
\tBudget        ResourceBudget `json:"budget,omitempty"`
\tRevision      uint64         `json:"revision,omitempty"`
\tProgram       []Op           `json:"program,omitempty"`
\tState         map[string]any `json:"state,omitempty"`
\tReuse         uint64         `json:"reuse_count,omitempty"`
\tTrials        uint64         `json:"real_trials,omitempty"`
\tSuccesses     uint64         `json:"successes,omitempty"`
\tReward        float64        `json:"total_reward,omitempty"`
\tCost          float64        `json:"total_cost,omitempty"`
\tStability     float64        `json:"stability,omitempty"`
\tCreatedUnix   int64          `json:"created_unix,omitempty"`
}'''
    new_memory_struct = '''type Memory struct {
\tID               string         `json:"id"`
\tLayer            string         `json:"layer"`
\tGeneration       int            `json:"generation"`
\tParents          []string       `json:"parents,omitempty"`
\tTags             []string       `json:"tags,omitempty"`
\tContent          string         `json:"content,omitempty"`
\tTrigger          []string       `json:"trigger,omitempty"`
\tCapabilities     []string       `json:"capabilities,omitempty"`
\tCapabilitySig    string         `json:"capability_sig,omitempty"`
\tBudget           ResourceBudget `json:"budget,omitempty"`
\tRevision         uint64         `json:"revision,omitempty"`
\tProgram          []Op           `json:"program,omitempty"`
\tState            map[string]any `json:"state,omitempty"`
\tRuntimeExecCount uint64         `json:"runtime_exec_count,omitempty"`
\tReuse            uint64         `json:"reuse_count,omitempty"`
\tTrials           uint64         `json:"real_trials,omitempty"`
\tSuccesses        uint64         `json:"successes,omitempty"`
\tReward           float64        `json:"total_reward,omitempty"`
\tCost             float64        `json:"total_cost,omitempty"`
\tStability        float64        `json:"stability,omitempty"`
\tCreatedUnix      int64          `json:"created_unix,omitempty"`
}'''
    text = _replace_once(
        text, old_memory_struct, new_memory_struct, "physical execution telemetry ABI"
    )

    old_run = '''\t// Surface/input memories are transport structures. Counting every raw surface
\t// traversal as cognitive reuse both distorts semantic credit and serializes the
\t// language hot path on dataMu. Their programs are immutable during a daemon
\t// read epoch, so copy under a read lock and leave durable reuse/trace accounting
\t// to actual cognitive structures.
\tsurfaceTransport := false
\tfor _, tag := range m.Tags {
\t\tif strings.HasPrefix(tag, "cog.surface.") || strings.HasPrefix(tag, "cog.input.") {
\t\t\tsurfaceTransport = true
\t\t\tbreak
\t\t}
\t}
\tvar program []Op
\tif surfaceTransport {
\t\te.dataMu.RLock()
\t\tprogram = append([]Op(nil), m.Program...)
\t\te.dataMu.RUnlock()
\t} else {
\t\te.dataMu.Lock()
\t\tm.Reuse++
\t\tprogram = append([]Op(nil), m.Program...)
\t\te.dataMu.Unlock()
\t\te.mu.Lock()
\t\te.trace = append(e.trace, m.ID)
\t\te.mu.Unlock()
\t}
'''
    new_run = '''\t// Execution count is physical runtime telemetry only. Semantic credit,
\t// confidence, utility and reuse are Memory-owned state and are never updated
\t// implicitly by Kernel execution.
\tsurfaceTransport := false
\tfor _, tag := range m.Tags {
\t\tif strings.HasPrefix(tag, "cog.surface.") || strings.HasPrefix(tag, "cog.input.") {
\t\t\tsurfaceTransport = true
\t\t\tbreak
\t\t}
\t}
\te.dataMu.Lock()
\tm.RuntimeExecCount++
\tprogram := append([]Op(nil), m.Program...)
\te.dataMu.Unlock()
\tif !surfaceTransport {
\t\te.mu.Lock()
\t\te.trace = append(e.trace, m.ID)
\t\te.mu.Unlock()
\t}
'''
    text = _replace_once(text, old_run, new_run, "execution accounting")

    text = _sub_once(
        text,
        r'\n\tcase "metric_add":.*?\n\tcase "cache_list":',
        '\n\tcase "cache_list":',
        "remove cognitive metric primitive",
    )

    text = _replace_once(
        text,
        '\t\tchild.Reuse = 0\n'
        '\t\tchild.Trials = 0\n'
        '\t\tchild.Successes = 0\n'
        '\t\tchild.Reward = 0\n'
        '\t\tchild.Cost = 0\n',
        '\t\tchild.RuntimeExecCount = 0\n'
        '\t\t// Legacy cognitive metric fields are passive compatibility data only.\n'
        '\t\tchild.Reuse = 0\n'
        '\t\tchild.Trials = 0\n'
        '\t\tchild.Successes = 0\n'
        '\t\tchild.Reward = 0\n'
        '\t\tchild.Cost = 0\n',
        "memory copy runtime telemetry reset",
    )

    text = _sub_once(
        text,
        r'\n\tcase "reuse":\n\t\treturn strconv\.FormatUint\(m\.Reuse, 10\).*?\n\tcase "content":',
        '\n\tcase "runtime_exec_count":\n'
        '\t\treturn strconv.FormatUint(m.RuntimeExecCount, 10)\n'
        '\tcase "content":',
        "remove cognitive metric field projection",
    )

    text = _sub_once(
        text,
        r'\nfunc metricAdd\(m \*Memory, k string, d float64\) \{.*?\n\}\nfunc max0',
        '\nfunc max0',
        "remove cognitive metric helper",
    )

    text = _sub_once(
        text,
        r'func mergeSameIdentity\(dst, src \*Memory\) \{.*?\n\tif dst\.State == nil \{',
        'func mergeSameIdentity(dst, src *Memory) {\n'
        '\t// RuntimeExecCount is physical monotonic telemetry. All semantic\n'
        '\t// reconciliation lives in Memory State/programs rather than Kernel.\n'
        '\tif src.RuntimeExecCount > dst.RuntimeExecCount {\n'
        '\t\tdst.RuntimeExecCount = src.RuntimeExecCount\n'
        '\t}\n'
        '\tif dst.State == nil {',
        "remove cognitive merge policy",
    )

    forbidden = (
        'case "metric_add"',
        'metricAdd(',
        'm.Reuse++',
        'case "reuse":',
        'case "trials":',
        'case "successes":',
        'case "reward":',
        'case "cost":',
        'case "stability":',
        'if src.Reuse > dst.Reuse',
        'if src.Reward > dst.Reward',
        'if src.Stability > dst.Stability',
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"kernel overlay left cognitive metric logic in Kernel: {bad}")
    if "RuntimeExecCount uint64" not in text or "m.RuntimeExecCount++" not in text:
        raise RuntimeError("kernel overlay failed to install physical execution telemetry")

    return text.encode("utf-8")
