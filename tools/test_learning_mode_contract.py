#!/usr/bin/env python3
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SEED = ROOT / "Kernel/current-required-structures.json"


class LearningModeContractTest(unittest.TestCase):
    def setUp(self):
        data = json.loads(SEED.read_text(encoding="utf-8"))
        self.memories = {m["id"]: m for m in data["memories"]}

    def test_default_mode_is_self_only_and_memory_owned(self):
        policy = self.memories.get("policy.learning.mode")
        self.assertIsNotNone(policy, "policy.learning.mode Memory missing")
        self.assertEqual(policy.get("state", {}).get("mode"), "SELF_ONLY")
        control = self.memories.get("learning.mode.control.parent")
        self.assertIsNotNone(control, "Memory-owned learning mode control missing")
        self.assertIn("event:learning.mode.set", control.get("trigger", []))

    def test_external_research_requires_assisted_mode(self):
        research = self.memories["research.external.parent"]
        program = research.get("program", [])
        serialized = json.dumps(program, sort_keys=True)
        self.assertIn("policy.learning.mode", serialized)
        self.assertIn("ASSISTED_LEARNING", serialized)


if __name__ == "__main__":
    unittest.main()
