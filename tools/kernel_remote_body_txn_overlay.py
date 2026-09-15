#!/usr/bin/env python3
from __future__ import annotations

import re


def apply_kernel_remote_body_txn_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "func loadPassiveStorage(",
        "func (e *Engine) close()",
        "forgetFabricOwner(e)",
        "e.closeStore()",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"remote-body-txn overlay upstream boundary missing: {missing}")

    # Keep the historical loader definition intact, but route every generated
    # call site through the serialized wrapper. The wrapper acquires the path
    # lease before opening the body and Engine.close releases it after closing
    # the Store, so the lock covers load -> mutate -> persist -> close.
    call_pattern = re.compile(r"(?<!func )\bloadPassiveStorage\(")
    call_count = len(call_pattern.findall(text))
    if call_count < 1:
        raise RuntimeError("remote-body-txn overlay found no passive-storage call sites")
    text = call_pattern.sub("loadPassiveStorageSerialized(", text)

    old_close_tail = '''\tforgetFabricOwner(e)
\te.closeStore()
}'''
    new_close_tail = '''\tforgetFabricOwner(e)
\te.closeStore()
\treleasePassiveBodyTransaction(e)
}'''
    if text.count(old_close_tail) != 1:
        raise RuntimeError(
            f"remote-body-txn overlay close hook: expected one match, got {text.count(old_close_tail)}"
        )
    text = text.replace(old_close_tail, new_close_tail, 1)

    if len(call_pattern.findall(text)) != 0:
        raise RuntimeError("unserialized loadPassiveStorage call remained")
    if text.count("func loadPassiveStorage(") != 1:
        raise RuntimeError("historical passive loader definition changed unexpectedly")
    if text.count("releasePassiveBodyTransaction(e)") != 1:
        raise RuntimeError("passive body transaction release hook missing or duplicated")
    if text.count("loadPassiveStorageSerialized(") != call_count:
        raise RuntimeError("passive body serialized-call count mismatch")
    return text.encode("utf-8")
