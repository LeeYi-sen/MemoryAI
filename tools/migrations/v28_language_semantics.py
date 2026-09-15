#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

EXPECTED_INPUT_SHA256 = "4de691df29abafd625e193e259caea1583b0cdcf60bab5c3d328beca779212d9"


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def memory(
    mid: str,
    content: str,
    tags: list[str],
    program: list[dict],
    *,
    trigger: list[str] | None = None,
    parents: list[str] | None = None,
    state: dict | None = None,
) -> dict:
    out = {
        "id": mid,
        "content": content,
        "tags": ["memory", "cog.evolvable", *tags],
        "parents": parents or [],
        "layer": "emergent",
        "generation": 31,
        "revision": 1,
        "executable": True,
        "capabilities": ["memory.write"],
        "program": program,
        "state": state or {},
    }
    if trigger:
        out["trigger"] = trigger
        if "runtime.trigger" not in out["tags"]:
            out["tags"].append("runtime.trigger")
    return out


def grounding_program() -> list[dict]:
    return [
        {"code": "state_get", "a": "{{__subject}}", "b": "surface", "c": "sg_surface"},
        {"code": "state_get", "a": "{{__subject}}", "b": "semantic_kind", "c": "sg_kind"},
        {"code": "state_get", "a": "{{__subject}}", "b": "source_id", "c": "sg_source"},
        {"code": "var_default", "a": "sg_source", "b": "source.unknown"},
        {"code": "set", "a": "sg_keyraw", "b": "{{sg_kind}}|{{sg_surface}}"},
        {"code": "sha256_text", "a": "sg_keyraw", "b": "sg_key"},
        {"code": "tag_list", "a": "cog.semantic.ground.key.{{sg_key}}", "b": "sg_old"},
        {"code": "list_len", "a": "sg_old", "b": "sg_n"},
        {"code": "cmp_gt", "a": "sg_exists", "b": "{{sg_n}}", "c": "0"},
        {"code": "jump_if", "a": "{{sg_exists}}", "b": "sg_existing"},
        {
            "code": "memory_new",
            "a": "sg_map",
            "args": {
                "content": "Observed surface-to-semantic grounding owned by Memory.",
                "layer": "emergent",
                "parents": "{{__subject}},semantic.ground.observe.parent",
                "tags": "memory,cog.semantic.ground,cog.semantic.ground.candidate,cog.semantic.ground.key.{{sg_key}}",
            },
        },
        {"code": "state_set", "a": "{{sg_map}}", "b": "surface", "c": "{{sg_surface}}"},
        {"code": "state_set", "a": "{{sg_map}}", "b": "semantic_kind", "c": "{{sg_kind}}"},
        {"code": "state_set", "a": "{{sg_map}}", "b": "observation_support", "c": "1"},
        {"code": "state_set", "a": "{{sg_map}}", "b": "independent_sources", "c": "0"},
        {"code": "jump", "a": "sg_source_check"},
        {"code": "label", "a": "sg_existing"},
        {"code": "list_get", "a": "sg_old", "b": "0", "c": "sg_map"},
        {"code": "state_num_add", "a": "{{sg_map}}", "b": "observation_support", "c": "1"},
        {"code": "label", "a": "sg_source_check"},
        {"code": "set", "a": "sg_srcraw", "b": "{{sg_key}}|{{sg_source}}"},
        {"code": "sha256_text", "a": "sg_srcraw", "b": "sg_srch"},
        {"code": "tag_list", "a": "cog.semantic.ground.source.{{sg_srch}}", "b": "sg_src_old"},
        {"code": "list_len", "a": "sg_src_old", "b": "sg_src_n"},
        {"code": "cmp_gt", "a": "sg_src_seen", "b": "{{sg_src_n}}", "c": "0"},
        {"code": "jump_if", "a": "{{sg_src_seen}}", "b": "sg_promote_check"},
        {
            "code": "memory_new",
            "a": "sg_ev",
            "args": {
                "content": "Independent grounding-source evidence.",
                "layer": "emergent",
                "parents": "{{__subject}},{{sg_map}}",
                "tags": "memory,cog.semantic.ground.evidence,cog.semantic.ground.source.{{sg_srch}}",
            },
        },
        {"code": "state_set", "a": "{{sg_ev}}", "b": "source_id", "c": "{{sg_source}}"},
        {"code": "state_num_add", "a": "{{sg_map}}", "b": "independent_sources", "c": "1"},
        {"code": "label", "a": "sg_promote_check"},
        {"code": "state_get", "a": "{{sg_map}}", "b": "observation_support", "c": "sg_obs"},
        {"code": "state_get", "a": "{{sg_map}}", "b": "independent_sources", "c": "sg_sources"},
        {"code": "state_get", "a": "policy.semantic.grounding", "b": "stable_observations", "c": "sg_obs_min"},
        {"code": "state_get", "a": "policy.semantic.grounding", "b": "stable_sources", "c": "sg_src_min"},
        {"code": "cmp_ge", "a": "sg_obs_ok", "b": "{{sg_obs}}", "c": "{{sg_obs_min}}"},
        {"code": "cmp_ge", "a": "sg_src_ok", "b": "{{sg_sources}}", "c": "{{sg_src_min}}"},
        {"code": "jump_if", "a": "{{sg_obs_ok}}", "b": "sg_check_source"},
        {"code": "jump", "a": "sg_end"},
        {"code": "label", "a": "sg_check_source"},
        {"code": "jump_if", "a": "{{sg_src_ok}}", "b": "sg_promote"},
        {"code": "jump", "a": "sg_end"},
        {"code": "label", "a": "sg_promote"},
        {"code": "memory_tag_remove", "a": "{{sg_map}}", "b": "cog.semantic.ground.candidate"},
        {"code": "memory_tag_add", "a": "{{sg_map}}", "b": "cog.semantic.ground.stable"},
        {"code": "state_set", "a": "{{sg_map}}", "b": "status", "c": "stable"},
        {"code": "emit_event", "a": "semantic.ground.stable", "b": "{{sg_map}}"},
        {"code": "label", "a": "sg_end"},
    ]


