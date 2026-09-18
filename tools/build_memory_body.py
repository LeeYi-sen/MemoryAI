#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import struct
import zipfile
from pathlib import Path
from typing import Any

FORMAT = "memoryai-body-v2"
GENESIS_FORMAT = "memoryai-genesis-index-v1"
DEFAULT_ABI = "memoryai-memory-abi-v1"
DEFAULT_VERSION = "28.9.0-memory-fabric-sovereign"
PHYSICAL_PREFIX = "\x1fmemoryai.phys.v1:"
INDEX_FORMAT = "memoryai-index-v2-sha256"
JOURNAL_COMMENT_SIZE = 65535
EPOCH = (1980, 1, 1, 0, 0, 0)


def compact_json(value: Any) -> bytes:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), sort_keys=False).encode("utf-8")


def pretty_json(value: Any) -> bytes:
    return (json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def fnv1a64(text: str) -> int:
    h = 14695981039346656037
    for b in text.encode("utf-8"):
        h ^= b
        h = (h * 1099511628211) & 0xFFFFFFFFFFFFFFFF
    return h


def scalar_string(value: Any) -> str | None:
    if isinstance(value, str):
        return value
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        return format(value, ".15g")
    return None


def features(memory: dict[str, Any]) -> list[str]:
    seen: set[str] = set()
    mid = str(memory.get("id", "")).strip()
    if mid:
        seen.add("id:" + mid)
    for tag in memory.get("tags") or []:
        tag = str(tag).strip()
        if tag:
            seen.add("tag:" + tag)
    for trigger in memory.get("trigger") or []:
        trigger = str(trigger).strip()
        if trigger:
            seen.add("trigger:" + trigger)
    state = memory.get("state") or {}
    if isinstance(state, dict):
        for key in sorted(state):
            k = str(key)
            seen.add("state-key:" + k)
            s = scalar_string(state[key])
            if s is not None:
                seen.add("state-kv:" + k + "=" + s)
    return sorted(seen)


def normalize_seed(raw: Any) -> tuple[str, list[dict[str, Any]]]:
    root = "root.memory"
    if isinstance(raw, list):
        memories = raw
    elif isinstance(raw, dict):
        root = str(raw.get("root") or root)
        for key in ("memories", "structures", "required_structures", "memory"):
            if isinstance(raw.get(key), list):
                memories = raw[key]
                break
        else:
            # Migration files historically emit {id: Memory} maps in some stages.
            if all(isinstance(v, dict) and "id" in v for v in raw.values()):
                memories = list(raw.values())
            else:
                raise ValueError("seed must be a Memory list or object containing memories/structures")
    else:
        raise ValueError("seed JSON must be object or list")

    out: list[dict[str, Any]] = []
    ids: set[str] = set()
    for item in memories:
        if not isinstance(item, dict):
            raise ValueError("every Memory record must be an object")
        m = dict(item)
        mid = str(m.get("id", "")).strip()
        if not mid:
            raise ValueError("Memory record missing id")
        if mid in ids:
            raise ValueError(f"duplicate Memory id: {mid}")
        ids.add(mid)
        m["id"] = mid
        if not m.get("layer"):
            m["layer"] = "inherited"
        if m.get("state") is None:
            m["state"] = {}
        out.append(m)
    out.sort(key=lambda m: m["id"])
    if root not in ids:
        raise ValueError(f"root Memory {root!r} missing from seed")
    return root, out


def encode_index(rows: list[tuple[int, int, int, int, bytes, str]]) -> bytes:
    rows.sort(key=lambda r: (r[0], r[5]))
    out = bytearray()
    for h, off, length, count, digest, _name in rows:
        if len(digest) != hashlib.sha256().digest_size:
            raise ValueError("index digest must be SHA-256")
        out += struct.pack("<QQII32s", h, off, length, count, digest)
    return bytes(out)


def build_sections(memories: list[dict[str, Any]]) -> tuple[bytes, bytes, bytes, bytes]:
    records = bytearray()
    id_rows: list[tuple[int, int, int, int, bytes, str]] = []
    postings: dict[str, set[str]] = {}

    for memory in sorted(memories, key=lambda m: m["id"]):
        blob = compact_json(memory)
        off = len(records)
        records += blob
        mid = memory["id"]
        id_rows.append((fnv1a64(mid), off, len(blob), 0, hashlib.sha256(blob).digest(), mid))
        keys = {str(t) for t in (memory.get("tags") or []) if str(t)}
        for feature in features(memory):
            keys.add(PHYSICAL_PREFIX + feature)
        for key in keys:
            postings.setdefault(key, set()).add(mid)

    taglists = bytearray()
    tag_rows: list[tuple[int, int, int, int, bytes, str]] = []
    for tag in sorted(postings):
        ids = sorted(postings[tag])
        blob = compact_json({"tag": tag, "ids": ids})
        off = len(taglists)
        taglists += blob
        tag_rows.append((fnv1a64(tag), off, len(blob), len(ids), hashlib.sha256(blob).digest(), tag))

    return bytes(records), encode_index(id_rows), encode_index(tag_rows), bytes(taglists)


def zip_info(name: str, stored: bool) -> zipfile.ZipInfo:
    info = zipfile.ZipInfo(name, EPOCH)
    info.compress_type = zipfile.ZIP_STORED if stored else zipfile.ZIP_DEFLATED
    info.external_attr = (0o644 & 0xFFFF) << 16
    info.create_system = 3
    return info


def build_body(seed: Path, out: Path, version: str, abi: str, role: str) -> dict[str, Any]:
    seed_bytes = seed.read_bytes()
    root, memories = normalize_seed(json.loads(seed_bytes.decode("utf-8")))
    records, ididx, tagidx, taglists = build_sections(memories)

    genesis = {
        "format": GENESIS_FORMAT,
        "root": root,
        "memory_count": len(memories),
    }
    store = {
        "records": "store/records.bin",
        "id_index": "store/id.idx",
        "tag_index": "store/tag.idx",
        "tag_lists": "store/taglists.bin",
        "index_format": INDEX_FORMAT,
        "memory_count": len(memories),
    }
    entries: dict[str, tuple[bytes, bool]] = {
        "genesis.json": (pretty_json(genesis), False),
        store["records"]: (records, True),
        store["id_index"]: (ididx, True),
        store["tag_index"]: (tagidx, True),
        store["tag_lists"]: (taglists, True),
    }
    hashes = {name: sha256_bytes(data) for name, (data, _stored) in entries.items()}
    seed_digest = sha256_bytes(seed_bytes)
    manifest = {
        "role": role,
        "format": FORMAT,
        "version": version,
        "memory_abi": abi,
        "body_id": "memoryai-" + seed_digest[:24],
        "root": root,
        "genesis_path": "genesis.json",
        "entry": {},
        "hashes": hashes,
        "store": store,
    }
    entries["manifest.json"] = (pretty_json(manifest), False)

    out.parent.mkdir(parents=True, exist_ok=True)
    tmp = out.with_suffix(out.suffix + ".tmp")
    if tmp.exists():
        tmp.unlink()
    with zipfile.ZipFile(tmp, "w", allowZip64=True) as zf:
        for name in sorted(entries):
            data, stored = entries[name]
            zf.writestr(zip_info(name, stored), data)
        # In-body dual-slot mutation journal reserve. No sidecar is permitted.
        zf.comment = b"\x00" * JOURNAL_COMMENT_SIZE
    tmp.replace(out)

    verify_body(out)
    return {
        "body": str(out),
        "sha256": sha256_bytes(out.read_bytes()),
        "seed_sha256": seed_digest,
        "memory_count": len(memories),
        "root": root,
        "version": version,
        "memory_abi": abi,
        "body_id": manifest["body_id"],
    }


def verify_body(path: Path) -> None:
    with zipfile.ZipFile(path, "r") as zf:
        if len(zf.comment) != JOURNAL_COMMENT_SIZE:
            raise ValueError("Memory.mem mutation journal reserve is not 65535 bytes")
        names = zf.namelist()
        if len(names) != len(set(names)):
            raise ValueError("Memory.mem contains duplicate ZIP entry names")
        manifest = json.loads(zf.read("manifest.json"))
        if manifest.get("format") != FORMAT:
            raise ValueError("unexpected Memory body format")
        store = manifest["store"]
        if store.get("index_format") != INDEX_FORMAT:
            raise ValueError(f"unexpected Memory index format: {store.get('index_format')!r}")
        for field in ("records", "id_index", "tag_index", "tag_lists"):
            info = zf.getinfo(store[field])
            if info.compress_type != zipfile.ZIP_STORED:
                raise ValueError(f"indexed section {store[field]} must use zip.Store")
        for field in ("id_index", "tag_index"):
            size = zf.getinfo(store[field]).file_size
            if size % struct.calcsize("<QQII32s") != 0:
                raise ValueError(f"indexed section {store[field]} is not v2 entry aligned")
        if zf.getinfo(store["id_index"]).file_size // struct.calcsize("<QQII32s") != int(store["memory_count"]):
            raise ValueError("Memory ID index cardinality does not match memory_count")
        for name, expected in manifest.get("hashes", {}).items():
            got = sha256_bytes(zf.read(name))
            if got != expected:
                raise ValueError(f"hash mismatch for {name}: {got} != {expected}")


def main() -> None:
    ap = argparse.ArgumentParser(description="Build deterministic MemoryAI Memory.mem from the current executable Memory seed")
    ap.add_argument("--seed", default="Kernel/current-required-structures.json")
    ap.add_argument("--out", default="data/Memory.mem")
    ap.add_argument("--version", default=DEFAULT_VERSION)
    ap.add_argument("--abi", default=DEFAULT_ABI)
    ap.add_argument("--role", choices=("core", "storage"), default="core")
    ap.add_argument("--verify-only", action="store_true")
    args = ap.parse_args()
    out = Path(args.out)
    if args.verify_only:
        verify_body(out)
        print(json.dumps({"ok": True, "body": str(out), "sha256": sha256_bytes(out.read_bytes())}, indent=2))
        return
    result = build_body(Path(args.seed), out, args.version, args.abi, args.role)
    print(json.dumps(result, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
