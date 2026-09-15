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
        'if er = persistEngineIfDirty(sp); err != nilìœ°(€€€€¤((€€€É•ÅÕ¥É•‘}…™Ñ•È€ô€ (€€€€€€€€É•ÅÕ¥É•I•µ½Ñ•5ÕÑ…Ñ¥½¹, ‰ÍÁ…•}É•…Ñ”ˆ°É•ÍÀ¤œ°(€€€€€€€€É•ÅÕ¥É•I•µ½Ñ•5ÕÑ…Ñ¥½¹, ‰ÍÁ…•}ÁÕĞˆ°É•ÍÀ¤œ°(€€€€€€€€É•ÅÕ¥É•I•µ½Ñ•5ÕÑ…Ñ¥½¹, ‰ÍÁ…•}ÕÁÍ•ÉĞˆ°É•ÍÀ¤œ°(€€€€€€€€É•ÅÕ¥É•I•µ½Ñ•5ÕÑ…Ñ¥½¹, ‰ÍÁ…•}‘•±•Ñ”ˆ°É•ÍÀ¤œ°(€€€€€€€€É•ÑÕÉ¸ÑÉ…¹Í™•É5•µ½ÉåÕÉ…‰±”¡”°¥°Ñ…É•Ğ°µ½Ù”¤œ°(€€€€€€€€¥˜•È€ôÁ•ÉÍ¥ÍÑ¹¥¹•%™¥ÉÑä¡ÍÀ¤ì•ÉÈ€„ô¹¥°ìœ°(€€€€¤(€€€µ¥ÍÍ¥¹œ€ômÑ½­•¸™½ÈÑ½­•¸¥¸É•ÅÕ¥É•‘}…™Ñ•È¥˜Ñ½­•¸¹½Ğ¥¸Ñ•áÑt(€€€¥˜µ¥ÍÍ¥¹œè(€€€€€€€É…¥Í”IÕ¹Ñ¥µ•ÉÉ½È (€€€€€€€€€€€˜‰É•µ½Ñ”µ‘ÕÉ…‰¥±¥Ñä½Ù•É±…ä½ÕÑÁÕĞ‰½Õ¹‘…Éäµ¥ÍÍ¥¹œèíµ¥ÍÍ¥¹ôˆ(€€€€€€€€¤(€€€¥˜Ñ•áĞ¹½Õ¹Ğ ¥˜•È€ôÁ•ÉÍ¥ÍÑ¹¥¹•%™¥ÉÑä¡ÍÀ¤ì•ÉÈ€„ô¹¥°ìœ¤€ğ€Èè(€€€€€€€É…¥Í”IÕ¹Ñ¥µ•ÉÉ½È ‰É•µ½Ñ”‘ÕÉ…‰±”µ•´µ¹½‘”Á•ÉÍ¥ÍÑ•¹”‰…ÉÉ¥•Èµ¥ÍÍ¥¹œˆ¤(€€€¥˜¡¥ÍÑ½É¥…±}Í…Ù”¥¸Ñ•áĞè(€€€€€€€É…¥Í”IÕ¹Ñ¥µ•ÉÉ½È ‰É•µ½Ñ”µ•´µ¹½‘”ÍÑ¥±°Á•ÉÍ¥ÍÑÌÑ¡É½Õ É•ÅÕ•ÍĞ…±¥…ÌÁ…Ñ ˆ¤(€€€¥˜€ÍÉŒ¹‘•±•Ñ•‘%Ím¥‘t€ôÑÉÕ”œ¥¸Ñ•áĞè(€€€€€€€É…¥Í”IÕ¹Ñ¥µ•ÉÉ½È ‰¡¥ÍÑ½É¥…°ÁÉ”µ‘ÕÉ…‰¥±¥ÑäÑÉ…¹Í™•ÈÍ½ÕÉ”‘•±•Ñ¥½¸É•µ…¥¹•ˆ¤(€€€É•ÑÕÉ¸Ñ•áĞ¹•¹½‘” ‰ÕÑ˜´àˆ¤(