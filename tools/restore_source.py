#!/usr/bin/env python3
from __future__ import annotations

import argparse
import base64
import gzip
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_MANIFESTS = (
    ROOT / "bootstrap/v27/Kernel/src/kernel.go.bootstrap.json",
    ROOT / "bootstrap/v27/Kernel/v27-required-structures.bootstrap.json",
)


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


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
    if expected_gzip and sha256(packed) != expected_gzip:
        raise RuntimeError(
            f"gzip SHA-256 mismatch for {manifest_path}: "
            f"expected {expected_gzip}, got {sha256(packed)}"
        )

    raw = gzip.decompress(packed)
    expected = str(meta.get("original_sha256") or "").strip()
    actual = sha256(raw)
    if expected and actual != expected:
        raise RuntimeError(
            f"source SHA-256 mismatch for {manifest_path}: expected {expected}, got {actual}"
        )

    rel_target = Path(str(meta["path"]))
    target = ROOT / rel_target
    if not check_only:
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(raw)

    return {
        "manifest": str(manifest_path.relative_to(ROOT)),
        "target": str(rel_target),
        "sha256": actual,
        "bytes": len(raw),
        "status": "verified" if check_only else "restored",
    }


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Restore and verify source files stored as deterministic bootstrap chunks."
    )
    parser.add_argument(
        "manifests",
        nargs="*",
        help="Bootstrap manifest paths relative to repository root. Defaults to all v27 source manifests.",
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="Verify bootstrap chunks and hashes without writing restored files.",
    )
    args = parser.parse_args()

    manifests = [ROOT / p for p in args.manifests] if args.manifests else list(DEFAULT_MANIFESTS)
    results = [restore_manifest(p, check_only=args.check) for p in manifests]
    print(json.dumps({"ok": True, "results": results}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
