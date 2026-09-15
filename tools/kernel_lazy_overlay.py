#!/usr/bin/env python3
from __future__ import annotations


def apply_kernel_lazy_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")

    required = (
        "RuntimeExecCount uint64",
        "bindMeshEngine(e)",
        "speculativeAdditionalForbiddenPrimitive(op.Code)",
        "func (e *Engine) resolveIDLocal(",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"lazy snapshot overlay upstream boundary missing: {missing}")

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
\tif err := recordSpeculativeBaseline(e, id, m); err != nil {
\t\treturn nil, err
\t}
\te.dataMu.Lock()
'''
    if text.count(old) != 1:
        raise RuntimeError(
            f"lazy snapshot resolve hook: expected one match, got {text.count(old)}"
        )
    out = text.replace(old, new, 1)
    if out.count("recordSpeculativeBaseline(e, id, m)") != 1:
        raise RuntimeError("lazy snapshot overlay failed to install first-read baseline hook")
    return out.encode("utf-8")
