#!/usr/bin/env python3
from __future__ import annotations
import argparse, copy, hashlib, json
from pathlib import Path

EXPECTED_INPUT_SHA256 = "4e8c3e069cfa7ebc3802fba53e330bd96155df322462b648e168ccefd819c851"
EXPECTED_OUTPUT_SHA256 = "43811560f77e2b94fa6627e32972fc71af10a0452074c77277cc90a953cb237b"
DISPATCH_IDS = (
    "concept.support.parent",
    "belief.reassess.parent",
    "dimension.induce.parent",
    "causal.qualify.parent",
    "research.strategy.select.parent",
    "research.resolve.parent",
)

def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()

def dispatcher_program(mid: str, base_id: str) -> list[dict]:
    return [
        {"code": "state_get", "a": mid, "b": "active_implementation", "c": "__active_impl"},
        {"code": "var_default", "a": "__active_impl", "b": base_id},
        {"code": "call", "a": "{{__active_impl}}"},
    ]

def migrate(doc: dict) -> dict:
    memories = doc["memories"]
    by_id = {m["id"]: m for m in memories}
    added: list[dict] = []

    for mid in DISPATCH_IDS:
        if mid not in by_id:
            raise RuntimeError(f"missing dispatcher target {mid}")
        logical = by_id[mid]
        if (logical.get("state") or {}).get("active_implementation"):
            raise RuntimeError(f"{mid} already dispatched")
        base_id = mid + ".impl.base"
        base = copy.deepcopy(logical)
        base["id"] = base_id
        base["trigger"] = []
        tags = list(base.get("tags", []))
        if "runtime.trigger" in tags:
            tags.remove("runtime.trigger")
        for tag in ("cog.evolution.implementation", "cog.evolvable"):
            if tag not in tags:
                tags.append(tag)
        base["tags"] = tags
        state = dict(base.get("state") or {})
        state["logical_dispatcher"] = mid
        base["state"] = state
        base["revision"] = int(base.get("revision", 0)) + 1
        added.append(base)

        logical_state = dict(logical.get("state") or {})
        logical_state["active_implementation"] = base_id
        logical_state["challenger"] = ""
        logical["state"] = logical_state
        logical["program"] = dispatcher_program(mid, base_id)
        logical["revision"] = int(logical.get("revision", 0)) + 1
        logical_tags = list(logical.get("tags", []))
        if "cog.evolution.dispatcher" not in logical_tags:
            logical_tags.append("cog.evolution.dispatcher")
        logical["tags"] = logical_tags

    feedback = by_id["evolution.feedback.parent"]
    program = feedback["program"]

    promote_start = next(
        i for i, op in enumerate(program)
        if op.get("code") == "state_set" and op.get("b") == "promoted"
    )
    promote_end = next(
        i for i, op in enumerate(program[promote_start:], promote_start)
        if op.get("code") == "emit_event" and op.get("a") == "structure.promoted"
    )
    program[promote_start:promote_end + 1] = [
        {"code": "state_set", "a": "{{ef_sid}}", "b": "promoted", "c": "1"},
        {"code": "state_get", "a": "{{ef_sid}}", "b": "logical_dispatcher", "c": "ef_dispatch"},
        {"code": "cmp_eq", "a": "ef_nodisp", "b": "{{ef_dispatch}}"},
        {"code": "jump_if", "a": "{{ef_nodisp}}", "b": "ef_promote_tag"},
        {"code": "state_set", "a": "{{ef_dispatch}}", "b": "active_implementation", "c": "{{ef_sid}}"},
        {"code": "state_set", "a": "{{ef_dispatch}}", "b": "challenger", "c": ""},
        {"code": "label", "a": "ef_promote_tag"},
        {"code": "memory_tag_add", "a": "{{ef_sid}}", "b": "cog.evolution.promoted"},
        {"code": "emit_event", "a": "structure.promoted", "b": "{{ef_sid}}"},
    ]

    copy_index = next(i for i, op in enumerate(program) if op.get("code") == "memory_copy")
    program[copy_index:copy_index] = [
        {"code": "state_get", "a": "policy.evolution", "b": "max_variants", "c": "ef_maxv"},
        {"code": "tag_list", "a": "cog.evolution.variant.parent.{{ef_sid}}", "b": "ef_vs"},
        {"code": "list_len", "a": "ef_vs", "b": "ef_vn"},
        {"code": "cmp_ge", "a": "ef_vfull", "b": "{{ef_vn}}", "c": "{{ef_maxv}}"},
        {"code": "jump_if", "a": "{{ef_vfull}}", "b": "ef_end"},
    ]
    copy_index += 5
    program[copy_index]["args"] = dict(program[copy_index].get("args") or {})
    program[copy_index]["args"]["tags"] = "cog.evolution.variant,cog.evolvable,cog.evolution.variant.parent.{{ef_sid}}"

    mutation_parent_index = next(
        i for i, op in enumerate(program)
        if op.get("code") == "state_set" and op.get("b") == "mutation_parent"
    )
    program[mutation_parent_index + 1:mutation_parent_index + 1] = [
        {"code": "state_get", "a": "{{ef_sid}}", "b": "logical_dispatcher", "c": "ef_ld"},
        {"code": "state_set", "a": "{{ef_var}}", "b": "logical_dispatcher", "c": "{{ef_ld}}"},
    ]
    feedback["revision"] = int(feedback.get("revision", 0)) + 1

    memories.extend(added)
    doc["version"] = "28.1.0-evolution-dispatch"
    return doc

def audit(doc: dict) -> None:
    by_id = {m["id"]: m for m in doc["memories"]}
    for mid in DISPATCH_IDS:
        base_id = mid + ".impl.base"
        logical = by_id[mid]
        if logical.get("state", {}).get("active_implementation") != base_id:
            raise RuntimeError(f"{mid} active implementation pointer missing")
        if len(logical.get("program", [])) != 3 or logical["program"][-1].get("code") != "call":
            raise RuntimeError(f"{mid} is not a dispatcher")
        if by_id[base_id].get("trigger"):
            raise RuntimeError(f"{base_id} inherited trigger")
    if not any(op.get("b") == "max_variants" for op in by_id["evolution.feedback.parent"]["program"]):
        raise RuntimeError("policy.evolution.max_variants is still unused")

def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    raw = args.input.read_bytes()
    actual = sha256(raw)
    if actual != EXPECTED_INPUT_SHA256:
        raise RuntimeError(f"refusing unexpected input {actual}")
    doc = migrate(json.loads(raw))
    audit(doc)
    encoded = (json.dumps(doc, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")
    output_sha = sha256(encoded)
    if output_sha != EXPECTED_OUTPUT_SHA256:
        raise RuntimeError(f"unexpected output hash {output_sha}")
    args.output.write_bytes(encoded)
    report = {
        "input_sha256": actual,
        "output_sha256": output_sha,
        "dispatchers": list(DISPATCH_IDS),
        "memory_count": len(doc["memories"]),
    }
    text = json.dumps(report, indent=2, sort_keys=True) + "\n"
    if args.report:
        args.report.write_text(text, encoding="utf-8")
    print(text, end="")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
