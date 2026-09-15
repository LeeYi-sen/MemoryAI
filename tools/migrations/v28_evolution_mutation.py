#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, json
from pathlib import Path

EXPECTED_INPUT_SHA256 = "43811560f77e2b94fa6627e32972fc71af10a0452074c77277cc90a953cb237b"
EXPECTED_OUTPUT_SHA256 = "68883bb5303fd3d925e8c99980ed2fc9db9d470f225de0ce694f8684fc33d277"

def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()

def mutation_memory() -> dict:
    return {
        "id": "evolution.mutate.parent",
        "layer": "inherited",
        "generation": 28,
        "revision": 1,
        "content": "Memory-owned executable mutation strategy. Kernel exposes only generic program inspection/edit primitives.",
        "tags": ["memory", "cog.evolution", "cog.evolution.mutation", "cog.evolvable"],
        "capabilities": ["program.mutate", "memory.write", "event.emit"],
        "budget": {
            "max_ops": 4000,
            "max_cpu_us": 300000,
            "max_memory_writes": 64,
            "max_events": 32,
            "max_alloc_bytes": 16777216,
            "max_heap_growth_bytes": 33554432,
        },
        "state": {"strategy": "first-comparator-boundary-variation"},
        "program": [
            {"code": "cmp_eq", "a": "em_none", "b": "{{variant_id}}"},
            {"code": "jump_if", "a": "{{em_none}}", "b": "em_end"},
            {"code": "program_len", "a": "{{variant_id}}", "b": "em_n"},
            {"code": "set", "a": "em_i", "b": "0"},
            {"code": "label", "a": "em_loop"},
            {"code": "cmp_ge", "a": "em_done", "b": "{{em_i}}", "c": "{{em_n}}"},
            {"code": "jump_if", "a": "{{em_done}}", "b": "em_end"},
            {"code": "program_op_get", "a": "{{variant_id}}", "b": "{{em_i}}", "args": {"code_out": "em_code", "a_out": "em_a", "b_out": "em_b", "c_out": "em_c"}},
            {"code": "cmp_eq", "a": "em_gt", "b": "{{em_code}}", "c": "cmp_gt"},
            {"code": "jump_if", "a": "{{em_gt}}", "b": "em_to_ge"},
            {"code": "cmp_eq", "a": "em_ge", "b": "{{em_code}}", "c": "cmp_ge"},
            {"code": "jump_if", "a": "{{em_ge}}", "b": "em_to_gt"},
            {"code": "num_add", "a": "em_i", "b": "{{em_i}}", "c": "1"},
            {"code": "jump", "a": "em_loop"},
            {"code": "label", "a": "em_to_ge"},
            {"code": "program_set_field", "a": "{{variant_id}}", "b": "{{em_i}}", "c": "cmp_ge", "args": {"field": "code"}},
            {"code": "set", "a": "em_from", "b": "cmp_gt"},
            {"code": "set", "a": "em_to", "b": "cmp_ge"},
            {"code": "jump", "a": "em_record"},
            {"code": "label", "a": "em_to_gt"},
            {"code": "program_set_field", "a": "{{variant_id}}", "b": "{{em_i}}", "c": "cmp_gt", "args": {"field": "code"}},
            {"code": "set", "a": "em_from", "b": "cmp_ge"},
            {"code": "set", "a": "em_to", "b": "cmp_gt"},
            {"code": "label", "a": "em_record"},
            {"code": "state_set", "a": "{{variant_id}}", "b": "mutation_index", "c": "{{em_i}}"},
            {"code": "state_set", "a": "{{variant_id}}", "b": "mutation_field", "c": "code"},
            {"code": "state_set", "a": "{{variant_id}}", "b": "mutation_from", "c": "{{em_from}}"},
            {"code": "state_set", "a": "{{variant_id}}", "b": "mutation_to", "c": "{{em_to}}"},
            {"code": "state_set", "a": "{{variant_id}}", "b": "mutation_strategy", "c": "first-comparator-boundary-variation"},
            {"code": "emit_event", "a": "structure.mutated", "b": "{{variant_id}}"},
            {"code": "label", "a": "em_end"},
        ],
    }

def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    raw = args.input.read_bytes()
    actual = sha256(raw)
    if actual != EXPECTED_INPUT_SHA256:
        raise RuntimeError(f"refusing unexpected input {actual}")
    doc = json.loads(raw)
    ids = {m["id"] for m in doc["memories"]}
    if "evolution.mutate.parent" in ids:
        raise RuntimeError("mutation structure already exists")
    doc["memories"].append(mutation_memory())
    doc["version"] = "28.2.0-memory-owned-mutation"
    encoded = (json.dumps(doc, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")
    output_sha = sha256(encoded)
    if output_sha != EXPECTED_OUTPUT_SHA256:
        raise RuntimeError(f"unexpected output hash {output_sha}")
    args.output.write_bytes(encoded)
    print(json.dumps({
        "input_sha256": actual,
        "output_sha256": output_sha,
        "memory_count": len(doc["memories"]),
        "added": "evolution.mutate.parent",
    }, indent=2, sort_keys=True))
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
