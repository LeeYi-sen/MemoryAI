#!/usr/bin/env python3
from __future__ import annotations

import re


def apply_kernel_parallel_boundary_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        'case "parallel_score6":',
        'globalParallelRuntime.Score6',
        'func (e *Engine) execPrimitive(',
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"parallel-boundary overlay upstream boundary missing: {missing}")

    # A fixed six-lane "score" primitive hard-codes a cognitive vector shape in
    # the Kernel ABI. Remove the entire VM case. Memory remains free to implement
    # arbitrary scoring structures using generic scalar/vector arithmetic outside
    # the immutable cognitive boundary.
    pattern = re.compile(
        r'\n\tcase "parallel_score6":.*?(?=\n\tcase "|\n\tdefault:|\n\t})',
        re.S,
    )
    out, count = pattern.subn("", text, count=1)
    if count != 1:
        raise RuntimeError(f"parallel-boundary overlay expected one score6 case, got {count}")
    if 'case "parallel_score6":' in out or 'globalParallelRuntime.Score6' in out:
        raise RuntimeError("fixed six-lane scoring primitive remained in generated Kernel")
    return out.encode("utf-8")
