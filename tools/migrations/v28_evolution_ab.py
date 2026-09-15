#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, json
from pathlib import Path

EXPECTED_INPUT_SHA256 = "68883bb5303fd3d925e8c99980ed2fc9db9d470f225de0ce694f8684fc33d277"
EXPECTED_OUTPUT_SHA256 = "3689e0d596b75eeb58a9d018fe24c75f492928b8a62cad68418013bb92c9642c"
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

def dispatcher_program(mid: str, base: str) -> list[dict]:
    return [
        {"code": "state_get", "a": mid, "b": "active_implementation", "c": "__evo_active"},
        {"code": "var_default", "a": "__evo_active", "b": base},
        {"code": "state_get", "a": mid, "b": "challenger", "c": "__evo_ch"},
        {"code": "cmp_eq", "a": "__evo_noch", "b": "{{__evo_ch}}"},
        {"code": "jump_if", "a": "{{__evo_noch}}", "b": "__evo_use_active"},
        {"code": "state_get", "a": mid, "b": "ab_next_challenger", "c": "__evo_turn"},
        {"code": "var_default", "a": "__evo_turn", "b": "0"},
        {"code": "cmp_eq", "a": "__evo_usech", "b": "{{__evo_turn}}", "c": "1"},
        {"code": "jump_if", "a": "{{__evo_usech}}", "b": "__evo_use_ch"},
        {"code": "label", "a": "__evo_use_active"},
        {"code": "copy", "a": "__evo_exec", "b": "__evo_active"},
        {"code": "cmp_eq", "a": "__evo_still_noch", "b": "{{__evo_ch}}"},
        {"code": "jump_if", "a": "{{__evo_still_noch}}", "b": "__evo_run"},
        {"code": "state_set", "a": mid, "b": "ab_next_challenger", "c": "1"},
        {"code": "jump", "a": "__evo_run"},
        {"code": "label", "a": "__evo_use_ch"},
        {"code": "copy", "a": "__evo_exec", "b": "__evo_ch"},
        {"code": "state_set", "a": mid, "b": "ab_next_challenger", "c": "0"},
        {"code": "label", "a": "__evo_run"},
        {"code": "call", "a": "{{__evo_exec}}"},
        {"code": "state_set", "a": mid, "b": "last_execution", "c": "{{__evo_exec}}"},
        {"code": "copy", "a": "__executed_implementation", "b": "__evo_exec"},
    ]

