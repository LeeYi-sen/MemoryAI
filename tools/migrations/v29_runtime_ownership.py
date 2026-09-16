#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

EXPECTED_INPUT_SHA256 = "eb82f503266a12c617e46e78eae882502daed54a8e90859760caf6be7a58ae73"

FRONTIERS = (
    "goal.frontier.experience.parent",
    "goal.frontier.concept.parent",
    "goal.frontier.belief.parent",
    "goal.frontier.revising.parent",
    "goal.frontier.dimension.parent",
    "goal.frontier.cognition.parent",
    "goal.frontier.causal.parent",
)

DRIVE_FACTORS = (
    ("uncertainty", "w_uncertainty", "1"),
    ("conflict", "w_conflict", "0"),
    ("curiosity", "w_curiosity", "0"),
    ("novelty", "w_novelty", "0"),
    ("goal_pressure", "w_goal", "1"),
)


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def lifecycle_dispatcher() -> dict:
    program: list[dict] = []
    for memory_id in FRONTIERS:
        program.append({"code": "call", "a": memory_id})
    return {
        "id": "memory.lifecycle.dispatch.parent",
        "layer": "emergent",
        "generation": 31,
        "revision": 1,
        "content": "Memory-owned lifecycle dispatcher activated by completed physical activity. Kernel emits the event but owns no learning sequence.",
        "tags": ["memory", "cog.lifecycle", "cog.policy.mutable", "cog.evolvable"],
        "trigger": ["event:memory.activity"],
        "capabilities": ["memory.read", "memory.write", "event.emit"],
        "state": {
            "policy_owner": "memory",
            "dispatcher_mode": "memory-ordered-frontiers",
        },
        "program": program,
    }


def drive_program() -> list[dict]:
    program: list[dict] = [{"code": "call", "a": "drive.weight.clamp.parent"}]
    for i, (state_key, weight_key, default_value) in enumerate(DRIVE_FACTORS):
        program.extend(
            [
                {"code": "state_get", "a": "{{__subject}}", "b": state_key, "c": f"gd_x{i}"},
                {"code": "var_default", "a": f"gd_x{i}", "b": default_value},
                {"code": "state_get", "a": "policy.drive", "b": weight_key, "c": f"gd_w{i}"},
                {"code": "var_default", "a": f"gd_w{i}", "b": "1"},
                {"code": "num_mul", "a": f"gd_t{i}", "b": f"{{{{gd_x{i}}}}}", "c": f"{{{{gd_w{i}}}}}"},
            ]
        )
    program.append({"code": "set", "a": "gd_score", "b": "0"})
    for i in range(len(DRIVE_FACTORS)):
        program.append({"code": "num_add", "a": "gd_score", "b": "{{gd_score}}", "c": f"{{{{gd_t{i}}}}}"})
    program.extend(
        [
            {"code": "state_set", "a": "{{__subject}}", "b": "drive_score", "c": "{{gd_score}}"},
            {"code": "state_set", "a": "{{__subject}}", "b": "drive_uncertainty", "c": "{{gd_t0}}"},
            {"code": "state_set", "a": "{{__subject}}", "b": "drive_conflict", "c": "{{gd_t1}}"},
            {"code": "state_set", "a": "{{__subject}}", "b": "drive_curiosity", "c": "{{gd_t2}}"},
            {"code": "state_set", "a": "{{__subject}}", "b": "drive_novelty", "c": "{{gd_t3}}"},
            {"code": "state_set", "a": "{{__subject}}", "b": "drive_goal", "c": "{{gd_t4}}"},
        ]
    )
    return program


def clamp_program() -> list[dict]:
    program: list[dict] = [
        {"code": "state_get", "a": "policy.drive", "b": "min_weight", "c": "dw_min"},
        {"code": "var_default", "a": "dw_min", "b": "0.05"},
    ]
    for i, (_, weight_key, _) in enumerate(DRIVE_FACTORS):
        next_label = f"dw_next_{i}"
        set_label = f"dw_set_{i}"
        program.extend(
            [
                {"code": "state_get", "a": "policy.drive", "b": weight_key, "c": f"dw_w{i}"},
                {"code": "var_default", "a": f"dw_w{i}", "b": "1"},
                {"code": "cmp_gt", "a": f"dw_low{i}", "b": "{{dw_min}}", "c": f"{{{{dw_w{i}}}}}"},
                {"code": "jump_if", "a": f"{{{{dw_low{i}}}}}", "b": set_label},
                {"code": "jump", "a": next_label},
                {"code": "label", "a": set_label},
                {"code": "state_set", "a": "policy.drive", "b": weight_key, "c": "{{dw_min}}"},
                {"code": "label", "a": next_label},
            ]
        )
    return program


