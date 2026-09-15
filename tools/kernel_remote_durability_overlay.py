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

    create_old = '''\tcase "remote_space_create":
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "create", "name": x(op.Args["name"])}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
'''
    create_new = '''\tcase "remote_space_create":
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "create", "name": x(op.Args["name"])}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tif err = requireRemoteMutationACK("space_create", resp); err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
'''
    text = _replace_once(text, create_old, create_new, "remote create ACK")

    put_old = '''\tcase "remote_space_put":
\t\tm, err := e.resolve(x(op.A))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "put", "name": x(op.Args["name"]), "memory": m}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
'''
    put_new = '''\tcase "remote_space_put":
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
'''
    text = _replace_once(text, put_old, put_new, "remote put ACK")

    upsert_old = '''\tcase "remote_space_upsert":
\t\tm, err := e.resolve(x(op.A))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "put", "name": x(op.Args["name"]), "memory": m, "replace": true}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
'''
    upsert_new = '''\tcase "remote_space_upsert":
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
'''
    text = _replace_once(text, upsert_old, upsert_new, "remote upsert ACK")

    delete_old = '''\tcase "remote_space_delete":
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "delete", "name": x(op.Args["name"]), "id": x(op.Args["id"])}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
'''
    delete_new = '''\tcase "remote_space_delete":
\t\tresp, err := remoteSpaceRequest(x(op.Args["host"]), x(op.Args["port"]), map[string]any{"op": "delete", "name": x(op.Args["name"]), "id": x(op.Args["id"])}, parseTimeout(x(op.Args["timeout_ms"])))
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tif err = requireRemoteMutationACK("space_delete", resp); err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Vars[op.Args["out"]] = resp
'''
    text = _replace_once(text, delete_old, delete_new, "remote delete ACK")

    transfer_pattern = re.compile(
        r'func \(e \*Engine\) transferMemory\(id, target string, move bool\) \(string, error\) \{.*?\n\}\nfunc sameStrings',
        re.S,
    )
    transfer_replacement = '''func (e *Engine) transferMemory(id, target string, move bool) (string, error) {
\treturn transferMemoryDurable(e, id, target, move)
}
func sameStrings'''
    text, count = transfer_pattern.subn(transfer_replacement, text, count=1)
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
    text = text.replace(
        historical_save,
        'if er = persistEngineIfDirty(sp); er != nil {',
    )

    forbidden = (
        historical_save,
        'src.deletedIDs[id] = true',
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"remote-durability historical unsafe path remained: {bad}")

    required_after = (
        'requireRemoteMutationACK("space_create", resp)',
        'requireRemoteMutationACK("space_put", resp)',
        'requireRemoteMutationACK("space_upsert", resp)',
        'requireRemoteMutationACK("space_delete", resp)',
        'return transferMemoryDurable(e, id, target, move)',
        'persistEngineIfDirty(sp)',
    )
    missing = [token for token in required_after if token not in text]
    if missing:
        raise RuntimeError(
            f"remote-durability repaired boundary missing after overlay: {missing}"
        )
    return text.encode("utf-8")
