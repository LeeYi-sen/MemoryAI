#!/usr/bin/env python3
from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from build_current_memory_seed import DEFAULT_MANIFEST, build_current_memory_seed

LANGUAGE_REPRO_SHA = "9223866b6921c4eefa486e3b34597fabe0b8c823eb79a04a465e441b50128b80"
FINAL_V29_SHA = "49c002cdfb99a6ae49213634be348fe83de126ad31b2a75fdcdeaa5245fa29b2"


class CurrentMemorySeedChainTest(unittest.TestCase):
    def test_reproducible_language_seed_flows_into_v29(self) -> None:
        with tempfile.TemporaryDirectory(prefix="memoryai-seed-test-") as tmp:
            report = build_current_memory_seed(
                DEFAULT_MANIFEST,
                Path(tmp) / "current-required-structures.json",
            )

        stages = {item["stage"]: item["sha256"] for item in report["stages"]}
        self.assertEqual(stages["v28_language_semantics.py"], LANGUAGE_REPRO_SHA)
        self.assertEqual(report["output_sha256"], FINAL_V29_SHA)
        self.assertEqual(report["version"], "29.0-memory-runtime-ownership")


if __name__ == "__main__":
    unittest.main()
