#!/usr/bin/env python3
from __future__ import annotations


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"persistence overlay {label}: expected one match, got {count}")
    return text.replace(old, new, 1)


def apply_kernel_persistence_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "func (e *Engine) persistAll() error {",
        "func (e *Engine) saveBody(out string) error {",
        "remote_space_import disabled:",
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"persistence overlay upstream boundary missing: {missing}")

    text = _replace_once(
        text,
        '''func (e *Engine) persistAll() error {
\tif err := e.saveBody(e.bodyPath); err != nil {
\t\treturn err
\t}
''',
        '''func (e *Engine) persistAll() error {
\tif err := persistEngineIfDirty(e); err != nil {
\t\treturn err
\t}
''',
        "skip clean primary body",
    )

    text = _replace_once(
        text,
        '''\tentries = append(entries, zipEntry{"manifest.json", mb, false})
\treturn writeDetZip(out, entries)
}

type zipEntry struct {''',
        '''\tentries = append(entries, zipEntry{"manifest.json", mb, false})
\tif err := writeDetZip(out, entries); err != nil {
\t\treturn err
\t}
\treturn finalizePersistedBody(e, out)
}

type zipEntry struct {''',
        "reopen active persisted store",
    )

    forbidden = (
        'func (e *Engine) persistAll() error {\n\tif err := e.saveBody(e.bodyPath)',
        'entries = append(entries, zipEntry{"manifest.json", mb, false})\n\treturn writeDetZip(out, entries)',
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"unfixed persistence path remained: {bad}")
    return text.encode("utf-8")
