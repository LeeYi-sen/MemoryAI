#!/usr/bin/env python3
from __future__ import annotations


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"scale overlay {label}: expected one match, got {count}")
    return text.replace(old, new, 1)


def apply_kernel_scale_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        "RuntimeExecCount uint64",
        "recordSpeculativeBaseline(e, id, m)",
        "func (e *Engine) localIDs()",
        'case "body_count":',
        'case "body_list":',
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"scale overlay upstream boundary missing: {missing}")

    text = _replace_once(
        text,
        '''\tcase "body_count":
\t\tf.Vars[op.A] = strconv.Itoa(e.localMemoryCount())
\tcase "body_list":
\t\tids, err := e.localIDs()
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Lists[op.A] = ids
''',
        '''\tcase "body_count":
\t\tf.Vars[op.A] = strconv.Itoa(localMemoryCountFast(e))
\tcase "body_list":
\t\tids, err := boundedLegacyBodyIDs(e)
\t\tif err != nil {
\t\t\treturn -1, err
\t\t}
\t\tf.Lists[op.A] = ids
''',
        "VM body count/list boundary",
    )

    text = _replace_once(
        text,
        '''func (e *Engine) localMemoryCount() int {
\tids, err := e.localIDs()
\tif err != nil {
\t\treturn 0
\t}
\treturn len(ids)
}
''',
        '''func (e *Engine) localMemoryCount() int {
\treturn localMemoryCountFast(e)
}
''',
        "local memory count",
    )

    forbidden = (
        'f.Vars[op.A] = strconv.Itoa(e.localMemoryCount())',
        'case "body_list":\n\t\tids, err := e.localIDs()',
        'func (e *Engine) localMemoryCount() int {\n\tids, err := e.localIDs()',
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"unbounded whole-body path remained after scale overlay: {bad}")
    return text.encode("utf-8")
