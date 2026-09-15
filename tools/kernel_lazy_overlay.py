#!/usr/bin/env python3
from __future__ import annotations

import hashlib

SPECULATIVE_OVERLAY_SHA256 = "1a76efe0cfa484bf5f408e94babf364d8b7a6906b9dd2943fa9012c8321fef96"


def apply_kernel_lazy_overlay(raw: bytes) -> bytes:
    got = hashlib.sha256(raw).hexdigest()
    if got != SPECULATIVE_OVERLAY_SHA256:
        raise RuntimeError(
            f"lazy snapshot overlay requires speculative-overlay source {SPECULATIVE_OVERLAY_SHA256}, got {got}"
        )
    text = raw.decode("utf-8")
    old = '''\tm, err := e.store.GetID(id)
\tif err != nil {
\t\treturn nil, err
\t}
\te.dataMu.Lock()
'''
    new = '''\tm, err := e.store.GetID(id)
\tif err != nil {
\t\treturn nil, err
\t}
\trecordSpeculativeBaseline(e, id, m)
\te.dataMu.Lock()
'''
    if text.count(old) != 1:
        raise RuntimeError(
            f"lazy snapshot resolve hook: expected one match, got {text.count(old)}"
        )
    return text.replace(old, new, 1).encode("utf-8")
