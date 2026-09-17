#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SEED = ROOT / "Kernel/current-required-structures.json"
CURRENT_SEED_SHA = "f96050776140416a3cc4f3852509c64b6afd48649ec289c24752a8eab1c543b3"


class CurrentMemorySeedSourceTest(unittest.TestCase):
    def test_tracked_seed_is_current_memory_source(self) -> None:
        raw = SEED.read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), CURRENT_SEED_SHA)
        data = json.loads(raw)
        self.assertEqual(data["version"], "29.0-memory-runtime-ownership")
        self.assertEqual(data["format"], "memoryai-required-structures-v1")
        self.assertEqual(len(data["memories"]), 128)


if __name__ == "__main__":
    unittest.main()
