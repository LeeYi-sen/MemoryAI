#!/usr/bin/env python3
from __future__ import annotations


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"store-access overlay {label}: expected one match, got {count}")
    return text.replace(old, new, 1)


def apply_kernel_store_access_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "finalizePersistedBody(e, out)",
        "e.placeRuntimeMemory(child)",
        "remote_space_import disabled:",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"store-access overlay upstream boundary missing: {missing}")

    # All generated Kernel access must flow through helpers that coordinate on
    # the physical IndexedStore instance. Do not add an Engine-local mutex:
    # speculative Engines share the same store descriptor and therefore require
    # one shared lifetime guard.
    text = text.replace("e.store.AllIDs()", "e.storeAllIDs()")
    text = text.replace("e.store.GetID(id)", "e.storeGetID(id)")
    text = text.replace("e.store.TagIDs(tag)", "e.storeTagIDs(tag)")

    text = _replace_once(
        text,
        '''\tfor _, sp := range e.spaces {
\t\tif sp != nil && sp.store != nil {
\t\t\tsp.store.Close()
\t\t}
\t}
\tif e.store != nil {
\t\te.store.Close()
\t}
''',
        '''\tfor _, sp := range e.spaces {
\t\tif sp != nil {
\t\t\tsp.closeStore()
\t\t}
\t}
\te.closeStore()
''',
        "Engine close store lifetime",
    )

    forbidden = (
        "e.store.AllIDs()",
        "e.store.GetID(id)",
        "e.store.TagIDs(tag)",
        "sp.store.Close()",
        "e.store.Close()",
        "storeMu            sync.RWMutex",
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"unlocked generated store access remained: {bad}")
    return text.encode("utf-8")
