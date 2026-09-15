#!/usr/bin/env python3
from __future__ import annotations

import re


def apply_kernel_mesh_journal_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")

    required = (
        "bindMeshEngine(e)",
        "func (e *Engine) execPrimitive(",
        "mesh_shared_propose",
        "mesh_journal_flush",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"mesh-journal overlay upstream boundary missing: {missing}")

    if text.count("bindMeshEngine(e)") != 1:
        raise RuntimeError(
            f"mesh-journal overlay bind hook: expected one match, got {text.count('bindMeshEngine(e)')}"
        )
    text = text.replace(
        "bindMeshEngine(e)",
        "bindMeshEngine(e)\n\trecoverMeshJournalAfterBind(e)",
        1,
    )

    propose_pattern = re.compile(r"\b([A-Za-z_][A-Za-z0-9_]*)\.proposeShared\(")
    propose_matches = propose_pattern.findall(text)
    if len(propose_matches) != 1:
        raise RuntimeError(
            f"mesh-journal overlay propose boundary: expected one VM call, got {len(propose_matches)}"
        )
    text = propose_pattern.sub(r"\1.proposeSharedDurable(", text, count=1)

    flush_pattern = re.compile(r"\b([A-Za-z_][A-Za-z0-9_]*)\.flushJournal\(\)")
    flush_matches = flush_pattern.findall(text)
    if len(flush_matches) != 1:
        raise RuntimeError(
            f"mesh-journal overlay flush boundary: expected one VM call, got {len(flush_matches)}"
        )
    text = flush_pattern.sub(r"flushMeshDeferredJournal(\1)", text, count=1)

    forbidden = (
        ".proposeShared(",
        ".flushJournal()",
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"legacy unbounded mesh journal VM boundary remained: {bad}")

    required_after = (
        "recoverMeshJournalAfterBind(e)",
        ".proposeSharedDurable(",
        "flushMeshDeferredJournal(",
    )
    missing = [token for token in required_after if token not in text]
    if missing:
        raise RuntimeError(f"mesh-journal overlay output boundary missing: {missing}")
    return text.encode("utf-8")
