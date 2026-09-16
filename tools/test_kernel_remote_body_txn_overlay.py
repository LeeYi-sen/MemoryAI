#!/usr/bin/env python3
from __future__ import annotations

import base64
import gzip
import json
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TOOLS = ROOT / "tools"
sys.path.insert(0, str(TOOLS))

from kernel_fabric_execution_overlay import apply_kernel_fabric_execution_overlay
from kernel_lazy_overlay import apply_kernel_lazy_overlay
from kernel_mesh_journal_overlay import apply_kernel_mesh_journal_overlay
from kernel_mesh_overlay import apply_kernel_mesh_overlay
from kernel_overlay import apply_kernel_overlay
from kernel_owner_lock_overlay import apply_kernel_owner_lock_overlay
from kernel_parallel_boundary_overlay import apply_kernel_parallel_boundary_overlay
from kernel_persistence_overlay import apply_kernel_persistence_overlay
from kernel_remote_body_txn_overlay import apply_kernel_remote_body_txn_overlay
from kernel_remote_memory_overlay import apply_kernel_remote_memory_overlay
from kernel_scale_overlay import apply_kernel_scale_overlay
from kernel_shard_overlay import apply_kernel_shard_overlay
from kernel_speculative_overlay import apply_kernel_speculative_overlay
from kernel_store_access_overlay import apply_kernel_store_access_overlay

PRE_REMOTE_BODY_OVERLAYS = (
    apply_kernel_overlay,
    apply_kernel_mesh_overlay,
    apply_kernel_speculative_overlay,
    apply_kernel_mesh_journal_overlay,
    apply_kernel_parallel_boundary_overlay,
    apply_kernel_lazy_overlay,
    apply_kernel_scale_overlay,
    apply_kernel_remote_memory_overlay,
    apply_kernel_persistence_overlay,
    apply_kernel_shard_overlay,
    apply_kernel_store_access_overlay,
    apply_kernel_fabric_execution_overlay,
    apply_kernel_owner_lock_overlay,
)


def bootstrap_kernel() -> bytes:
    manifest_path = ROOT / "bootstrap/v27/Kernel/src/kernel.go.bootstrap.json"
    meta = json.loads(manifest_path.read_text(encoding="utf-8"))
    encoded = "".join(
        (manifest_path.parent / str(name)).read_text(encoding="utf-8").strip()
        for name in meta["parts"]
    )
    return gzip.decompress(base64.b64decode(encoded, validate=True))


class RemoteBodyTxnOverlayTest(unittest.TestCase):
    def test_real_restore_chain_isolates_unmount_by_function_boundary(self) -> None:
        current = bootstrap_kernel()
        for overlay in PRE_REMOTE_BODY_OVERLAYS:
            current = overlay(current)

        patched = apply_kernel_remote_body_txn_overlay(current).decode("utf-8")
        lease = "releaseUnmountBodyTxn := acquireRemoteBodyTransaction(cp)"
        self.assertEqual(patched.count(lease), 1)

        unmount_start = patched.index("func (e *Engine) unmountSpace(")
        next_func = patched.index("\nfunc ", unmount_start + 1)
        self.assertIn(lease, patched[unmount_start:next_func])

    def test_remote_durability_scopes_unsafe_persistence_check_to_remote_handler(self) -> None:
        from kernel_remote_durability_overlay import apply_kernel_remote_durability_overlay

        current = bootstrap_kernel()
        for overlay in PRE_REMOTE_BODY_OVERLAYS:
            current = overlay(current)
        current = apply_kernel_remote_body_txn_overlay(current)

        patched = apply_kernel_remote_durability_overlay(current).decode("utf-8")
        self.assertNotIn("if er = sp.saveBody(p); er != nil {", patched)
        self.assertEqual(patched.count("if er = persistEngineIncremental(sp); er != nil {"), 2)
        self.assertGreaterEqual(patched.count("persistEngineIfDirty(sp)"), 2)

    def test_mutation_journal_allows_only_local_unmount_full_persistence(self) -> None:
        from kernel_mutation_journal_overlay import apply_kernel_mutation_journal_overlay
        from kernel_remote_durability_overlay import apply_kernel_remote_durability_overlay

        current = bootstrap_kernel()
        for overlay in PRE_REMOTE_BODY_OVERLAYS:
            current = overlay(current)
        current = apply_kernel_remote_body_txn_overlay(current)
        current = apply_kernel_remote_durability_overlay(current)

        patched = apply_kernel_mutation_journal_overlay(current).decode("utf-8")
        full = "persistEngineIfDirty(sp)"
        self.assertEqual(patched.count(full), 1)
        start = patched.index("func (e *Engine) unmountSpace(")
        end = patched.index("\nfunc ", start + 1)
        self.assertIn(full, patched[start:end])


if __name__ == "__main__":
    unittest.main()
