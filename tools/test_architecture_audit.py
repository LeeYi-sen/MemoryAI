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


    def test_rejects_clock_only_identity_generator(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        identity = repo / "Kernel/src/identity_runtime.go"
        identity.write_text(
            'package main\n\nimport ("fmt"; "time")\n\n'
            'func nextID(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }\n',
            encoding="utf-8",
        )
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'func nextID(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }',
                '',
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "identity generator"):
            self.run_audit(repo)

    def test_requires_durable_initial_body_writer(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'if err = f.Sync(); err != nil {',
                'if false {',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "initial body writer"):
            self.run_audit(repo)


    def test_rejects_unbounded_physical_exchange(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        limits = repo / "Kernel/src/physical_limits_runtime.go"
        limits.write_text(
            limits.read_text(encoding="utf-8").replace(
                "io.LimitReader(r, maxBytes+1)",
                "r",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "physical byte ceilings"):
            self.run_audit(repo)

    def test_rejects_unbounded_artifact_primitive(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'readAllPhysicalBounded(file, maxBytes, "artifact read")',
                "io.ReadAll(file)",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "single-primitive physical I/O boundary"):
            self.run_audit(repo)


    def test_rejects_symlink_unsafe_physical_path_sandbox(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        sandbox = repo / "Kernel/src/path_sandbox_runtime.go"
        sandbox.write_text(
            sandbox.read_text(encoding="utf-8").replace(
                "os.Lstat(current)",
                "os.Stat(current)",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "physical path sandbox"):
            self.run_audit(repo)

    def test_requires_memnode_and_artifact_to_share_path_sandbox(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'return physicalSandboxPath(root, name, "remote storage")',
                'return filepath.Join(root, name), nil',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "shared physical path sandbox"):
            self.run_audit(repo)


    def test_rejects_mesh_without_node_identity_signature(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        identity = repo / "Kernel/src/mesh_node_identity_runtime.go"
        identity.write_text(
            identity.read_text(encoding="utf-8").replace(
                "ed25519.Verify(pub, body, sig)",
                "true",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Mesh node identity"):
            self.run_audit(repo)

    def test_requires_sovereign_requester_identity_binding(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        authority = repo / "Kernel/src/mesh_authority_runtime.go"
        authority.write_text(
            authority.read_text(encoding="utf-8").replace(
                "shared grant requester identity mismatch",
                "shared grant requester unchecked",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Sovereign node identity binding"):
            self.run_audit(repo)

    def test_requires_node_public_key_rebind_fence(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        authority = repo / "Kernel/src/mesh_authority_runtime.go"
        authority.write_text(
            authority.read_text(encoding="utf-8").replace(
                "node identity rebind denied",
                "node identity rebound",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Sovereign node identity binding"):
            self.run_audit(repo)


    def test_rejects_unbounded_memory_metadata_zip_read(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'readZipFileBounded(file, hardJSONZipEntryMaxBytes, "Memory metadata")',
                'io.ReadAll(mustOpenZip(file))',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Memory indexed body bounds"):
            self.run_audit(repo)

    def test_requires_indexed_record_slice_bounds(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        store = repo / "Kernel/src/store.go"
        store.write_text(
            store.read_text(encoding="utf-8").replace(
                'indexedSliceBounds(s.records, e.off, e.length, hardMemoryRecordMaxBytes, "Memory record")',
                'nil',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Memory indexed body bounds"):
            self.run_audit(repo)


    def test_requires_duplicate_zip_entry_load_gate(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "validateUniqueZipEntryNames(&zr.Reader)",
                "validateUniqueZipEntryNamesDisabled(&zr.Reader)",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Memory indexed body bounds"):
            self.run_audit(repo)

    def test_requires_normal_load_integrity_gate(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "verifyBodyLoadIntegrity(&zr.Reader, mf)",
                "verifyBodyLoadIntegrityDisabled(&zr.Reader, mf)",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Memory indexed body bounds"):
            self.run_audit(repo)

    def test_requires_lazy_record_digest_verification(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        store = repo / "Kernel/src/store.go"
        store.write_text(
            store.read_text(encoding="utf-8").replace(
                "Memory record digest mismatch",
                "Memory record digest unchecked",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Memory indexed body bounds"):
            self.run_audit(repo)

    def test_rejects_v2_full_record_scan_at_load(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        source = repo / "Kernel/src/body_input_runtime.go"
        source.write_text(
            source.read_text(encoding="utf-8").replace(
                "case indexFormatV2:\n",
                "case indexFormatV2:\n\t\tverify = append(verify, mf.Store.Records, mf.Store.TagLists)\n",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "full records/taglists scan"):
            self.run_audit(repo)


    def test_requires_resource_budget_before_primitive_execution(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "checkResourceBudgetBeforePrimitive(m.Budget, budgetStart, opsExecuted, f, op.Code)",
                "checkResourceBudgetAfterPrimitive(m.Budget, budgetStart, opsExecuted, f, op.Code)",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "resource budget"):
            self.run_audit(repo)

    def test_requires_daemon_transport_concurrency_boundary(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        daemon = repo / "Kernel/src/daemon_runtime.go"
        daemon.write_text(
            daemon.read_text(encoding="utf-8").replace(
                "daemon physical concurrency limit reached",
                "daemon concurrency unchecked",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "daemon transport boundary"):
            self.run_audit(repo)

    def test_requires_mesh_http_server_timeouts(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        mesh = repo / "Kernel/src/mesh_runtime.go"
        mesh.write_text(
            mesh.read_text(encoding="utf-8").replace(
                "ReadHeaderTimeout: meshHTTPReadHeaderTimeout",
                "ReadHeaderTimeout: 0",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "Mesh HTTP transport boundary"):
            self.run_audit(repo)

    def test_rejects_goroutine_per_event_handler_fanout(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        event = repo / "Kernel/src/event_runtime.go"
        event.write_text(
            event.read_text(encoding="utf-8").replace(
                "parallelCPUFor(batchLen, workers",
                "parallelEventFanoutUnbounded(batchLen, workers",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded event fanout"):
            self.run_audit(repo)

    def test_requires_call_parallel_fanout_ceiling(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "call_parallel fan-out exceeds physical target ceiling",
                "call_parallel fan-out unchecked",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded call_parallel fanout"):
            self.run_audit(repo)

    def test_requires_storage_handler_concurrency_boundary(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "make(chan struct{}, storageMaxConcurrent())",
                "make(chan struct{}, 1)",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "remote storage transport boundary"):
            self.run_audit(repo)

    def test_requires_storage_connection_timeout_boundary(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "SetDeadline(time.Now().Add(storageConnectionTimeout()))",
                "SetDeadline(time.Now().Add(30 * time.Second))",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "remote storage transport boundary"):
            self.run_audit(repo)

    def test_requires_storage_oversize_response_fallback(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "writeStorageReplyBounded(c",
                "writeStorageEnvelope(c",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "remote storage transport boundary"):
            self.run_audit(repo)

    def test_requires_raw_exchange_timeout_clamp_at_io_boundary(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "timeout = physicalExchangeTimeout(timeout)",
                "timeout = timeout",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "external I/O timeout boundary"):
            self.run_audit(repo)

    def test_requires_remote_storage_timeout_clamp_at_io_boundary(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "timeout = storageRequestTimeout(timeout)",
                "timeout = timeout",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "external I/O timeout boundary"):
            self.run_audit(repo)

    def test_requires_source_adapter_timeout_hard_cap(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        source = repo / "Kernel/src/source_adapter_runtime.go"
        source.write_text(
            source.read_text(encoding="utf-8").replace(
                "clampPhysicalTimeout(d, defaultSourceAdapterTimeout, hardSourceAdapterTimeout)",
                "d",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "external I/O timeout boundary"):
            self.run_audit(repo)

    def test_requires_frame_list_cardinality_gate(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "FindAllStringSubmatch(f.Vars[op.A], maxItems+1)",
                "FindAllStringSubmatch(f.Vars[op.A], -1)",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded Frame-list cardinality"):
            self.run_audit(repo)

    def test_requires_frame_scalar_join_ceiling(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'ensureFrameJoinBytes("str_join", left, sep, right)',
                'nil',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded Frame scalar/output growth"):
            self.run_audit(repo)

    def test_requires_frame_output_append_ceiling(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                "ensureFrameOutputAppend(f.Output, value)",
                "nil",
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded Frame scalar/output growth"):
            self.run_audit(repo)


    def test_requires_program_splice_cardinality_gate(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'ensureProgramOpCount(newLen, "program_insert_from")',
                'nil',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded executable Program growth"):
            self.run_audit(repo)

    def test_requires_program_import_predecode_byte_gate(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'int64(len(rawProgram)) > programMaxBytes()',
                'false',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded executable Program growth"):
            self.run_audit(repo)



    def test_requires_bounded_template_expansion(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                's, err = replaceAllFrameValueBounded(s, "{{"+k+"}}", v, maxBytes)',
                's = strings.ReplaceAll(s, "{{"+k+"}}", v)',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded template expansion"):
            self.run_audit(repo)



    def test_requires_bounded_memory_state_record_growth(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'candidateState, err := memoryStateCandidateWithinPhysicalLimit(target, key, value, "state_set")',
                'candidateState := map[string]any{key: value}; var err error',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded Memory State record growth"):
            self.run_audit(repo)



    def test_requires_bounded_memory_metadata_record_growth(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        kernel = repo / "Kernel/src/kernel.go"
        kernel.write_text(
            kernel.read_text(encoding="utf-8").replace(
                'if err := ensureMemoryRecordWithinPhysicalLimit(&candidate, "memory_tag_add"); err != nil {',
                'if err := error(nil); err != nil {',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded Memory metadata record growth"):
            self.run_audit(repo)



    def test_requires_bounded_memory_creation_import_record_growth(self):
        tmp, repo = self.with_repo()
        self.addCleanup(tmp.cleanup)
        structure = repo / "Kernel/src/structure_runtime.go"
        structure.write_text(
            structure.read_text(encoding="utf-8").replace(
                'if err := ensureMemoryRecordRawBytesWithinPhysicalLimit(raw, "memory_import_json"); err != nil {',
                'if err := error(nil); err != nil {',
                1,
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(RuntimeError, "bounded Memory creation/import record growth"):
            self.run_audit(repo)



if __name__ == "__main__":
    unittest.main()
