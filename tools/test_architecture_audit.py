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

    def test_rejects_topk_vocabulary_in_activation_callers(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        payload = normalized_seed_copy(repo)
        (repo / "Kernel/current-required-structures.json").write_text(json.dumps(payload), encoding="utf-8")
        daemon = repo / "Kernel/src/daemon_runtime.go"
        daemon.write_text(daemon.read_text(encoding="utf-8") + "\nvar topK int\n", encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "topK"):
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


    def test_rejects_cognitive_trace_namespace_in_kernel(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(kernel.read_text(encoding="utf-8") + '\nvar traceCognitionLeak = "cog.surface."\n', encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "cognitive namespace"):
            self.run_audit(repo)

    def test_rejects_active_cog_storage_namespace_in_kernel(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(kernel.read_text(encoding="utf-8") + '\nvar storageNamespaceLeak = "cog.storage.remote.endpoint"\n', encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "storage namespace"):
            self.run_audit(repo)



    def test_rejects_legacy_nondurable_mesh_flush_path(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        obs = repo / "Kernel/src/mesh_observability_runtime.go"
        obs.write_text(obs.read_text(encoding="utf-8") + "\nfunc (m *meshRuntime) flushJournal() error { return nil }\n", encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "non-durable Mesh journal"):
            self.run_audit(repo)



    def test_rejects_kernel_owned_automatic_shard_creation(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        shard = repo / "Kernel/src/shard_runtime.go"
        shard.write_text(shard.read_text(encoding="utf-8") + "\nfunc createAutomaticWritableShard() {}\n", encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "Memory-owned expansion"):
            self.run_audit(repo)

    def test_rejects_remote_conflict_identity_branching(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(kernel.read_text(encoding="utf-8") + '\nvar conflictBranchLeak = "merge_source_id"\n', encoding="utf-8")
        with self.assertRaisesRegex(RuntimeError, "physical remote conflict"):
            self.run_audit(repo)

    def test_requires_unified_structural_digest(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        persistence = repo / "Kernel/src/persistence_runtime.go"
        persistence.write_text(
            persistence.read_text(encoding="utf-8").replace(
                "return memoryJSONDigest(m)", 'return "divergent-digest"'
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "unified structural Memory digest"):
            self.run_audit(repo)

    def test_requires_authenticated_remote_storage_transport(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        security = repo / "Kernel/src/storage_transport_security.go"
        security.write_text(
            security.read_text(encoding="utf-8").replace(
                "non-loopback Memory storage listener requires",
                "plaintext remote storage allowed",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "remote storage transport security"):
            self.run_audit(repo)


    def test_rejects_revision_increment_without_signature_invalidation(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        src = repo / "Kernel/src/source_adapter_runtime.go"
        src.write_text(
            src.read_text(encoding="utf-8")
            + "\nfunc staleSignatureRegression(q *Memory) { q.Revision++ }\n",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "stale CapabilitySig"):
            self.run_audit(repo)

    def test_rejects_structure_sync_revision_bump_without_signature_clear(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        src = repo / "Kernel/src/structure_runtime.go"
        src.write_text(
            src.read_text(encoding="utf-8").replace(
                'q.CapabilitySig = ""\n\t\t\tq.Revision = current.Revision + 1',
                'q.Revision = current.Revision + 1',
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "structure-sync revision bump"):
            self.run_audit(repo)


    def test_rejects_single_target_tag_first_wins(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'return nil, fmt.Errorf("ambiguous Memory tag %q matched %d identities; Memory must select an explicit ID", idOrTag, len(ids))',
                'return e.resolveID(ids[0])',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "single-target"):
            self.run_audit(repo)


    def test_requires_remote_storage_compare_and_swap_fence(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "remote replace compare-and-swap conflict",
                "remote replace without compare-and-swap",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "single-target boundary|remote storage"):
            self.run_audit(repo)


    def test_requires_memory_new_parent_resolution_fence(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'return -1, fmt.Errorf("memory_new parent %q unresolved: %w", pid, er)',
                'continue',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "single-target boundary|remote storage"):
            self.run_audit(repo)


if __name__ == "__main__":
    unittest.main()
