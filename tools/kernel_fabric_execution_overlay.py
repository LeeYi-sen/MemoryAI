#!/usr/bin/env python3
from __future__ import annotations


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"fabric-execution overlay {label}: expected one match, got {count}")
    return text.replace(old, new, 1)


def apply_kernel_fabric_execution_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "RuntimeExecCount uint64",
        "func (e *Engine) run(",
        "func (e *Engine) ownerOf(",
        "m.RuntimeExecCount++",
        "recordSpeculativeBaseline(e, id, m)",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"fabric-execution overlay upstream boundary missing: {missing}")

    # resolveExecutable/resolveMutable already traverse the root mounted Fabric.
    # A Memory returned from a passive shard therefore belongs to that shard's
    # dataMu, not the root Engine's dataMu. Lock the actual physical owner while
    # taking the executable Program snapshot and updating runtime-only telemetry.
    old = '''\te.dataMu.Lock()
\tm.RuntimeExecCount++
\tprogram := append([]Op(nil), m.Program...)
\te.dataMu.Unlock()
'''
    new = '''\texecutionOwner := e.ownerOf(m.ID)
\tif executionOwner == nil {
\t\treturn fmt.Errorf("physical execution owner unavailable: %s", m.ID)
\t}
\texecutionOwner.dataMu.Lock()
\tm.RuntimeExecCount++
\tprogram := append([]Op(nil), m.Program...)
\texecutionOwner.dataMu.Unlock()
'''
    text = _replace_once(text, old, new, "run physical-owner lock")

    if "executionOwner.dataMu.Lock()" not in text:
        raise RuntimeError("fabric-execution overlay failed to install owner lock")
    if old in text:
        raise RuntimeError("fabric-execution overlay left root execution lock")
    return text.encode("utf-8")
