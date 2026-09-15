#!/usr/bin/env python3
from __future__ import annotations


def apply_kernel_speculative_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")

    # Upstream semantic preconditions replace the former whole-file hash chain.
    # The immutable v27 source is still SHA-verified before any overlay runs;
    # here we verify only the exact boundaries this layer depends on.
    required = (
        "RuntimeExecCount uint64",
        "bindMeshEngine(e)",
        "func (e *Engine) execOp(",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(
            f"speculative overlay upstream boundary missing: {missing}"
        )

    old = 'if e.speculative && speculativeForbiddenPrimitive(op.Code) {'
    new = 'if e.speculative && (speculativeForbiddenPrimitive(op.Code) || speculativeAdditionalForbiddenPrimitive(op.Code)) {'
    if text.count(old) != 1:
        raise RuntimeError(
            f"speculative overlay boundary hook: expected one match, got {text.count(old)}"
        )
    out = text.replace(old, new, 1)
    if out.count("speculativeAdditionalForbiddenPrimitive(op.Code)") != 1:
        raise RuntimeError("speculative overlay failed to install additional side-effect boundary")
    return out.encode("utf-8")
