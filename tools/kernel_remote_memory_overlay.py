#!/usr/bin/env python3
from __future__ import annotations

import re


def apply_kernel_remote_memory_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        'case "remote_space_import":',
        'case "remote_space_list":',
        'recordSpeculativeBaseline(e, id, m)',
        'boundedLegacyBodyIDs(e)',
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"remote-memory overlay upstream boundary missing: {missing}")

    pattern = r'\n\tcase "remote_space_import":.*?\n\tcase "remote_space_list":'
    replacement = '''
\tcase "remote_space_import":
\t\treturn -1, errors.New("remote_space_import disabled: remote Memory is direct-read only; disconnected means forgotten, reconnected means remembered")
\tcase "remote_space_list":'''
    out, count = re.subn(pattern, replacement, text, count=1, flags=re.S)
    if count != 1:
        raise RuntimeError(f"remote-memory overlay expected one import block, got {count}")

    forbidden = (
        'e.importMemory(rr.Memory, e.writeEngine(self.ID), mode)',
        'remote memory missing")\n\t\t}\n\t\tmode := x(op.Args["mode"])',
    )
    bad = [token for token in forbidden if token in out]
    if bad:
        raise RuntimeError(f"remote Memory retrieval path remained in Kernel: {bad}")
    return out.encode("utf-8")
