#!/usr/bin/env python3
"""Deterministically migrate cognitive metric opcodes to Memory-owned State.

This migration is intentionally source-schema only. It transforms the exact
v27 required-structures baseline and refuses any other input hash.

The final Kernel must not understand trials/successes/reward/cost/stability/
reuse as cognitive fields and must not expose metric_add.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

EXPECTED_INPUT_SHA256 = "22c6ca302532a2af6cc9a09bacbbc84d364b1e1ce30daf6d4a23eac846e69e92"
COGNITIVE_METRICS = {
    "trials",
    "successes",
    "reward",
    "cost",
    "stability",
    "reuse",
}
TARGET_IDS = {
    "dimension.induce.parent",
    "research.strategy.select.parent",
    "evolution.feedback.parent",
}


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def metric_state_key(name: str) -> str:
    if name not in COGNITIVE_METRICS:
        raise ValueError(f"unsupported cognitive metric: {name}")
    return f"cog.metric.{name}"


def transform_program(program: list[dict]) -> tuple[list[dict], int]:
    out: list[dict] = []
    changes = 0
    for op in program:
        code = op.get("code")
        field = op.get("b")
        if code == "metric_add" and field in COGNITIVE_METRICS:
            q = dict(op)
            q["code"] = "state_num_add"
            q["b"] = metric_state_key(field)
            out.append(q)
            changes += 1
            continue
        if code == "field_get" and field in COGNITIVE_METRICS:
            q = dict(op)
            q["code"] = "state_get"
            q["b"] = metric_state_key(field)
            out.append(q)
            # Legacy top-level numeric fields implicitly read as zero when absent.
            # State does not have that implicit cognition-specific default, so make
            # the default explicit in Memory-owned bytecode.
            out.append({"code": "var_default", "a": q.get("c", ""), "b": "0"})
            changes += 1
            continue
        out.append(dict(op))
    return out, changes


def audit(memories: list[dict]) -> None:
    errors: list[str] = []
    for memory in memories:
        mid = memory.get("id", "<missing>")
        for i, op in enumerate(memory.get("program", [])):
            code = op.get("code")
            field = op.get("b")
            if code == "metric_add":
                errors.append(f"{mid}[{i}] still uses metric_add")
            if code == "field_get" and field in COGNITIVE_METRICS:
                errors.append(f"{mid}[{i}] still reads cognitive field {field}")
    if errors:
        raise RuntimeError("migration audit failed:\n" + "\n".join(errors))


def migrate(input_path: Path, output_path: Path) -> dict:
    raw = input_path.read_bytes()
    actual = sha256_bytes(raw)
    if actual != EXPECTED_INPUT_SHA256:
        raise RuntimeError(
            f"refusing non-v27 baseline: sha256={actual}, expected={EXPECTED_INPUT_SHA256}"
        )

    doc = json.loads(raw)
    memories = doc.get("memories")
    if not isinstance(memories, list):
        raise RuntimeError("required-structures document has no memories list")

    changed_ids: list[str] = []
    changed_ops = 0
    for memory in memories:
        program = memory.get("program")
        if not isinstance(program, list):
            continue
        transformed, changes = transform_program(program)
        if changes:
            memory["program"] = transformed
            memory["revision"] = int(memory.get("revision", 0)) + 1
            changed_ids.append(memory.get("id", ""))
            changed_ops += changes

    missing_expected = sorted(TARGET_IDS - set(changed_ids))
    unexpected = sorted(set(changed_ids) - TARGET_IDS)
    if missing_expected or unexpected:
        raise RuntimeError(
            f"unexpected migration coverage: missing={missing_expected}, unexpected={unexpected}"
        )

    audit(memories)
    doc["version"] = "28.0.0-cognitive-metrics-state"
    encoded = (json.dumps(doc, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")
    output_path.write_bytes(encoded)

    return {
        "input_sha256": actual,
        "output_sha256": sha256_bytes(encoded),
        "changed_ids": sorted(changed_ids),
        "changed_ops": changed_ops,
        "memory_count": len(memories),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()

    report = migrate(args.input, args.output)
    text = json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    if args.report:
        args.report.write_text(text, encoding="utf-8")
    print(text, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
