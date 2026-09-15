#!/usr/bin/env python3
from __future__ import annotations

import hashlib

ABI_OVERLAY_SHA256 = "8fb0b2714984ca59b39ab5fef1dd6ad0066995ac3cc3c4e68213a60f60656e18"


def apply_kernel_mesh_overlay(raw: bytes) -> bytes:
    got = hashlib.sha256(raw).hexdigest()
    if got != ABI_OVERLAY_SHA256:
        raise RuntimeError(
            f"mesh overlay requires ABI-overlay source {ABI_OVERLAY_SHA256}, got {got}"
        )
    text = raw.decode("utf-8")
    old = '''\te, err := loadEngine(body)
\tif err != nil {
\t\tdie(err)
\t}
'''
    new = '''\te, err := loadEngine(body)
\tif err != nil {
\t\tdie(err)
\t}
\tbindMeshEngine(e)
'''
    if text.count(old) != 1:
        raise RuntimeError(f"mesh overlay loadEngine hook: expected one match, got {text.count(old)}")
    return text.replace(old, new, 1).encode("utf-8")
