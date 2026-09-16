#!/usr/bin/env python3
from __future__ import annotations

import argparse
import base64
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_MANIFEST = ROOT / "bootstrap/v27/Kernel/v27-required-structures.bootstrap.json"
DEFAULT_OUTPUT = ROOT / "Kernel/current-required-structures.json"

STAGES = (
    ("v28_metrics_to_state.py", "4e8c3e069cfa7ebc3802fba53e330bd96155df322462b648e168ccefd819c851"),
    ("v28_evolution_dispatch.py", "43811560f77e2b94fa6627e32972fc71af10a0452074c77277cc90a953cb237b"),
    ("v28_evolution_mutation.py", "68883bb5303fd3d925e8c99980ed2fc9db9d470f225de0ce694f8684fc33d277"),
    ("v28_evolution_ab.py", "3689e0d596b75eeb58a9d018fe24c75f492928b8a62cad68418013bb92c9642c"),
    ("v28_frontier_drive.py", "c79cfe749fa4f274a61a9cea8dc9e8fe7f3bde10f44a4137df579ff1e66b2f5f"),
    ("v28_belief_reliability_deadline.py", "7d4e512d624c4b13ad619fc051e62342c1feb5e291c98f21f1ad35037b13087c"),
    ("v28_context_research_sources.py", "4de691df29abafd625e193e259caea1583b0cdcf60bab5c3d328beca779212d9"),
    ("v28_language_semantics.py", "eb82f503266a12c617e46e78eae882502daed54a8e90859760caf6be7a58ae73"),
    ("v29_runtime_ownership.py", None),
)


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def decode_verified_bootstrap(manifest_path: Path) -> bytes:
    manifest_path = manifest_path.resolve()
    meta = json.loads(manifest_path.read_text(encoding="utf-8"))
    if meta.get("encoding") != "gzip+base64-chunks":
        raise RuntimeError(f"unsupported bootstrap encoding: {meta.get('encoding')!r}")
    encoded = "".join(
        (manifest_path.parent / str(name)).read_text(encoding="utf-8").strip()
        for name in meta.get("parts", [])
    )
    if not encoded:
        raise RuntimeError("required-structures bootstrap has no parts")
    packed = base64.b64decode(encoded, validate=True)
    expected_gzip = str(meta.get("gzip_sha256") or "").strip()
    if expected_gzip and sha256(packed) != expected_gzip:
        raise RuntimeError("required-structures bootstrap gzip hash mismatch")
    raw = gzip.decompress(packed)
    expected_raw = str(meta.get("original_sha256") or "").strip()
    if expected_raw and sha256(raw) != expected_raw:
        raise RuntimeError("required-structures bootstrap source hash mismatch")
    return raw


def run_stage(script: Path, input_path: Path, output_path: Path) -> None:
    proc = subprocess.run(
        [sys.executable, str(script), str(input_path), str(output_path)],
        cwd=ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"Memory seed migration {script.name} failed:\n{proc.stdout}")


def build_current_memory_seed(manifest_path: Path, output_path: Path) -> dict[str, object]:
    baseline = decode_verified_bootstrap(manifest_path)
    migration_dir = ROOT / "tools/migrations"
    stages_report: list[dict[str, object]] = []
    with tempfile.TemporaryDirectory(prefix="memoryai-current-seed-") as tmp_name:
        tmp = Path(tmp_name)
        current = tmp / "stage-00.json"
        current.write_bytes(baseline)
        for index, (script_name, expected_sha) in enumerate(STAGES, start=1):
            script = migration_dir / script_name
            if not script.is_file():
                raise FileNotFoundError(f"Memory seed migration missing: {script}")
            nxt = tmp / f"stage-{index:02d}.json"
            run_stage(script, current, nxt)
            digest = sha256(nxt.read_bytes())
            if expected_sha and digest != expected_sha:
                raise RuntimeError(
                    f"Memory seed stage {script_name} hash mismatch: expected {expected_sha}, got {digest}"
                )
            stages_report.append({"stage": script_name, "sha256": digest})
            current = nxt

        final_bytes = current.read_bytes()
        output_path = output_path.resolve()
        output_path.parent.mkdir(parents=True, exist_ok=True)
        tmp_out = output_path.with_name(output_path.name + ".tmp")
        tmp_out.write_bytes(final_bytes)
        os.replace(tmp_out, output_path)

    doc = json.loads(final_bytes)
    return {
        "baseline_sha256": sha256(baseline),
        "output": str(output_path.relative_to(ROOT)) if output_path.is_relative_to(ROOT) else str(output_path),
        "output_sha256": sha256(final_bytes),
        "version": doc.get("version"),
        "memory_count": len(doc.get("memories", [])),
        "stages": stages_report,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Build the single current Memory cognition seed from immutable v27 provenance.")
    parser.add_argument("--manifest", type=Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    args = parser.parse_args()
    report = build_current_memory_seed(args.manifest, args.output)
    print(json.dumps({"ok": True, **report}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