def feedback_program() -> list[dict]:
    return [
        {"code":"copy","a":"ef_sid","b":"__event.structure_id"},
        {"code":"cmp_eq","a":"ef_none","b":"{{ef_sid}}"},
        {"code":"jump_if","a":"{{ef_none}}","b":"ef_end"},
        {"code":"copy","a":"ef_gain","b":"__event.gain"}, {"code":"var_default","a":"ef_gain","b":"0"},
        {"code":"copy","a":"ef_cost","b":"__event.cost"}, {"code":"var_default","a":"ef_cost","b":"0"},
        {"code":"state_num_add","a":"{{ef_sid}}","b":"cog.metric.trials","c":"1"},
        {"code":"state_num_add","a":"{{ef_sid}}","b":"cog.metric.reward","c":"{{ef_gain}}"},
        {"code":"state_num_add","a":"{{ef_sid}}","b":"cog.metric.cost","c":"{{ef_cost}}"},
        {"code":"state_get","a":"{{ef_sid}}","b":"cog.metric.trials","c":"ef_t"},
        {"code":"state_get","a":"{{ef_sid}}","b":"cog.metric.reward","c":"ef_r"}, {"code":"var_default","a":"ef_r","b":"0"},
        {"code":"state_get","a":"{{ef_sid}}","b":"cog.metric.cost","c":"ef_c"}, {"code":"var_default","a":"ef_c","b":"0"},
        {"code":"num_sub","a":"ef_net","b":"{{ef_r}}","c":"{{ef_c}}"},
        {"code":"state_get","a":"{{ef_sid}}","b":"logical_dispatcher","c":"ef_ld"},
        {"code":"cmp_eq","a":"ef_nold","b":"{{ef_ld}}"},
        {"code":"jump_if","a":"{{ef_nold}}","b":"ef_legacy"},
        {"code":"state_get","a":"{{ef_ld}}","b":"active_implementation","c":"ef_active"},
        {"code":"state_get","a":"{{ef_ld}}","b":"challenger","c":"ef_ch"},
        {"code":"cmp_eq","a":"ef_is_ch","b":"{{ef_sid}}","c":"{{ef_ch}}"},
        {"code":"jump_if","a":"{{ef_is_ch}}","b":"ef_eval_ch"},
        {"code":"cmp_eq","a":"ef_is_active","b":"{{ef_sid}}","c":"{{ef_active}}"},
        {"code":"jump_if","a":"{{ef_is_active}}","b":"ef_eval_active"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_eval_ch"},
        {"code":"state_get","a":"policy.evolution","b":"holdout_trials","c":"ef_hold"},
        {"code":"cmp_ge","a":"ef_ch_ready","b":"{{ef_t}}","c":"{{ef_hold}}"},
        {"code":"jump_if","a":"{{ef_ch_ready}}","b":"ef_compare_ch"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_compare_ch"},
        {"code":"state_get","a":"{{ef_active}}","b":"cog.metric.reward","c":"ef_ar"}, {"code":"var_default","a":"ef_ar","b":"0"},
        {"code":"state_get","a":"{{ef_active}}","b":"cog.metric.cost","c":"ef_ac"}, {"code":"var_default","a":"ef_ac","b":"0"},
        {"code":"num_sub","a":"ef_anet","b":"{{ef_ar}}","c":"{{ef_ac}}"},
        {"code":"num_sub","a":"ef_improve","b":"{{ef_net}}","c":"{{ef_anet}}"},
        {"code":"state_get","a":"policy.evolution","b":"min_improvement","c":"ef_minimp"},
        {"code":"cmp_ge","a":"ef_win","b":"{{ef_improve}}","c":"{{ef_minimp}}"},
        {"code":"jump_if","a":"{{ef_win}}","b":"ef_promote"},
        {"code":"state_set","a":"{{ef_sid}}","b":"disabled","c":"1"},
        {"code":"memory_tag_add","a":"{{ef_sid}}","b":"cog.evolution.rejected"},
        {"code":"state_set","a":"{{ef_ld}}","b":"challenger","c":""},
        {"code":"emit_event","a":"structure.variant.rejected","b":"{{ef_sid}}"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_promote"},
        {"code":"state_set","a":"{{ef_ld}}","b":"previous_implementation","c":"{{ef_active}}"},
        {"code":"state_set","a":"{{ef_ld}}","b":"active_implementation","c":"{{ef_sid}}"},
        {"code":"state_set","a":"{{ef_ld}}","b":"challenger","c":""},
        {"code":"state_set","a":"{{ef_sid}}","b":"promoted","c":"1"},
        {"code":"memory_tag_add","a":"{{ef_sid}}","b":"cog.evolution.promoted"},
        {"code":"emit_event","a":"structure.promoted","b":"{{ef_sid}}"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_eval_active"},
        {"code":"state_get","a":"{{ef_ld}}","b":"previous_implementation","c":"ef_prev"},
        {"code":"cmp_eq","a":"ef_noprev","b":"{{ef_prev}}"},
        {"code":"jump_if","a":"{{ef_noprev}}","b":"ef_active_mutation_gate"},
        {"code":"state_get","a":"policy.evolution","b":"holdout_trials","c":"ef_hold2"},
        {"code":"cmp_ge","a":"ef_active_ready","b":"{{ef_t}}","c":"{{ef_hold2}}"},
        {"code":"jump_if","a":"{{ef_active_ready}}","b":"ef_check_rollback"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_check_rollback"},
        {"code":"state_get","a":"{{ef_prev}}","b":"cog.metric.reward","c":"ef_pr"}, {"code":"var_default","a":"ef_pr","b":"0"},
        {"code":"state_get","a":"{{ef_prev}}","b":"cog.metric.cost","c":"ef_pc"}, {"code":"var_default","a":"ef_pc","b":"0"},
        {"code":"num_sub","a":"ef_pnet","b":"{{ef_pr}}","c":"{{ef_pc}}"},
        {"code":"num_sub","a":"ef_gap","b":"{{ef_pnet}}","c":"{{ef_net}}"},
        {"code":"state_get","a":"policy.evolution","b":"rollback_margin","c":"ef_rb"},
        {"code":"cmp_ge","a":"ef_rollback","b":"{{ef_gap}}","c":"{{ef_rb}}"},
        {"code":"jump_if","a":"{{ef_rollback}}","b":"ef_do_rollback"},
        {"code":"jump","a":"ef_active_mutation_gate"},
        {"code":"label","a":"ef_do_rollback"},
        {"code":"state_set","a":"{{ef_ld}}","b":"active_implementation","c":"{{ef_prev}}"},
        {"code":"state_set","a":"{{ef_ld}}","b":"previous_implementation","c":""},
        {"code":"state_set","a":"{{ef_ld}}","b":"challenger","c":""},
        {"code":"state_set","a":"{{ef_sid}}","b":"rolled_back","c":"1"},
        {"code":"memory_tag_add","a":"{{ef_sid}}","b":"cog.evolution.rolled_back"},
        {"code":"emit_event","a":"structure.rollback","b":"{{ef_sid}}"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_active_mutation_gate"},
        {"code":"cmp_eq","a":"ef_noch","b":"{{ef_ch}}"},
        {"code":"jump_if","a":"{{ef_noch}}","b":"ef_maybe_mutate"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_maybe_mutate"},
        {"code":"state_get","a":"policy.evolution","b":"min_trials","c":"ef_min"},
        {"code":"cmp_ge","a":"ef_enough","b":"{{ef_t}}","c":"{{ef_min}}"},
        {"code":"jump_if","a":"{{ef_enough}}","b":"ef_mutthr"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_mutthr"},
        {"code":"state_get","a":"policy.evolution","b":"mutate_below_net_reward","c":"ef_badthr"},
        {"code":"cmp_gt","a":"ef_bad","b":"{{ef_badthr}}","c":"{{ef_net}}"},
        {"code":"jump_if","a":"{{ef_bad}}","b":"ef_mutate"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_legacy"},
        {"code":"state_get","a":"policy.evolution","b":"min_trials","c":"ef_lmin"},
        {"code":"cmp_ge","a":"ef_lenough","b":"{{ef_t}}","c":"{{ef_lmin}}"},
        {"code":"jump_if","a":"{{ef_lenough}}","b":"ef_lthr"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_lthr"},
        {"code":"state_get","a":"policy.evolution","b":"mutate_below_net_reward","c":"ef_lbadthr"},
        {"code":"cmp_gt","a":"ef_lbad","b":"{{ef_lbadthr}}","c":"{{ef_net}}"},
        {"code":"jump_if","a":"{{ef_lbad}}","b":"ef_mutate"},
        {"code":"jump","a":"ef_end"},
        {"code":"label","a":"ef_mutate"},
        {"code":"state_get","a":"policy.evolution","b":"max_variants","c":"ef_maxv"},
        {"code":"tag_list","a":"cog.evolution.variant.parent.{{ef_sid}}","b":"ef_vs"},
        {"code":"list_len","a":"ef_vs","b":"ef_vn"},
        {"code":"cmp_ge","a":"ef_vfull","b":"{{ef_vn}}","c":"{{ef_maxv}}"},
        {"code":"jump_if","a":"{{ef_vfull}}","b":"ef_end"},
        {"code":"memory_copy","a":"ef_var","b":"{{ef_sid}}","args":{"layer":"emergent","tags":"cog.evolution.variant,cog.evolvable,cog.evolution.variant.parent.{{ef_sid}}"}},
        {"code":"state_set","a":"{{ef_var}}","b":"mutation_parent","c":"{{ef_sid}}"},
        {"code":"state_set","a":"{{ef_var}}","b":"logical_dispatcher","c":"{{ef_ld}}"},
        {"code":"state_set","a":"{{ef_var}}","b":"cog.metric.trials","c":"0"},
        {"code":"state_set","a":"{{ef_var}}","b":"cog.metric.reward","c":"0"},
        {"code":"state_set","a":"{{ef_var}}","b":"cog.metric.cost","c":"0"},
        {"code":"state_set","a":"{{ef_var}}","b":"promoted","c":"0"},
        {"code":"state_set","a":"{{ef_var}}","b":"rolled_back","c":"0"},
        {"code":"state_set","a":"{{ef_var}}","b":"disabled","c":"0"},
        {"code":"copy","a":"variant_id","b":"ef_var"},
        {"code":"call","a":"evolution.mutate.parent"},
        {"code":"cmp_eq","a":"ef_nomld","b":"{{ef_ld}}"},
        {"code":"jump_if","a":"{{ef_nomld}}","b":"ef_variant_emit"},
        {"code":"state_set","a":"{{ef_ld}}","b":"challenger","c":"{{ef_var}}"},
        {"code":"state_set","a":"{{ef_ld}}","b":"ab_next_challenger","c":"0"},
        {"code":"label","a":"ef_variant_emit"},
        {"code":"emit_event","a":"structure.variant.born","b":"{{ef_var}}"},
        {"code":"label","a":"ef_end"},
    ]

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
    by_id = {m["id"]: m for m in doc["memories"]}
    policy = by_id["policy.evolution"]
    state = dict(policy.get("state") or {})
    state.update({"holdout_trials": "3", "min_improvement": "0.05", "rollback_margin": "0.10"})
    policy["state"] = state
    policy["revision"] = int(policy.get("revision", 0)) + 1
    for mid in DISPATCH_IDS:
        logical = by_id[mid]
        logical["program"] = dispatcher_program(mid, mid + ".impl.base")
        capabilities = list(logical.get("capabilities") or [])
        if "memory.write" not in capabilities:
            capabilities.append("memory.write")
        logical["capabilities"] = capabilities
        logical_state = dict(logical.get("state") or {})
        logical_state.setdefault("ab_next_challenger", "0")
        logical_state.setdefault("previous_implementation", "")
        logical["state"] = logical_state
        logical["revision"] = int(logical.get("revision", 0)) + 1
    feedback = by_id["evolution.feedback.parent"]
    feedback["program"] = feedback_program()
    feedback["revision"] = int(feedback.get("revision", 0)) + 1
    doc["version"] = "28.3.0-evolution-ab-holdout"
    encoded = (json.dumps(doc, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")
    output_sha = sha256(encoded)
    if output_sha != EXPECTED_OUTPUT_SHA256:
        raise RuntimeError(f"unexpected output hash {output_sha}")
    args.output.write_bytes(encoded)
    print(json.dumps({"input_sha256": actual, "output_sha256": output_sha, "memory_count": len(doc["memories"])}, indent=2, sort_keys=True))
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
