#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
SRC = ROOT / "Kernel" / "src"
DEFAULT_OUT = ROOT / "dist" / "MemoryAI"


def run(cmd: list[str], *, cwd: Path = ROOT, env: dict[str, str] | None = None, timeout: int = 300, capture: bool = False) -> subprocess.CompletedProcess[str]:
    print("+", " ".join(str(x) for x in cmd), flush=True)
    merged = os.environ.copy()
    if env:
        merged.update(env)
    return subprocess.run(
        [str(x) for x in cmd], cwd=cwd, env=merged, check=True,
        text=True, stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.STDOUT if capture else None, timeout=timeout,
    )


def sha(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def parse_kernel_constants(kernel_go: Path) -> tuple[str, str]:
    import re
    text = kernel_go.read_text(encoding="utf-8")
    vm = re.search(r'const\s+imageVersion\s*=\s*"([^"]+)"', text)
    am = re.search(r'const\s+memoryABI\s*=\s*"([^"]+)"', text)
    if not vm or not am:
        raise RuntimeError("restored kernel.go does not expose imageVersion/memoryABI")
    return vm.group(1), am.group(1)


def go_env(*, race: bool = False) -> dict[str, str]:
    env = {"GO111MODULE": "off"}
    if race:
        env["CGO_ENABLED"] = "1"
    return env


def full_gate() -> None:
    run([sys.executable, "tools/restore_source.py", "--check"])
    run([sys.executable, "tools/architecture_audit.py"])
    go_files = sorted(SRC.glob("*.go"))
    if not go_files:
        raise RuntimeError("Kernel/src has no Go source")
    diff = run(["gofmt", "-d", *[p.name for p in go_files]], cwd=SRC, capture=True)
    if diff.stdout.strip():
        raise RuntimeError("gofmt drift in Kernel/src:\n" + diff.stdout)
    run(["go", "vet", "."], cwd=SRC, env=go_env())
    run(["go", "test", "."], cwd=SRC, env=go_env(), timeout=600)
    run(["go", "test", "-race", "."], cwd=SRC, env=go_env(race=True), timeout=900)
    run([sys.executable, "tools/architecture_audit.py"])


def build_kernel(out: Path, allow_cpu_only_cross: bool) -> str:
    out.parent.mkdir(parents=True, exist_ok=True)
    host_linux_amd64 = platform.system().lower() == "linux" and platform.machine().lower() in {"x86_64", "amd64"}
    env = {"GO111MODULE": "off", "GOOS": "linux", "GOARCH": "amd64"}
    gpu_mode = "opencl-dynamic"
    if host_linux_amd64:
        env["CGO_ENABLED"] = "1"
    elif allow_cpu_only_cross:
        env["CGO_ENABLED"] = "0"
        gpu_mode = "cpu-only-cross-build"
    else:
        raise RuntimeError("Linux AMD64 OpenCL release requires a Linux AMD64+cgo build host; use --allow-cpu-only-cross only for explicit CPU-only cross builds")
    run(["go", "build", "-trimpath", "-ldflags=-buildid=", "-o", str(out), "."], cwd=SRC, env=env, timeout=600)
    return gpu_mode


def build_memory(out: Path, version: str, abi: str) -> None:
    run([sys.executable, "tools/build_current_memory_seed.py", "--output", "Kernel/current-required-structures.json"])
    run([
        sys.executable, "tools/build_memory_body.py",
        "--seed", "Kernel/current-required-structures.json",
        "--out", str(out), "--version", version, "--abi", abi,
    ])


def try_cmd(cmd: list[str], env: dict[str, str] | None = None, timeout: int = 20) -> tuple[bool, str]:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    try:
        p = subprocess.run(cmd, cwd=ROOT, env=merged, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=timeout)
        return p.returncode == 0, p.stdout
    except Exception as exc:
        return False, repr(exc)


def detect_cli(kernel: Path, memory: Path) -> str:
    candidates = [
        ("body-first", [str(kernel), str(memory), "version"], {}),
        ("env-body", [str(kernel), "version"], {"MEMORYAI_BODY": str(memory)}),
    ]
    results: list[str] = []
    for mode, cmd, env in candidates:
        ok, out = try_cmd(cmd, env)
        results.append(f"{mode}: {out.strip()}")
        if ok:
            return mode
    raise RuntimeError("unable to detect Kernel CLI body binding:\n" + "\n".join(results))


def kernel_cmd(mode: str, kernel: Path, memory: Path, args: list[str]) -> tuple[list[str], dict[str, str]]:
    if mode == "body-first":
        return [str(kernel), str(memory), *args], {}
    if mode == "env-body":
        return [str(kernel), *args], {"MEMORYAI_BODY": str(memory)}
    raise ValueError(mode)


def daemon_smoke(mode: str, kernel: Path, memory: Path) -> None:
    with tempfile.TemporaryDirectory(prefix="memoryai-release-smoke-") as td:
        sock = Path(td) / "memoryai.sock"
        dcmd, denv = kernel_cmd(mode, kernel, memory, ["daemon", str(sock)])
        env = os.environ.copy(); env.update(denv)
        proc = subprocess.Popen(dcmd, cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        try:
            last = ""
            for _ in range(80):
                ccmd, cenv = kernel_cmd(mode, kernel, memory, ["client", str(sock), "health"])
                ok, last = try_cmd(ccmd, cenv, timeout=5)
                if ok:
                    # Persist path must also be wired before release.
                    pcmd, penv = kernel_cmd(mode, kernel, memory, ["client", str(sock), "persist"])
                    pok, pout = try_cmd(pcmd, penv, timeout=10)
                    if not pok:
                        raise RuntimeError("daemon persist smoke failed: " + pout)
                    return
                if proc.poll() is not None:
                    break
                time.sleep(0.1)
            raise RuntimeError("daemon health smoke failed: " + last)
        finally:
            if proc.poll() is None:
                proc.send_signal(signal.SIGTERM)
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.kill(); proc.wait(timeout=5)


def script_for(mode: str, start: bool) -> str:
    if start:
        invocation = (
            '"$KERNEL" "$MEMORY" daemon "$SOCKET" >>"$LOG" 2>&1 &' if mode == "body-first"
            else 'MEMORYAI_BODY="$MEMORY" "$KERNEL" daemon "$SOCKET" >>"$LOG" 2>&1 &'
        )
        health = (
            '"$KERNEL" "$MEMORY" client "$SOCKET" health >/dev/null 2>&1' if mode == "body-first"
            else 'MEMORYAI_BODY="$MEMORY" "$KERNEL" client "$SOCKET" health >/dev/null 2>&1'
        )
        return f'''#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${{BASH_SOURCE[0]}}")" && pwd)"
KERNEL="$ROOT/Kernel/Linux/amd64/Kernel"
MEMORY="$ROOT/data/Memory.mem"
RUN="$ROOT/var/run"; LOGDIR="$ROOT/var/log"
SOCKET="$RUN/memoryai.sock"; PIDFILE="$RUN/memoryai.pid"; LOG="$LOGDIR/kernel.log"
mkdir -p "$RUN" "$LOGDIR"
if [[ -f "$PIDFILE" ]] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  echo "MemoryAI already running pid=$(cat "$PIDFILE")"; exit 0
fi
rm -f "$SOCKET" "$PIDFILE"
{invocation}
PID=$!; echo "$PID" > "$PIDFILE"
for _ in $(seq 1 100); do
  if {health}; then echo "MemoryAI ready pid=$PID socket=$SOCKET"; exit 0; fi
  if ! kill -0 "$PID" 2>/dev/null; then tail -n 80 "$LOG" || true; rm -f "$PIDFILE"; exit 1; fi
  sleep 0.1
done
kill "$PID" 2>/dev/null || true; rm -f "$PIDFILE" "$SOCKET"
echo "MemoryAI daemon readiness timeout" >&2; exit 1
'''
    persist = (
        '"$KERNEL" "$MEMORY" client "$SOCKET" persist >/dev/null 2>&1 || true' if mode == "body-first"
        else 'MEMORYAI_BODY="$MEMORY" "$KERNEL" client "$SOCKET" persist >/dev/null 2>&1 || true'
    )
    return f'''#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${{BASH_SOURCE[0]}}")" && pwd)"
KERNEL="$ROOT/Kernel/Linux/amd64/Kernel"; MEMORY="$ROOT/data/Memory.mem"
SOCKET="$ROOT/var/run/memoryai.sock"; PIDFILE="$ROOT/var/run/memoryai.pid"
if [[ ! -f "$PIDFILE" ]]; then rm -f "$SOCKET"; echo "MemoryAI not running"; exit 0; fi
PID="$(cat "$PIDFILE")"
if kill -0 "$PID" 2>/dev/null; then
  {persist}
  kill -TERM "$PID" 2>/dev/null || true
  for _ in $(seq 1 100); do kill -0 "$PID" 2>/dev/null || break; sleep 0.1; done
  kill -0 "$PID" 2>/dev/null && kill -KILL "$PID" 2>/dev/null || true
fi
rm -f "$PIDFILE" "$SOCKET"
echo "MemoryAI stopped"
'''


def write_release(out: Path, kernel: Path, memory: Path, mode: str, version: str, abi: str, gpu_mode: str) -> dict[str, object]:
    if out.exists():
        shutil.rmtree(out)
    (out / "Kernel/Linux/amd64").mkdir(parents=True)
    (out / "Kernel/src").mkdir(parents=True)
    (out / "data").mkdir(parents=True)
    (out / "var/run").mkdir(parents=True)
    (out / "var/log").mkdir(parents=True)
    shutil.copy2(kernel, out / "Kernel/Linux/amd64/Kernel")
    shutil.copy2(memory, out / "data/Memory.mem")
    for p in SRC.iterdir():
        if p.is_file() and p.suffix in {".go", ".c", ".h"}:
            shutil.copy2(p, out / "Kernel/src" / p.name)
    for name, content in (("start.sh", script_for(mode, True)), ("stop.sh", script_for(mode, False))):
        path = out / name; path.write_text(content, encoding="utf-8"); path.chmod(0o755)
    (out / "VERSION").write_text(version + "\n", encoding="utf-8")

    files = [p for p in out.rglob("*") if p.is_file()]
    checksum_rows = []
    for p in sorted(files):
        if p.name in {"CHECKSUMS.sha256", "RELEASE-MANIFEST.json"}:
            continue
        checksum_rows.append(f"{sha(p)}  {p.relative_to(out).as_posix()}")
    (out / "CHECKSUMS.sha256").write_text("\n".join(checksum_rows) + "\n", encoding="utf-8")
    manifest = {
        "format": "memoryai-release-v1",
        "kernel_version": version,
        "memory_abi": abi,
        "platform": "linux-amd64",
        "gpu_mode": gpu_mode,
        "cli_mode": mode,
        "kernel_sha256": sha(out / "Kernel/Linux/amd64/Kernel"),
        "memory_sha256": sha(out / "data/Memory.mem"),
        "runtime_ai_persistence": ["Kernel/Linux/amd64/Kernel", "data/Memory.mem"],
        "sidecar_persistence": False,
    }
    (out / "RELEASE-MANIFEST.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return manifest


def main() -> int:
    ap = argparse.ArgumentParser(description="Build the validated Linux AMD64 MemoryAI release from current source + current Memory seed")
    ap.add_argument("--out", type=Path, default=DEFAULT_OUT)
    ap.add_argument("--allow-cpu-only-cross", action="store_true")
    args = ap.parse_args()

    run([sys.executable, "tools/restore_source.py"])
    full_gate()
    version, abi = parse_kernel_constants(SRC / "kernel.go")

    with tempfile.TemporaryDirectory(prefix="memoryai-release-build-") as td:
        td = Path(td)
        kernel = td / "Kernel"
        memory = td / "Memory.mem"
        gpu_mode = build_kernel(kernel, args.allow_cpu_only_cross)
        build_memory(memory, version, abi)
        # Verify the built body with the actual Kernel before packaging.
        mode = detect_cli(kernel, memory)
        fcmd, fenv = kernel_cmd(mode, kernel, memory, ["fsck"])
        ok, output = try_cmd(fcmd, fenv, timeout=120)
        if not ok:
            raise RuntimeError("Kernel FSCK rejected freshly built Memory.mem:\n" + output)
        daemon_smoke(mode, kernel, memory)
        manifest = write_release(args.out.resolve(), kernel, memory, mode, version, abi, gpu_mode)
    print(json.dumps({"ok": True, "out": str(args.out.resolve()), **manifest}, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
