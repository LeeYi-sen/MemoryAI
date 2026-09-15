#!/usr/bin/env python3
from __future__ import annotations


def apply_kernel_mesh_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")

    required = (
        "RuntimeExecCount uint64",
        "func loadEngine(",
        "const imageVersion = \"28.8.0-memory-fabric-sovereign\"",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"mesh overlay upstream boundary missing: {missing}")

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
    out = text.replace(old, new, 1)
    if out.count("bindMeshEngine(e)") != 1:
        raise RuntimeError("mesh overlay failed to install engine binding")
    return out.encode("utf-8")
