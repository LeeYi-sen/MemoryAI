#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SEED = ROOT / "Kernel/current-required-structures.json"
FINAL_V29_SHA = "ab90e81cf7ca55cfa404a350b5f386aa231c3bab291b39a8bdc99dc73aa4647e"


class CurrentMemorySeedSourceTest(unittest.TestCase):
    def test_tracked_seed_is_current_v29_memory_source(self) -> None:
        raw = SEED.read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), FINAL_V29_SHA)
        data = json.loads(raw)
        self.assertEqual(data["version"], "29.0-memory-runtime-ownership")
        self.assertEqual(data["format"], "memoryai-required-structures-v1")
        self.assertEqual(len(data["memories"]), 99)


if __name__ == "__main__":
    unittest.main()
