#!/usr/bin/env python3
from __future__ import annotations

import re


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(
            f"remote-durability overlay {label}: expected one match, got {count}"
        )
    return text.replace(old, new, 1)


def apply_kernel_remote_durability_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        'case "remote_space_create":',
        'case "remote_space_put":',
        'case "remote_space_upsert":',
        'case "remote_space_delete":',
        'func (e *Engine) transferMemory(',
        'loadPassiveStorageSerialized(p)',
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(
            f"remote-durability overlay upstream boundary missing: {missing}"
        )

    replacements = (
        (
            '''\tcase "remote_space_create":
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "create", "name": x(op.Args["name"])}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
''',
            '''\tcase "remote_space_create":
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "create", "name": x(op.Args["name"])}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tif err = requireRemoteMutationACK("space_create", resp); err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
''',
            "remote create ACK",
        ),
        (
            '''\tcase "remote_space_put":
\t\tm, err := e.resolve(x(op.A))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "put", "name": x(op.Args["name"]), "memory": m}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
''',
            '''\tcase "remote_space_put":
\t\tm, err := e.resolve(x(op.A))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "put", "name": x(op.Args["name"]), "memory": m}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tif err = requireRemoteMutationACK("space_put", resp); err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
''',
            "remote put ACK",
        ),
        (
            '''\tcase "remote_space_upsert":
\t\tm, err := e.resolve(x(op.A))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "put", "name": x(op.Args["name"]), "memory": m, "replace": true}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
''',
            '''\tcase "remote_space_upsert":
\t\tm, err := e.resolve(x(op.A))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "put", "name": x(op.Args["name"]), "memory": m, "replace": true}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tif err = requireRemoteMutationACK("space_upsert", resp); err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
''',
            "remote upsert ACK",
        ),
        (
            '''\tcase "remote_space_delete":
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "delete", "name": x(op.Args["name"]), "id": x(op.Args["id"])}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
''',
            '''\tcase "remote_space_delete":
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "delete", "name": x(op.Args["name"]), "id": x(op.Args["id"])}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tif err = requireRemoteMutationACK("space_delete", resp); err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
''',
            "remote delete ACK",
        ),
    )
    for old, new, label in replacements:
        text = _replace_once(text, old, new, label)

    transfer_pattern = re.compile(
        r'func \(e \*Engine\) transferMemory\(id, target string, move bool\) \(string, error\) \{.*?\n\}\nfunc sameStrings',
        re.S,
    )
    text, count = transfer_pattern.subn(
        '''func (e *Engine) transferMemory(id, target string, move bool) (string, error) {
\treturn transferMemoryDurable(e, id, target, move)
}
func sameStrings''',
        text,
        count=1,
    )
    if count != 1:
        raise RuntimeError(
            f"remote-durability overlay transfer boundary: expected one match, got {count}"
        )

    historical_save = 'if er = sp.saveBody(p); er != nil {'
    save_count = text.count(historical_save)
    if save_count != 2:
        raise RuntimeError(
            f"remote-durability overlay remote write persistence: expected two historical saveBody(p) calls, got {save_count}"
        )
    # Physical verification is journal-aware, so remote mutation ACK paths no
    # longer need full-body rewrites. The in-body dual-slot journal is durable.
    text = text.replace(
        historical_save,
        'if er = persistEngineIncremental(sp); er != nil {',
    )

    forbidden = (
        historical_save,
        'src.deletedIDs[id] = true',
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"remote-durability historical unsafe path remained: {bad}")

    handler_start = text.find("func handleMemNodeConn(")
    handler_end = text.find("\nfunc ", handler_start + 1)
    if handler_start < 0 or handler_end < 0:
        raise RuntimeError("remote-durability could not isolate remote node handler")
    handler_block = text[handler_start:handler_end]
    if 'persistEngineIfDirty(sp)' in handler_block:
        raise RuntimeError("remote-durability unsafe full-body helper remained in remote node handler")

    required_after = (
        'requireRemoteMutationACK("space_create", resp)',
        'requireRemoteMutationACK("space_put", resp)',
        'requireRemoteMutationACK("space_upsert", resp)',
        'requireRemoteMutationACK("space_delete", resp)',
        'return transferMemoryDurable(e, id, target, move)',
        'persistEngineIncremental(sp)',
    )
    missing = [token for token in required_after if token not in text]
    if missing:
        raise RuntimeError(
            f"remote-durability repaired boundary missing after overlay: {missing}"
        )
    return text.encode("utf-8")
