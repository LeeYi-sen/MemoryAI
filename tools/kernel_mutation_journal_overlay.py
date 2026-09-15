#!/usr/bin/env python3
from __future__ import annotations

import re


def apply_kernel_mutation_journal_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "loadEngineCanonical(",
        "writeDetZip(out, entries)",
        "func (e *Engine) saveBody(out string) error {",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"mutation-journal overlay upstream boundary missing: {missing}")

    load_pattern = re.compile(r"\bloadEngineCanonical\(")
    load_count = len(load_pattern.findall(text))
    if load_count < 1:
        raise RuntimeError("mutation-journal overlay found no canonical Engine loads")
    text = load_pattern.sub("loadEngineWithMutationJournal(", text)

    for old_call, new_call, label in (
        ("persistEngineIfDirty(e)", "persistEngineIncremental(e)", "primary persistAll"),
        ("persistEngineIfDirty(sp)", "persistEngineIncremental(sp)", "mounted persistAll"),
    ):
        if text.count(old_call) != 1:
            raise RuntimeError(
                f"mutation-journal overlay {label}: expected one call, got {text.count(old_call)}"
            )
        text = text.replace(old_call, new_call, 1)

    old_write = "if err := writeDetZip(out, entries); err != nil {"
    new_write = "if err := writeDetZipWithMutationJournal(out, entries); err != nil {"
    if text.count(old_write) != 1:
        raise RuntimeError(
            f"mutation-journal overlay expected one active saveBody writer, got {text.count(old_write)}"
        )
    text = text.replace(old_write, new_write, 1)

    if "loadEngineCanonical(" in text:
        raise RuntimeError("non-journal-aware canonical Engine load remained in generated Kernel")
    if text.count("loadEngineWithMutationJournal(") != load_count:
        raise RuntimeError("journal-aware Engine load count mismatch")
    if "persistEngineIfDirty(e)" in text or "persistEngineIfDirty(sp)" in text:
        raise RuntimeError("generated persistAll still routes through full-body persistence")
    if text.count("persistEngineIncremental(e)") != 1 or text.count("persistEngineIncremental(sp)") != 1:
        raise RuntimeError("incremental persistAll routing missing or duplicated")
    if text.count("writeDetZipWithMutationJournal(out, entries)") != 1:
        raise RuntimeError("journal-capable full-body writer missing or duplicated")
    return text.encode("utf-8")
