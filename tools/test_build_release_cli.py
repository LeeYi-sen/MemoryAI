#!/usr/bin/env python3
from pathlib import Path
import unittest

from build_release import kernel_cmd, script_for


class ReleaseCLIContractTest(unittest.TestCase):
    def test_daemon_client_is_native_kernel_control_path(self) -> None:
        kernel = Path("/tmp/Kernel")
        memory = Path("/tmp/Memory.mem")
        cmd, env = kernel_cmd(
            "body-first", kernel, memory,
            ["client", "/tmp/memoryai.sock", "health"],
        )
        self.assertEqual(
            cmd,
            [str(kernel), "client", "/tmp/memoryai.sock", "health"],
        )
        self.assertEqual(env, {})

    def test_release_scripts_never_put_memory_before_client(self) -> None:
        for start in (True, False):
            script = script_for("body-first", start)
            self.assertIn('"$KERNEL" client "$SOCKET"', script)
            self.assertNotIn('"$KERNEL" "$MEMORY" client', script)


if __name__ == "__main__":
    unittest.main()