def relation_program() -> list[dict]:
    return [
        {"code": "state_get", "a": "{{__subject}}", "b": "subject_id", "c": "sr_s"},
        {"code": "state_get", "a": "{{__subject}}", "b": "predicate_id", "c": "sr_p"},
        {"code": "state_get", "a": "{{__subject}}", "b": "object_id", "c": "sr_o"},
        {"code": "state_get", "a": "{{__subject}}", "b": "source_id", "c": "sr_source"},
        {"code": "var_default", "a": "sr_source", "b": "source.unknown"},
        {"code": "set", "a": "sr_raw", "b": "{{sr_s}}|{{sr_p}}|{{sr_o}}"},
        {"code": "sha256_text", "a": "sr_raw", "b": "sr_h"},
        {"code": "tag_list", "a": "cog.semantic.relation.key.{{sr_h}}", "b": "sr_old"},
        {"code": "list_len", "a": "sr_old", "b": "sr_n"},
        {"code": "cmp_gt", "a": "sr_exists", "b": "{{sr_n}}", "c": "0"},
        {"code": "jump_if", "a": "{{sr_exists}}", "b": "sr_existing"},
        {
            "code": "memory_new",
            "a": "sr_rel",
            "args": {
                "content": "Learned semantic relation induced from grounded experience.",
                "layer": "emergent",
                "parents": "{{__subject}},semantic.relation.learn.parent",
                "tags": "memory,cog.semantic.relation,cog.semantic.relation.candidate,cog.semantic.relation.key.{{sr_h}}",
            },
        },
        {"code": "state_set", "a": "{{sr_rel}}", "b": "subject_id", "c": "{{sr_s}}"},
        {"code": "state_set", "a": "{{sr_rel}}", "b": "predicate_id", "c": "{{sr_p}}"},
        {"code": "state_set", "a": "{{sr_rel}}", "b": "object_id", "c": "{{sr_o}}"},
        {"code": "state_set", "a": "{{sr_rel}}", "b": "observation_support", "c": "1"},
        {"code": "state_set", "a": "{{sr_rel}}", "b": "independent_sources", "c": "0"},
        {"code": "jump", "a": "sr_source_check"},
        {"code": "label", "a": "sr_existing"},
        {"code": "list_get", "a": "sr_old", "b": "0", "c": "sr_rel"},
        {"code": "state_num_add", "a": "{{sr_rel}}", "b": "observation_support", "c": "1"},
        {"code": "label", "a": "sr_source_check"},
        {"code": "set", "a": "sr_srcraw", "b": "{{sr_h}}|{{sr_source}}"},
        {"code": "sha256_text", "a": "sr_srcraw", "b": "sr_srch"},
        {"code": "tag_list", "a": "cog.semantic.relation.source.{{sr_srch}}", "b": "sr_src_old"},
        {"code": "list_len", "a": "sr_src_old", "b": "sr_src_n"},
        {"code": "cmp_gt", "a": "sr_src_seen", "b": "{{sr_src_n}}", "c": "0"},
        {"code": "jump_if", "a": "{{sr_src_seen}}", "b": "sr_promote_check"},
        {
            "code": "memory_new",
            "a": "sr_ev",
            "args": {
                "content": "Independent semantic-relation source evidence.",
                "layer": "emergent",
                "parents": "{{__subject}},{{sr_rel}}",
                "tags": "memory,cog.semantic.relation.evidence,cog.semantic.relation.source.{{sr_srch}}",
            },
        },
        {"code": "state_set", "a": "{{sr_ev}}", "b": "source_id", "c": "{{sr_source}}"},
        {"code": "state_num_add", "a": "{{sr_rel}}", "b": "independent_sources", "c": "1"},
        {"code": "label", "a": "sr_promote_check"},
        {"code": "state_get", "a": "{{sr_rel}}", "b": "observation_support", "c": "sr_obs"},
        {"code": "state_get", "a": "{{sr_rel}}", "b": "independent_sources", "c": "sr_sources"},
        {"code": "state_get", "a": "policy.semantic.grounding", "b": "relation_stable_observations", "c": "sr_obs_min"},
        {"code": "state_get", "a": "policy.semantic.grounding", "b": "stable_sources", "c": "sr_src_min"},
        {"code": "cmp_ge", "a": "sr_obs_ok", "b": "{{sr_obs}}", "c": "{{sr_obs_min}}"},
        {"code": "cmp_ge", "a": "sr_src_ok", "b": "{{sr_sources}}", "c": "{{sr_src_min}}"},
        {"code": "jump_if", "a": "{{sr_obs_ok}}", "b": "sr_check_source"},
        {"code": "jump", "a": "sr_end"},
        {"code": "label", "a": "sr_check_source"},
        {"code": "jump_if", "a": "{{sr_src_ok}}", "b": "sr_promote"},
        {"code": "jump", "a": "sr_end"},
        {"code": "label", "a": "sr_promote"},
        {"code": "memory_tag_remove", "a": "{{sr_rel}}", "b": "cog.semantic.relation.candidate"},
        {"code": "memory_tag_add", "a": "{{sr_rel}}", "b": "cog.semantic.relation.stable"},
        {"code": "state_set", "a": "{{sr_rel}}", "b": "status", "c": "stable"},
        {"code": "emit_event", "a": "semantic.relation.stable", "b": "{{sr_rel}}"},
        {"code": "label", "a": "sr_end"},
    ]


