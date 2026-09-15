#!/usr/bin/env python3
from __future__ import annotations

import re


def _replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"parallel-boundary overlay {label}: expected one match, got {count}")
    return text.replace(old, new, 1)


def apply_kernel_parallel_boundary_overlay(raw: bytes) -> bytes:
    text = raw.decode("utf-8")
    required = (
        'case "parallel_score6":',
        'globalParallelRuntime.Score6',
        'case "cognition_stats":',
        'case "cognition_concurrency_set":',
        'case "mesh_route_cognition":',
        'm.routeCognition(',
        'func (e *Engine) execPrimitive(',
    )
    missing = [token for token in required if token not in text]
    if missing:
        raise RuntimeError(f"parallel-boundary overlay upstream boundary missing: {missing}")

    # Fixed six-lane score shape is not a physical Kernel primitive.
    pattern = re.compile(
        r'\n\tcase "parallel_score6":.*?(?=\n\tcase "|\n\tdefault:|\n\t})',
        re.S,
    )
    text, count = pattern.subn("", text, count=1)
    if count != 1:
        raise RuntimeError(f"parallel-boundary overlay expected one score6 case, got {count}")

    # Physical telemetry/concurrency must not present itself as a cognition ABI.
    text = _replace_once(
        text,
        '''\tcase "cognition_stats":
\t\tb, _ := json.Marshal(cognitionInfo())
\t\tf.Vars[op.A] = string(b)
''',
        '''\tcase "physical_runtime_stats":
\t\tb, _ := json.Marshal(physicalRuntimeInfo())
\t\tf.Vars[op.A] = string(b)
''',
        "runtime stats primitive",
    )
    text = _replace_once(
        text,
        '''\tcase "cognition_concurrency_set":
\t\tn := int(num(x(op.A)))
\t\tif n < 1 {
\t\t\tn = 1
\t\t}
\t\tmaxPhysical := runtime.GOMAXPROCS(0)
\t\tif maxPhysical < 1 {
\t\t\tmaxPhysical = 1
\t\t}
\t\tif n > maxPhysical {
\t\t\tn = maxPhysical
\t\t}
\t\tsetCognitionConcurrency(n)
''',
        '''\tcase "physical_execution_concurrency_set":
\t\tn := int(num(x(op.A)))
\t\tif n < 1 {
\t\t\tn = 1
\t\t}
\t\tmaxPhysical := runtime.GOMAXPROCS(0)
\t\tif maxPhysical < 1 {
\t\t\tmaxPhysical = 1
\t\t}
\t\tif n > maxPhysical {
\t\t\tn = maxPhysical
\t\t}
\t\tsetPhysicalExecutionConcurrency(n)
''',
        "physical concurrency primitive",
    )
    text = _replace_once(text, 'case "mesh_route_cognition":', 'case "mesh_route_execution":', "mesh route primitive")
    text = _replace_once(text, 'errors.New("mesh route cognition requires sovereign mode")', 'errors.New("mesh route execution requires sovereign mode")', "mesh route error")
    text = _replace_once(text, 'm.routeCognition(', 'm.routeExecution(', "mesh physical route call")

    forbidden = (
        'case "parallel_score6":',
        'globalParallelRuntime.Score6',
        'case "cognition_stats":',
        'case "cognition_concurrency_set":',
        'setCognitionConcurrency(',
        'case "mesh_route_cognition":',
        'm.routeCognition(',
    )
    bad = [token for token in forbidden if token in text]
    if bad:
        raise RuntimeError(f"cognition-shaped Kernel ABI remained: {bad}")
    required_after = (
        'case "physical_runtime_stats":',
        'physicalRuntimeInfo()',
        'case "physical_execution_concurrency_set":',
        'setPhysicalExecutionConcurrency(n)',
        'case "mesh_route_execution":',
        'm.routeExecution(',
    )
    missing = [token for token in required_after if token not in text]
    if missing:
        raise RuntimeError(f"physical Kernel ABI missing after overlay: {missing}")
    return text.encode("utf-8")
