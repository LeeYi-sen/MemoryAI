#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import shutil
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
AUDIT_PATH = ROOT / "tools/architecture_audit.py"


def load_audit_module():
    spec = importlib.util.spec_from_file_location("memoryai_architecture_audit", AUDIT_PATH)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def normalized_seed_copy(repo: Path) -> dict:
    path = repo / "Kernel/current-required-structures.json"
    payload = json.loads(path.read_text(encoding="utf-8"))
    replacements = {
        "cognition_concurrency_set": "physical_execution_concurrency_set",
        "cognition_stats": "physical_runtime_stats",
        "mesh_route_cognition": "mesh_route_execution",
    }
    for memory in payload["memories"]:
        for op in memory.get("program") or []:
            op["code"] = replacements.get(op.get("code"), op.get("code"))
        required = {op.get("code") for op in memory.get("program") or []}
        if "emit_event" in required and "event.emit" not in (memory.get("capabilities") or []):
            memory.setdefault("capabilities", []).append("event.emit")
    return payload


class ArchitectureAuditMemoryABITest(unittest.TestCase):
    def with_repo(self):
        tmp = tempfile.TemporaryDirectory()
        repo = Path(tmp.name) / "MemoryAI"
        shutil.copytree(ROOT, repo, ignore=shutil.ignore_patterns(".git", "__pycache__"))
        return tmp, repo

    def run_audit(self, repo: Path):
        module = load_audit_module()
        module.ROOT = repo
        module.SRC = repo / "Kernel/src"
        return module.audit()

    def test_rejects_memory_opcode_not_implemented_by_kernel(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        payload = normalized_seed_copy(repo)
        payload["memories"][0]["program"] = [{"code": "definitely_not_a_primitive"}]
        (repo / "Kernel/current-required-structures.json").write_text(json.dumps(payload), encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "unsupported Memory opcode"):
            self.run_audit(repo)

    def test_rejects_privileged_opcode_without_declared_capability(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        payload = normalized_seed_copy(repo)
        target = next(m for m in payload["memories"] if any(op.get("code") == "emit_event" for op in m.get("program") or []))
        target["capabilities"] = [c for c in target.get("capabilities") or [] if c != "event.emit"]
        (repo / "Kernel/current-required-structures.json").write_text(json.dumps(payload), encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "missing capability"):
            self.run_audit(repo)


if __name__ == "__main__":
    unittest.main()
