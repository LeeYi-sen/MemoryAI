#!/usr/bin/env python3
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SEED = ROOT / "Kernel/current-required-structures.json"


class SemanticGraphContractTest(unittest.TestCase):
    def setUp(self):
        data = json.loads(SEED.read_text(encoding="utf-8"))
        self.memories = {m["id"]: m for m in data["memories"]}

    def test_live_semantic_and_reply_bridges_exist(self):
        for mid in (
            "semantic.experience.bridge.parent",
            "semantic.relation.bridge.parent",
            "answer.request.from.experience.parent",
            "expression.surface.render.parent",
            "reply.capture.parent",
        ):
            self.assertIn(mid, self.memories, mid)
        answer = self.memories["answer.request.from.experience.parent"]
        encoded = json.dumps(answer, ensure_ascii=False)
        self.assertIn("semantic.answer.request", encoded)
        renderer = json.dumps(self.memories["expression.surface.render.parent"], ensure_ascii=False)
        self.assertIn("expression.frame.ready", renderer)
        self.assertIn("reply.ready", renderer)

    def test_initial_chinese_grounding_is_memory_owned(self):
        encoded = json.dumps(list(self.memories.values()), ensure_ascii=False)
        self.assertIn("你", encoded)
        self.assertIn("谁", encoded)
        self.assertIn("我", encoded)
        self.assertIn("MemoryAI", encoded)
        self.assertIn("cog.surface.grounding.stable", encoded)
        self.assertIn("cog.concept.query", encoded)

    def test_v31_dead_events_have_memory_producers(self):
        produced = set()
        for memory in self.memories.values():
            for op in memory.get("program", []):
                if op.get("code") == "emit_event" and op.get("a"):
                    produced.add(op["a"])
        for event in (
            "experience.raw",
            "semantic.ground.observed",
            "semantic.relation.observed",
            "interaction.raw",
            "semantic.answer.request",
        ):
            self.assertIn(event, produced, event)


if __name__ == "__main__":
    unittest.main()