def remove_prediction_error_drive_adaptation(program: list[dict]) -> list[dict]:
    out: list[dict] = []
    i = 0
    while i < len(program):
        op = program[i]
        if op.get("code") == "state_get" and op.get("a") == "{{pa_g}}" and op.get("b") == "prediction_error":
            # v28.4 emitted one fixed five-op update group for each Drive factor.
            group = program[i : i + 5]
            if len(group) != 5 or group[-1].get("code") != "state_num_add" or group[-1].get("b") != "w_prediction_error":
                raise RuntimeError("unexpected prediction_error Drive adaptation shape")
            i += 5
            continue
        out.append(op)
        i += 1
    return out


def migrate(doc: dict) -> dict:
    memories = doc.get("memories")
    if not isinstance(memories, list):
        raise RuntimeError("Memory seed has no memories list")
    by_id = {m.get("id"): m for m in memories if isinstance(m, dict)}
    missing = [mid for mid in (*FRONTIERS, "goal.drive.parent", "drive.weight.clamp.parent", "policy.drive", "policy.feedback.adapt.parent") if mid not in by_id]
    if missing:
        raise RuntimeError(f"required Memory-owned cognition structures missing: {missing}")

    if "memory.lifecycle.dispatch.parent" in by_id:
        raise RuntimeError("runtime ownership dispatcher already exists")
    memories.append(lifecycle_dispatcher())

    drive = by_id["goal.drive.parent"]
    drive["program"] = drive_program()
    drive["revision"] = int(drive.get("revision", 0)) + 1

    clamp = by_id["drive.weight.clamp.parent"]
    clamp["program"] = clamp_program()
    clamp["revision"] = int(clamp.get("revision", 0)) + 1

    policy = by_id["policy.drive"]
    state = policy.setdefault("state", {})
    if isinstance(state, dict):
        state.pop("w_prediction_error", None)
        state["drive_factors"] = "curiosity,conflict,uncertainty,goal,novelty"
        state["prediction_error_role"] = "evidence-not-drive-factor"
    policy["revision"] = int(policy.get("revision", 0)) + 1

    feedback = by_id["policy.feedback.adapt.parent"]
    feedback["program"] = remove_prediction_error_drive_adaptation(feedback.get("program", []))
    feedback["revision"] = int(feedback.get("revision", 0)) + 1

    doc["version"] = "29.0-memory-runtime-ownership"
    return doc


def audit(doc: dict) -> None:
    by_id = {m.get("id"): m for m in doc["memories"] if isinstance(m, dict)}
    dispatcher = by_id.get("memory.lifecycle.dispatch.parent")
    if not dispatcher or dispatcher.get("trigger") != ["event:memory.activity"]:
        raise RuntimeError("Memory-owned activity dispatcher missing exact physical trigger")
    calls = [op.get("a") for op in dispatcher.get("program", []) if op.get("code") == "call"]
    if tuple(calls) != FRONTIERS:
        raise RuntimeError(f"unexpected Memory lifecycle dispatch policy: {calls}")

    serialized_drive = json.dumps(by_id["goal.drive.parent"].get("program", []), sort_keys=True)
    serialized_clamp = json.dumps(by_id["drive.weight.clamp.parent"].get("program", []), sort_keys=True)
    serialized_feedback = json.dumps(by_id["policy.feedback.adapt.parent"].get("program", []), sort_keys=True)
    for payload_name, payload in (
        ("goal.drive.parent", serialized_drive),
        ("drive.weight.clamp.parent", serialized_clamp),
        ("policy.feedback.adapt.parent", serialized_feedback),
    ):
        if "w_prediction_error" in payload:
            raise RuntimeError(f"prediction_error remains a Drive weight in {payload_name}")
    if "prediction_error" in serialized_drive:
        raise RuntimeError("prediction_error remains a Drive contribution")
    policy_state = by_id["policy.drive"].get("state", {})
    if isinstance(policy_state, dict) and "w_prediction_error" in policy_state:
        raise RuntimeError("policy.drive still carries prediction_error weight")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()

    raw = args.input.read_bytes()
    actual = sha256_bytes(raw)
    if actual != EXPECTED_INPUT_SHA256:
        raise RuntimeError(f"refusing non-v28.7 input {actual}; expected {EXPECTED_INPUT_SHA256}")
    doc = migrate(json.loads(raw))
    audit(doc)
    encoded = (json.dumps(doc, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")
    args.output.write_bytes(encoded)
    report = {
        "input_sha256": actual,
        "output_sha256": sha256_bytes(encoded),
        "memory_count": len(doc["memories"]),
        "runtime_owner": "memory.lifecycle.dispatch.parent",
        "drive_factors": [x[0] for x in DRIVE_FACTORS],
    }
    text = json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    if args.report:
        args.report.write_text(text, encoding="utf-8")
    print(text, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
