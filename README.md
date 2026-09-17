# MemoryAI

Private source-of-truth repository for MemoryAI.

## Repository rule

Every code change starts from the latest `main` HEAD of `LeeYi-sen/MemoryAI`, is tested against that checkout, and is committed/pushed before further development continues.

## Architecture invariant

```text
Kernel = minimal physical runtime / safety / transport / VM / I/O / crash recovery
AI     = Memory body + executable/reusable Memory structures
```

There is no independent Skill subsystem. Capabilities emerge from experience-derived Memory structures.

## Development artifact policy

The development repository directly tracks canonical source plus `data/Memory.mem`.

Keep in Git:
- `Kernel/src/*.go` and other source/test code
- `Kernel/current-required-structures.json`
- `data/Memory.mem`
- architecture docs, migrations and development tests

Do not commit compiled Kernel/Gateway binaries, release packages, archives, compressed/base64 bootstrap chunks or split payloads. The historical v27 gzip/base64 reconstruction pipeline is retired.

Current development mode runs source/runtime tests directly (`go test`) and architecture/Memory-body validation. Release binary compilation and packaging are intentionally outside the current development phase.