def role_resolve_program() -> list[dict]:
    return [
        {"code": "state_get", "a": "{{__subject}}", "b": "role_surface", "c": "rr_surface"},
        {"code": "set", "a": "rr_raw", "b": "role|{{rr_surface}}"},
        {"code": "sha256_text", "a": "rr_raw", "b": "rr_h"},
        {"code": "tag_list", "a": "cog.semantic.ground.key.{{rr_h}}", "b": "rr_maps"},
        {"code": "list_len", "a": "rr_maps", "b": "rr_n"},
        {"code": "cmp_gt", "a": "rr_have", "b": "{{rr_n}}", "c": "0"},
        {"code": "jump_if", "a": "{{rr_have}}", "b": "rr_use"},
        {"code": "jump", "a": "rr_end"},
        {"code": "label", "a": "rr_use"},
        {"code": "list_get", "a": "rr_maps", "b": "0", "c": "rr_map"},
        {"code": "state_get", "a": "{{rr_map}}", "b": "target_id", "c": "rr_target"},
        {"code": "var_default", "a": "rr_target", "b": "{{rr_map}}"},
        {"code": "state_set", "a": "{{__subject}}", "b": "resolved_role", "c": "{{rr_target}}"},
        {"code": "emit_event", "a": "semantic.role.resolved", "b": "{{__subject}}"},
        {"code": "label", "a": "rr_end"},
    ]


