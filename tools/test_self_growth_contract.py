#!/usr/bin/env python3
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SEED = ROOT / "Kernel/current-required-structures.json"
KERNEL = ROOT / "Kernel/src/kernel.go"

class SelfGrowthContractTest(unittest.TestCase):
    def setUp(self):
        data = json.loads(SEED.read_text(encoding="utf-8"))
        self.mem = {m["id"]: m for m in data["memories"]}

    def test_recombination_fission_and_outcome_history_are_memory_owned(self):
        for mid in ("evolution.recombine.parent", "evolution.fission.parent", "execution.outcome.record.parent"):
            self.assertIn(mid, self.mem, mid)
        recombine = json.dumps(self.mem["evolution.recombine.parent"], ensure_ascii=False)
        fission = json.dumps(self.mem["evolution.fission.parent"], ensure_ascii=False)
        history = json.dumps(self.mem["execution.outcome.record.parent"], ensure_ascii=False)
        self.assertIn("program_insert_from", recombine)
        self.assertIn("program_delete", fission)
        self.assertIn("memory_history_append", history)
        self.assertIn("success_history", history)
        self.assertIn("failure_history", history)
        self.assertNotIn("runtime_exec_count", history)

    def test_body_fusion_is_four_stage_memory_decision_chain(self):
        for mid in ("memory.fusion.discover.parent", "memory.fusion.evaluate.parent",
                    "memory.fusion.decide.parent", "memory.fusion.execute.parent"):
            self.assertIn(mid, self.mem, mid)
        execute = json.dumps(self.mem["memory.fusion.execute.parent"], ensure_ascii=False)
        self.assertIn("space_merge", execute)
        for mid in ("memory.fusion.discover.parent", "memory.fusion.evaluate.parent", "memory.fusion.decide.parent"):
            self.assertNotIn("space_merge", json.dumps(self.mem[mid], ensure_ascii=False))

    def test_kernel_space_merge_does_not_reconcile_semantics(self):
        src = KERNEL.read_text(encoding="utf-8")
        start = src.index("func (e *Engine) mergeSpace")
        end = src.index("\nfunc remoteEndpointKey", start)
        body = src[start:end]
        self.assertNotIn("mergeSameIdentity", body)
        self.assertNotIn('importMemory(m, dst, "reconcile")', body)
        self.assertNotIn("merge_state_conflicts", body)

if __name__ == "__main__":
    unittest.main()
