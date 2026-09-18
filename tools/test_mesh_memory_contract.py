#!/usr/bin/env python3
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SEED = ROOT / "Kernel/current-required-structures.json"

class MeshMemoryContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        data = json.loads(SEED.read_text(encoding="utf-8"))
        cls.mem = {m["id"]: m for m in data["memories"]}

    def test_global_sync_direct_fetches_without_import(self):
        sync = json.dumps(self.mem["mesh.global.sync.parent"], ensure_ascii=False)
        integrate = json.dumps(self.mem["mesh.global.integrate.parent"], ensure_ascii=False)
        self.assertIn("mesh_shared_search", sync)
        self.assertIn("mesh_shared_fetch", sync)
        self.assertIn("mesh.memory.integrated", sync)
        self.assertNotIn("memory_import_json", sync)
        self.assertNotIn("memory_import_json", integrate)
        self.assertNotIn("memory_tag_add", integrate)
        self.assertIn("__event.remote_memory_json", integrate)

    def test_current_mesh_execution_primitives_are_memory_owned(self):
        all_programs = json.dumps([m.get("program", []) for m in self.mem.values()], ensure_ascii=False)
        self.assertIn("mesh_route_execution", all_programs)
        self.assertIn("mesh_shared_fetch", all_programs)
        self.assertIn("mesh_structure_run", all_programs)
        self.assertIn("mesh.remote.execute.parent", self.mem)

    def test_conflict_status_enters_memory_frontier(self):
        rec = json.dumps(self.mem["mesh.conflict.reconcile.parent"], ensure_ascii=False)
        self.assertIn("mesh_shared_reconcile", rec)
        self.assertIn("mesh.version.conflict", rec)
        self.assertIn("cog.mesh.version.conflict", rec)

if __name__ == "__main__":
    unittest.main()