def expression_frame_program() -> list[dict]:
    return [
        {"code": "state_get", "a": "{{__subject}}", "b": "relation_id", "c": "ex_rel"},
        {"code": "state_get", "a": "{{ex_rel}}", "b": "subject_id", "c": "ex_s"},
        {"code": "state_get", "a": "{{ex_rel}}", "b": "predicate_id", "c": "ex_p"},
        {"code": "state_get", "a": "{{ex_rel}}", "b": "object_id", "c": "ex_o"},
        {
            "code": "memory_new",
            "a": "ex_frame",
            "args": {
                "content": "Semantic expression frame; presentation adapters may render it in learned natural language.",
                "layer": "emergent",
                "parents": "{{__subject}},{{ex_rel}},semantic.expression.frame.parent",
                "tags": "memory,cog.expression.frame",
            },
        },
        {"code": "state_set", "a": "{{ex_frame}}", "b": "subject_id", "c": "{{ex_s}}"},
        {"code": "state_set", "a": "{{ex_frame}}", "b": "predicate_id", "c": "{{ex_p}}"},
        {"code": "state_set", "a": "{{ex_frame}}", "b": "object_id", "c": "{{ex_o}}"},
        {"code": "state_set", "a": "{{ex_frame}}", "b": "render_policy", "c": "learned-surface-map"},
        {"code": "emit_event", "a": "expression.frame.ready", "b": "{{ex_frame}}"},
    ]


def direct_ground_policy_program() -> list[dict]:
    return [
        {"code": "state_get", "a": "{{__subject}}", "b": "observation_support", "c": "dg_obs"},
        {"code": "state_get", "a": "{{__subject}}", "b": "independent_sources", "c": "dg_sources"},
        {"code": "state_get", "a": "policy.semantic.grounding", "b": "stable_observations", "c": "dg_obs_min"},
        {"code": "state_get", "a": "policy.semantic.grounding", "b": "stable_sources", "c": "dg_src_min"},
        {"code": "cmp_ge", "a": "dg_obs_ok", "b": "{{dg_obs}}", "c": "{{dg_obs_min}}"},
        {"code": "cmp_ge", "a": "dg_src_ok", "b": "{{dg_sources}}", "c": "{{dg_src_min}}"},
        {"code": "jump_if", "a": "{{dg_obs_ok}}", "b": "dg_check_source"},
        {"code": "state_set", "a": "{{__subject}}", "b": "status", "c": "candidate"},
        {"code": "jump", "a": "dg_end"},
        {"code": "label", "a": "dg_check_source"},
        {"code": "jump_if", "a": "{{dg_src_ok}}", "b": "dg_stable"},
        {"code": "state_set", "a": "{{__subject}}", "b": "status", "c": "candidate"},
        {"code": "jump", "a": "dg_end"},
        {"code": "label", "a": "dg_stable"},
        {"code": "state_set", "a": "{{__subject}}", "b": "status", "c": "stable"},
        {"code": "label", "a": "dg_end"},
    ]


