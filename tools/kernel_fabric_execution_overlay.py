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
        "func (e *Engine) resolveID(",
        "func (e *Engine) resolveMutable(",
        "func (e *Engine) mountSpace(",
        "func (e *Engine) unmountSpace(",
        "m.RuntimeExecCount++",
        "recordSpeculativeBaseline(e, id, m)",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"fabric-execution overlay upstream boundary missing: {missing}")

    # Canonical/local exact resolution must use one unique physical owner rule.
    # The historical resolver iterated the spaces map directly, so duplicate
    # physical IDs could be read nondeterministically. Speculative resolution is
    # intentionally kept private: resolveIDLocal/storeGetID already pages from the
    # origin Fabric into the transaction read-set without consulting live state.
    old_resolve_id = '''func (e *Engine) resolveID(id string) (*Memory, error) {
\tif m, err := e.resolveIDLocal(id); err == nil {
\t\treturn m, nil
\t}
\te.spaceMu.RLock()
\tspaces := make([]*Engine, 0, len(e.spaces))
\tfor _, sp := range e.spaces {
\t\tspaces = append(spaces, sp)
\t}
\te.spaceMu.RUnlock()
\tfor _, sp := range spaces {
\t\tif m, err := sp.resolveIDLocal(id); err == nil {
\t\t\treturn m, nil
\t\t}
\t}
\tif m, err := e.resolveRemoteID(id); err == nil {
\t\treturn m, nil
\t}
\t// Sovereign Mesh is a virtual remote address space. Shared Memory is read
\t// directly from a live origin and is never imported into local Memory.mem.
\tif mr := meshRuntimeCurrent(); mr != nil && mr.role != "standalone" {
\t\tif res, er := mr.sharedFetch(id); er == nil && res.Memory != nil {
\t\t\treturn res.Memory, nil
\t\t}
\t}
\treturn nil, io.EOF
}'''
    new_resolve_id = '''func (e *Engine) resolveID(id string) (*Memory, error) {
\tif e == nil {
\t\treturn nil, io.EOF
\t}
\tif e.speculative {
\t\tif m, err := e.resolveIDLocal(id); err == nil {
\t\t\treturn m, nil
\t\t}
\t} else {
\t\troot := fabricRootFor(e)
\t\tif root == nil {
\t\t\troot = e
\t\t}
\t\t_, m, err := root.resolveLocalFabricMemory(id)
\t\tif err == nil {
\t\t\treturn m, nil
\t\t}
\t\tif err != io.EOF {
\t\t\treturn nil, err
\t\t}
\t\te = root
\t}
\tif m, err := e.resolveRemoteID(id); err == nil {
\t\treturn m, nil
\t}
\t// Sovereign Mesh is a virtual remote address space. Shared Memory is read
\t// directly from a live origin and is never imported into local Memory.mem.
\tif mr := meshRuntimeCurrent(); mr != nil && mr.role != "standalone" {
\t\tif res, er := mr.sharedFetch(id); er == nil && res.Memory != nil {
\t\t\treturn res.Memory, nil
\t\t}
\t}
\treturn nil, io.EOF
}'''
    text = _replace_once(text, old_resolve_id, new_resolve_id, "unique local exact resolver")

    old_mutable = '''func (e *Engine) resolveMutable(idOrTag string) (*Memory, error) {
\tif m, err := e.resolveIDLocal(idOrTag); err == nil {
\t\treturn m, nil
\t}
\te.spaceMu.RLock()
\tspaces := make([]*Engine, 0, len(e.spaces))
\tfor _, sp := range e.spaces {
\t\tspaces = append(spaces, sp)
\t}
\te.spaceMu.RUnlock()
\tfor _, sp := range spaces {
\t\tif m, err := sp.resolveIDLocal(idOrTag); err == nil {
\t\t\treturn m, nil
\t\t}
\t}
\tids := []string{}
\tif xs, err := e.listTagLocal(idOrTag); err == nil {
\t\tids = append(ids, xs...)
\t}
\tfor _, sp := range spaces {
\t\tif xs, err := sp.listTagLocal(idOrTag); err == nil {
\t\t\tids = append(ids, xs...)
\t\t}
\t}
\tif len(ids) == 0 {
\t\treturn nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
\t}
\tsort.Strings(ids)
\tfor _, id := range ids {
\t\tif m, err := e.resolveIDLocal(id); err == nil {
\t\t\treturn m, nil
\t\t}
\t\tfor _, sp := range spaces {
\t\t\tif m, err := sp.resolveIDLocal(id); err == nil {
\t\t\t\treturn m, nil
\t\t\t}
\t\t}
\t}
\treturn nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
}'''
    new_mutable = '''func (e *Engine) resolveMutable(idOrTag string) (*Memory, error) {
\tif e == nil {
\t\treturn nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
\t}
\tif e.speculative {
\t\tif m, err := e.resolveIDLocal(idOrTag); err == nil {
\t\t\treturn m, nil
\t\t}
\t\tids, err := e.listTagLocal(idOrTag)
\t\tif err != nil && err != io.EOF {
\t\t\treturn nil, err
\t\t}
\t\tsort.Strings(ids)
\t\tfor _, id := range ids {
\t\t\tif m, er := e.resolveIDLocal(id); er == nil {
\t\t\t\treturn m, nil
\t\t\t}
\t\t}
\t\treturn nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
\t}

\troot := fabricRootFor(e)
\tif root == nil {
\t\troot = e
\t}
\t_, m, err := root.resolveLocalFabricMemory(idOrTag)
\tif err == nil {
\t\treturn m, nil
\t}
\tif err != io.EOF {
\t\treturn nil, err
\t}
\tids, _, err := root.listTagFabricOwned(idOrTag)
\tif err != nil {
\t\treturn nil, err
\t}
\tif len(ids) == 0 {
\t\treturn nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
\t}
\tfor _, id := range ids {
\t\t_, m, er := root.resolveLocalFabricMemory(id)
\t\tif er == nil {
\t\t\treturn m, nil
\t\t}
\t\tif er != io.EOF {
\t\t\treturn nil, er
\t\t}
\t}
\treturn nil, fmt.Errorf("mutable memory %q not found in mounted Memory Fabric", idOrTag)
}'''
    text = _replace_once(text, old_mutable, new_mutable, "unique mutable Fabric resolver")

    # Mounted bodies are independent physical concurrency domains. Sharing the
    # root dataMu across every shard makes the multi-owner two-phase commit lock
    # the same RWMutex repeatedly and also serializes unrelated shard writes.
    # Keep each loadEngine-created dataMu and register only the physical topology.
    old_mount = '''\tsp.dataMu = e.dataMu
\te.spaceMu.Lock()
\te.spaces[cp] = sp
\te.spaceMu.Unlock()
\treturn cp, nil
'''
    new_mount = '''\te.spaceMu.Lock()
\te.spaces[cp] = sp
\te.spaceMu.Unlock()
\trememberFabricOwner(e, sp)
\treturn cp, nil
'''
    text = _replace_once(text, old_mount, new_mount, "independent shard data lock")

    # Remove topology before closing a body so no new Fabric lookup can acquire
    # an Engine that is being persisted/closed. The Store lifetime guard handles
    # readers that already obtained the old physical Store.
    old_unmount = '''func (e *Engine) unmountSpace(path string) bool {
\tcp, err := e.localLocatorPath(path)
\tif err != nil {
\t\treturn false
\t}
\te.spaceMu.Lock()
\tdefer e.spaceMu.Unlock()
\tsp := e.spaces[cp]
\tif sp == nil {
\t\treturn false
\t}
\tif sp.dirty {
\t\t_ = sp.saveBody(sp.bodyPath)
\t}
\tsp.close()
\tdelete(e.spaces, cp)
\tif e.writeSpace == cp {
\t\te.writeSpace = ""
\t}
\treturn true
}'''
    new_unmount = '''func (e *Engine) unmountSpace(path string) bool {
\tcp, err := e.localLocatorPath(path)
\tif err != nil {
\t\treturn false
\t}
\te.spaceMu.Lock()
\tsp := e.spaces[cp]
\tif sp == nil {
\t\te.spaceMu.Unlock()
\t\treturn false
\t}
\tdelete(e.spaces, cp)
\tif e.writeSpace == cp {
\t\te.writeSpace = ""
\t}
\te.spaceMu.Unlock()
\tforgetFabricOwner(sp)
\tif sp.dirty {
\t\t_ = sp.saveBody(sp.bodyPath)
\t}
\tsp.close()
\treturn true
}'''
    text = _replace_once(text, old_unmount, new_unmount, "unmount topology before close")

    # store-access overlay has already converted close() to guarded closeStore().
    # Do not hold root.spaceMu while waiting for independent shard Store guards.
    old_close = '''func (e *Engine) close() {
\tif e == nil {
\t\treturn
\t}
\te.spaceMu.Lock()
\tdefer e.spaceMu.Unlock()
\tfor _, sp := range e.spaces {
\t\tif sp != nil {
\t\t\tsp.closeStore()
\t\t}
\t}
\te.closeStore()
}'''
    new_close = '''func (e *Engine) close() {
\tif e == nil {
\t\treturn
\t}
\te.spaceMu.Lock()
\tspaces := make([]*Engine, 0, len(e.spaces))
\tfor path, sp := range e.spaces {
\t\tif sp != nil {
\t\t\tspaces = append(spaces, sp)
\t\t}
\t\tdelete(e.spaces, path)
\t}
\te.writeSpace = ""
\te.spaceMu.Unlock()
\tfor _, sp := range spaces {
\t\tforgetFabricOwner(sp)
\t\tsp.closeStore()
\t}
\tforgetFabricOwner(e)
\te.closeStore()
}'''
    text = _replace_once(text, old_close, new_close, "independent Fabric close")

    # A Memory returned from a passive shard belongs to that shard's dataMu,
    # not the root Engine's dataMu. Lock the actual physical owner while taking
    # the executable Program snapshot and updating runtime-only telemetry.
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

    # ownerOf is a physical identity lookup. Route through the single Fabric
    # resolver; speculative Engines remain private owners because resolveIDLocal
    # lazily pages shard records into the speculative cache/read-set.
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
        "root.listTagFabricOwned(idOrTag)",
        "rememberFabricOwner(e, sp)",
        "forgetFabricOwner(sp)",
        "spaces := make([]*Engine, 0, len(e.spaces))",
        "if e.speculative {",
    )
    missing = [token for token in required_after if token not in text]
    if missing:
        raise RuntimeError(f"fabric-execution overlay output boundary missing: {missing}")
    forbidden = (
        old_run,
        old_owner,
        old_resolve_id,
        old_mutable,
        "sp.dataMu = e.dataMu",
    )
    if any(token in text for token in forbidden):
        raise RuntimeError("fabric-execution overlay left historical resolver/owner/shared-lock boundary")
    return text.encode("utf-8")
