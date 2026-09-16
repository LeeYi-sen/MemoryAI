#!/usr/bin/env python3
from __future__ import annotations

import re


def _top_level_function_block(text: str, signature: str) -> tuple[int, int, str]:
    start = text.find(signature)
    if start < 0:
        raise RuntimeError(f"mutation-journal overlay function boundary missing: {signature}")
    end = text.find("\nfunc ", start + len(signature))
    if end < 0:
        end = len(text)
    return start, end, text[start:end]


def apply_kernel_mutation_journal_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "loadEngineCanonical(",
        "writeDetZip(out, entries)",
        "func (e *Engine) saveBody(out string) error {",
        "func (e *Engine) persistAll() error {",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"mutation-journal overlay upstream boundary missing: {missing}")

    load_pattern = re.compile(r"\bloadEngineCanonical\(")
    load_count = len(load_pattern.findall(text))
    if load_count < 1:
        raise RuntimeError("mutation-journal overlay found no canonical Engine loads")
    text = load_pattern.sub("loadEngineWithMutationJournal(", text)

    persist_start, persist_end, persist_block = _top_level_function_block(
        text, "func (e *Engine) persistAll() error {"
    )
    for old_call, new_call, label in (
        ("persistEngineIfDirty(e)", "persistEngineIncremental(e)", "primary persistAll"),
        ("persistEngineIfDirty(sp)", "persistEngineIncremental(sp)", "mounted persistAll"),
    ):
        count = persist_block.count(old_call)
        if count != 1:
            raise RuntimeError(
                f"mutation-journal overlay {label}: expected one call inside persistAll, got {count}"
            )
        persist_block = persist_block.replace(old_call, new_call, 1)
    text = text[:persist_start] + persist_block + text[persist_end:]

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

    _, _, final_persist_block = _top_level_function_block(
        text, "func (e *Engine) persistAll() error {"
    )
    if "persistEngineIfDirty(e)" in final_persist_block or "persistEngineIfDirty(sp)" in final_persist_block:
        raise RuntimeError("generated persistAll still routes through full-body persistence")
    if final_persist_block.count("persistEngineIncremental(e)") != 1 or final_persist_block.count("persistEngineIncremental(sp)") != 1:
        raise RuntimeError("incremental persistAll routing missing or duplicated")

    # Entry 026: remote mutation and move verification now reads base+journal,
    # so the only remaining mounted full-body helper is the local unmount
    # durability barrier. Any occurrence outside unmountSpace fails closed.
    full_helper = "persistEngineIfDirty(sp)"
    _, _, unmount_block = _top_level_function_block(
        text, "func (e *Engine) unmountSpace("
    )
    if text.count(full_helper) != 1 or unmount_block.count(full_helper) != 1:
        raise RuntimeError("unexpected mounted full-body persistence remained after journal-aware verification")
    if text.count("persistEngineIncremental(sp)") < 3:
        raise RuntimeError("expected mounted persistAll plus remote incremental durability barriers")
    if text.count("writeDetZipWithMutationJournal(out, entries)") != 1:
        raise RuntimeError("journal-capable full-body writer missing or duplicated")
    return text.encode("utf-8")
