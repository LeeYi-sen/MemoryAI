#!/usr/bin/env python3
from __future__ import annotations

import hashlib

MESH_OVERLAY_SHA256 = "e0c46e34a55f09dbd05d3f6822f4a837da87b8edf9497699bbdc76c62e806ed2"


def apply_kernel_speculative_overlay(raw: bytes) -> bytes:
    got = hashlib.sha256(raw).hexdigest()
    if got != MESH_OVERLAY_SHA256:
        raise RuntimeError(
            f"speculative overlay requires mesh-overlay source {MESH_OVERLAY_SHA256}, got {got}"
        )
    text = raw.decode("utf-8")
    old = 'if e.speculative && speculativeForbiddenPrimitive(op.Code) {'
    new = 'if e.speculative && (speculativeForbiddenPrimitive(op.Code) || speculativeAdditionalForbiddenPrimitive(op.Code)) {'
    if text.count(old) != 1:
        raise RuntimeError(
            f"speculative overlay boundary hook: expected one match, got {text.count(old)}"
        )
    return text.replace(old, new, 1).encode("utf-8")
