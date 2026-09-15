#!/usr/bin/env python3
from __future__ import annotations

import argparse
import base64
import gzip
import hashlib
import json
from pathlib import Path

from kernel_mesh_overlay import apply_kernel_mesh_overlay
from kernel_overlay import apply_kernel_overlay
from kernel_speculative_overlay import apply_kernel_speculative_overlay

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_MANIFESTS = (
    ROOT / "bootstrap/v27/Kernel/src/kernel.go.bootstrap.json",
    ROOT / "bootstrap/v27/Kernel/v27-required-structures.bootstrap.json",
)


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def current_source_bytes(rel_target: Path, raw: bytes) -> bytes:
    # Historical bootstrap bytes remain immutable and hash-verifiable. Forward
    # source fixes are applied only after that verification as explicit,
    # deterministic overlays with their own input hashes.
    if rel_target.as_posix() == "Kernel/src/kernel.go":
        current = apply_kernel_overlay(raw)
        current = apply_kernel_mesh_overlay(current)
        current = apply_kernel_speculative_overlay(current)
        return current
    return raw


def restore_manifest(manifest_path: Path, *, check_only: bool = False) -> dict[str, object]:
    manifest_path = manifest_path.resolve()
    meta = json.loads(manifest_path.read_text(encoding="utf-8"))
    if meta.get("encoding") != "gzip+base64-chunks":
        raise RuntimeError(f"unsupported bootstrap encoding: {meta.get('encoding')!r}")

    encoded_parts: list[str] = []
    for name in meta.get("parts", []):
        part_path = manifest_path.parent / str(name)
        if not part_path.is_file():
            raise FileNotFoundError(f"bootstrap part missing: {part_path}")
        encoded_parts.append(part_path.read_text(encoding="utf-8").strip())

    if not encoded_parts:
        raise RuntimeError(f"bootstrap manifest has no parts: {manifest_path}")

    packed = base64.b64decode("".join(encoded_parts), validate=True)
    expected_gzip = str(meta.get("gzip_sha256") or "").strip()
    packed_sha = sha256(packed)
    if expected_gzip and packed_sha != expected_gzip:
        raise RuntimeError(
            f"gzip SHA-256 mismatch for {manifest_path}: "
            f"expected {expected_gzip}, got {packed_sha}"
        )

    raw = gzip.decompress(packed)
    expected = str(meta.get("original_sha256") or "").strip()
    bootstrap_sha = sha256(raw)
    if expected and bootstrap_sha != expected:
        raise RuntimeError(
            f"source SHA-256 mismatch for {manifest_path}: expected {expected}, got {bootstrap_sha}"
        )

    rel_target = Path(str(meta["path"]))
    current = current_source_bytes(rel_target, raw)
    current_sha = sha256(current)
    target = ROOT / rel_target
    if not check_only:
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(current)

    return {
        "manifest": str(manifest_path.relative_to(ROOT)),
        "target": str(rel_target),
        "bootstrap_sha256": bootstrap_sha,
        "current_sha256": current_sha,
        "bytes": len(current),
        "overlay_applied": current != raw,
        "status": "verified" if check_only else "restored",
    }


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Restore, verify and forward-patch source files stored as deterministic bootstrap chunks."
    )
    parser.add_argument(
        "manifests",
        nargs="*",
        help="Bootstrap manifest paths relative to repository root. Defaults to all v27 source manifests.",
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="Verify bootstrap chunks and current overlays without writing restored files.",
    )
    args = parser.parse_args()

    manifests = [ROOT / p for p in args.manifests] if args.manifests else list(DEFAULT_MANIFESTS)
    results = [restore_manifest(p, check_only=args.check) for p in manifests]
    print(json.dumps({"ok": True, "results": results}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
