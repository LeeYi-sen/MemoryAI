#!/usr/bin/env python3
from __future__ import annotations

import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def tracked_files() -> set[str]:
    out = subprocess.check_output(["git", "ls-files"], cwd=ROOT, text=True)
    return {line.strip() for line in out.splitlines() if line.strip()}


class RepositorySourceLayoutTest(unittest.TestCase):
    def test_repository_tracks_direct_source_and_memory_body(self) -> None:
        tracked = tracked_files()
        self.assertIn("Kernel/src/kernel.go", tracked)
        self.assertIn("Kernel/current-required-structures.json", tracked)
        self.assertIn("data/Memory.mem", tracked)

    def test_repository_has_no_chunked_or_packaged_bootstrap_payloads(self) -> None:
        tracked = tracked_files()
        forbidden = sorted(
            path for path in tracked
            if ".gz.b64.part" in path or path.endswith(".bootstrap.json")
        )
        self.assertEqual(forbidden, [], f"forbidden packaged bootstrap payloads: {forbidden}")

    def test_development_repository_has_no_release_builder(self) -> None:
        tracked = tracked_files()
        self.assertNotIn("tools/build_release.py", tracked)
        self.assertNotIn("tools/test_build_release_cli.py", tracked)

    def test_gitignore_keeps_memory_body_but_rejects_binary_packages(self) -> None:
        ignore = (ROOT / ".gitignore").read_text(encoding="utf-8")
        self.assertNotIn("data/Memory.mem", ignore)
        for pattern in ("*.zip", "*.tar", "*.gz", "*.7z", "*.rar", "*.so", "*.dylib", "*.o", "*.a"):
            self.assertIn(pattern, ignore)


# Direct-source repositories must not retain the historical source-reconstruction
# pipeline after the canonical source has been materialized in Git.
class RepositoryNoReconstructionScaffoldTest(unittest.TestCase):
    def test_obsolete_reconstruction_tools_are_absent(self) -> None:
        tracked = tracked_files()
        forbidden = {
            "tools/restore_source.py",
            "tools/build_current_memory_seed.py",
            "tools/test_kernel_remote_body_txn_overlay.py",
        }
        forbidden.update(
            path for path in tracked
            if path.startswith("tools/kernel_") and path.endswith("_overlay.py")
        )
        self.assertEqual(sorted(forbidden & tracked), [])


if __name__ == "__main__":
    unittest.main()
