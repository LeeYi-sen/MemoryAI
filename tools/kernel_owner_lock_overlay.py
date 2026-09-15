#!/usr/bin/env python3
from __future__ import annotations


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"owner-lock overlay {label}: expected one match, got {count}")
    return text.replace(old, new, 1)


def apply_kernel_owner_lock_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "rememberFabricOwner(e, sp)",
        "forgetFabricOwner(sp)",
        "executionOwner.dataMu.Lock()",
        "persistEngineIfDirty(sp)",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"owner-lock overlay upstream boundary missing: {missing}")

    text = _replace_once(
        text,
        '''\tforgetFabricOwner(sp)
\tif sp.dirty {
\t\t_ = sp.saveBody(sp.bodyPath)
\t}
\tsp.close()
''',
        '''\tforgetFabricOwner(sp)
\tif err := persistEngineIfDirty(sp); err != nil {
\t\t// Historical unmount returns bool only; fail closed and keep the Engine
\t\t// open when durability cannot be established.
\t\trememberFabricOwner(e, sp)
\t\te.spaceMu.Lock()
\t\te.spaces[cp] = sp
\t\te.spaceMu.Unlock()
\t\treturn false
\t}
\tsp.close()
''',
        "unmount owner dirty check",
    )

    forbidden = (
        "if sp.dirty {",
        "sp.dataMu = e.dataMu",
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"cross-body shared-lock/dirty access remained: {bad}")
    return text.encode("utf-8")
