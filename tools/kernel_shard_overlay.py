#!/usr/bin/env python3
from __future__ import annotations


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"shard overlay {label}: expected one match, got {count}")
    return text.replace(old, new, 1)


def apply_kernel_shard_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "finalizePersistedBody(e, out, persistedSnapshot)",
        "remote_space_import disabled:",
        "func (e *Engine) mountRegisteredStorageForIntegrity()",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"shard overlay upstream boundary missing: {missing}")

    text = _replace_once(
        text,
        '''\tif e.manifest.Role == "core" {
\t\te.mountRegisteredStorageForIntegrity()
\t}
\tdefer e.close()
''',
        '''\tif e.manifest.Role == "core" {
\t\t// Automatic Memory.N.mem files are physical shards and are recovered
\t\t// independently from Memory-owned registry semantics.
\t\te.mountAutomaticStorageShards()
\t\te.mountRegisteredStorageForIntegrity()
\t}
\tdefer e.close()
''',
        "startup automatic shard recovery",
    )

    old_place = '''\t\t// New Memory is born into the currently selected local Memory Fabric segment.
\t\te.writeEngine(child.ID).addRuntimeMemory(child)
'''
    new_place = '''\t\t// Memory decides that the structure is born; Kernel only selects a bounded
\t\t// physical shard for its bytes.
\t\tif err := e.placeRuntimeMemory(child); err != nil {
\t\t\treturn -1, err
\t\t}
'''
    count = text.count(old_place)
    if count != 2:
        raise RuntimeError(f"shard overlay runtime placement: expected two matches, got {count}")
    text = text.replace(old_place, new_place)

    if text.count("e.placeRuntimeMemory(child)") != 2:
        raise RuntimeError("shard overlay failed to route memory_new/memory_copy through bounded placement")
    if "e.writeEngine(child.ID).addRuntimeMemory(child)" in text:
        raise RuntimeError("unbounded new-Memory write path remained after shard overlay")
    return text.encode("utf-8")
