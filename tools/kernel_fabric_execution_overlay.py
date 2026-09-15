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
    old_run = '''\te.dataMu.Lock()
\tm.RuntimeExecCount++
\tprogram := append([]Op(nil), m.Program...)
\te.dataMu.Unlock()
'''
    new_run = '''\texecutionOwner := e.ownerOf(m.ID)
\tif executionOwner == nil {
\t\treturn fmt.Errorf("physical execution owner unavailable: %s", m.ID)
\t}
\texecutionOwner.dataMu.Lock()
\tm.RuntimeExecCount++
\tprogram := append([]Op(nil), m.Program...)
\texecutionOwner.dataMu.Unlock()
'''
    text = _replace_once(text, old_run, new_run, "run physical-owner lock")

    # ownerOf is a physical identity lookup. The historical implementation
    # iterated the spaces map directly, so duplicate IDs could select a random
    # owner. Route through the single Fabric resolver instead: it sorts physical
    # bodies and rejects duplicate local identities. Speculative Engines remain
    # private owners because resolveIDLocal lazily pages shard records into the
    # speculative cache/read-set.
    old_owner = '''func (e *Engine) ownerOf(id string) *Engine {
\tif _, err := e.resolveIDLocal(id); err == nil {
\t\treturn e
\t}
\te.spaceMu.RLock()
\tdefer e.spaceMu.RUnlock()
\tfor _, sp := range e.spaces {
\t\tif _, err := sp.resolveIDLocal(id); err == nil {
\t\t\treturn sp
\t\t}
\t}
\treturn nil
}'''
    new_owner = '''func (e *Engine) ownerOf(id string) *Engine {
\tif e == nil {
\t\treturn nil
\t}
\tif e.speculative {
\t\tif _, err := e.resolveIDLocal(id); err == nil {
\t\t\treturn e
\t\t}
\t\treturn nil
\t}
\troot := fabricRootFor(e)
\tif root == nil {
\t\troot = e
\t}
\towner, _, err := root.resolveLocalFabricMemory(id)
\tif err != nil {
\t\treturn nil
\t}
\treturn owner
}'''
    text = _replace_once(text, old_owner, new_owner, "unique Fabric owner lookup")

    required_after = (
        "executionOwner.dataMu.Lock()",
        "root.resolveLocalFabricMemory(id)",
        "if e.speculative {",
    )
    missing = [token for token in required_after if token not in text]
    if missing:
        raise RuntimeError(f"fabric-execution overlay output boundary missing: {missing}")
    if old_run in text or old_owner in text:
        raise RuntimeError("fabric-execution overlay left historical owner/execution lock")
    return text.encode("utf-8")