def structures() -> list[dict]:
    return [
        {
            "id": "policy.semantic.grounding",
            "content": "Mutable Memory-owned policy for semantic grounding stability.",
            "tags": ["memory", "cog.policy.mutable", "cog.semantic", "cog.evolvable"],
            "parents": [],
            "layer": "emergent",
            "generation": 31,
            "revision": 1,
            "state": {
                "stable_observations": 3,
                "stable_sources": 1,
                "relation_stable_observations": 3,
                "surface_selection": "memory-owned",
                "kernel_language_policy": "none",
            },
        },
        memory(
            "semantic.ground.observe.parent",
            "Convert repeated raw surface observations into mutable Memory-owned grounding structures.",
            ["cog.semantic", "cog.semantic.ground"],
            grounding_program(),
            trigger=["event:experience.raw", "subject_tag:cog.experience.raw"],
            parents=["policy.semantic.grounding"],
        ),
        memory(
            "semantic.direct_ground.policy.parent",
            "Evaluate candidate/stable grounding status using mutable Memory policy.",
            ["cog.semantic", "cog.semantic.ground", "cog.policy"],
            direct_ground_policy_program(),
            trigger=["event:semantic.ground.observed", "subject_tag:cog.semantic.ground"],
            parents=["policy.semantic.grounding"],
        ),
        memory(
            "semantic.relation.learn.parent",
            "Induce and stabilize predicate relations from repeated grounded observations.",
            ["cog.semantic", "cog.semantic.relation"],
            relation_program(),
            trigger=["event:semantic.relation.observed", "subject_tag:cog.semantic.relation.observation"],
            parents=["policy.semantic.grounding"],
        ),
        memory(
            "semantic.role.resolve.parent",
            "Resolve interaction roles through learned surface grounding instead of hard-coded language rules.",
            ["cog.semantic", "cog.semantic.role"],
            role_resolve_program(),
            trigger=["event:interaction.raw", "subject_tag:cog.interaction.raw"],
            parents=["policy.semantic.grounding"],
        ),
        memory(
            "semantic.expression.frame.parent",
            "Project a stable relation into a semantic output frame; natural-language rendering remains Memory/presentation owned.",
            ["cog.semantic", "cog.expression"],
            expression_frame_program(),
            trigger=["event:semantic.answer.request", "subject_tag:cog.answer.request"],
            parents=["policy.semantic.grounding"],
        ),
    ]


def upsert(memories: list[dict], item: dict) -> None:
    for i, current in enumerate(memories):
        if current.get("id") == item["id"]:
            revision = int(current.get("revision", 0)) + 1
            replacement = dict(item)
            replacement["revision"] = revision
            memories[i] = replacement
            return
    memories.append(item)


def migrate(doc: dict) -> dict:
    memories = doc["memories"]
    for item in structures():
        upsert(memories, item)
    doc["version"] = "28.8-language-semantics-reproducible"
    return doc


def audit(doc: dict) -> None:
    by_id = {m["id"]: m for m in doc["memories"]}
    required = {
        "policy.semantic.grounding",
        "semantic.ground.observe.parent",
        "semantic.direct_ground.policy.parent",
        "semantic.relation.learn.parent",
        "semantic.role.resolve.parent",
        "semantic.expression.frame.parent",
    }
    missing = sorted(required - set(by_id))
    if missing:
        raise RuntimeError(f"missing semantic structures: {missing}")

    policy = by_id["policy.semantic.grounding"]["state"]
    if int(policy.get("stable_observations", 0)) < 2:
        raise RuntimeError("semantic grounding stability cannot be one-shot")
    if int(policy.get("stable_sources", 0)) < 1:
        raise RuntimeError("semantic grounding must preserve provenance")

    encoded = json.dumps([by_id[mid] for mid in sorted(required)], ensure_ascii=False)
    forbidden = (
        "苹果",
        "水果",
        "test.role.question",
        "test.role.answer",
        "surface:苹果",
        "surface:水果",
    )
    for literal in forbidden:
        if literal in encoded:
            raise RuntimeError(f"language migration contains test-specific literal: {literal}")

    for mid in required - {"policy.semantic.grounding"}:
        program = by_id[mid].get("program") or []
        if not program:
            raise RuntimeError(f"{mid} is not executable")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("input", type=Path)
    ap.add_argument("output", type=Path)
    ap.add_argument("--report", type=Path)
    ap.add_argument(
        "--allow-different-input",
        action="store_true",
        help="Allow migration from a different source hash for forward-port recovery; the observed hash is still reported.",
    )
    args = ap.parse_args()

    raw = args.input.read_bytes()
    got = sha256_bytes(raw)
    if got != EXPECTED_INPUT_SHA256 and not args.allow_different_input:
        raise RuntimeError(
            f"refusing unexpected v28.7 predecessor: expected {EXPECTED_INPUT_SHA256}, got {got}"
        )

    doc = migrate(json.loads(raw))
    audit(doc)

    encoded = (json.dumps(doc, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode()
    args.output.write_bytes(encoded)

    report = {
        "input_sha256": got,
        "output_sha256": sha256_bytes(encoded),
        "memory_count": len(doc["memories"]),
        "semantic_structures": 6,
        "test_specific_language_literals": 0,
        "kernel_language_policy": "none",
        "status": "PASS",
    }
    if args.report:
        args.report.write_text(
            json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
    print(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
