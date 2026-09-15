# MemoryAI

Private source-of-truth repository for MemoryAI.

## Repository rule

Every code change must start from the latest `main` HEAD of `LeeYi-sen/MemoryAI`, be tested against that checkout, and be committed back before further development continues.

## Architecture invariant

```text
Kernel = minimal physical runtime / safety / transport / VM / I/O / crash recovery
AI     = Memory body + executable/reusable Memory structures
```

There is no independent Skill subsystem. Capabilities emerge from experience-derived Memory structures.

## Artifact policy

Large runtime artifacts are not treated as source files. `data/Memory.mem` and compiled Kernel/Gateway binaries are bound by hashes/manifests and rebuilt/recovered from the source baseline. The repository stores Kernel/Gateway source, build tools, required structures, configuration, scripts, architecture docs and acceptance manifests.

Initial source baseline: MemoryAI v27 repair_73_92, recovered from the latest complete local release artifact before this repository was created.
