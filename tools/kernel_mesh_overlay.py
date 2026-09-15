#!/usr/bin/env python3
from __future__ import annotations

import hashlib

ABI_OVERLAY_SHA256 = "3bdfa38a3c7e9c03624e7e3e6a5c77abbc913372e82267b6e2744d3ca775145f"


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
