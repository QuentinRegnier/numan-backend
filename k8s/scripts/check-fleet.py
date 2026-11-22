#!/usr/bin/env python3
"""Multi-datacenter fleet validator for Numan Kubernetes environments.

Validates 2+ datacenter infrastructure .env files together to guarantee zero
cross-cluster collisions across:
  - DC_ID (logical datacenter slug)
  - DC_INDEX (0..7 Snowflake 128-ID block index)
  - KUBE_CONTEXT (target Kubernetes cluster)
  - ENVIRONMENT_NAME (restic backup host prefix)
  - Snowflake 10-bit NODE_ID ranges (API, WebSocket, Worker across all DCs)

Uses Python standard library only (imports envtool.py).

Usage:
  python3 k8s/scripts/check-fleet.py dc-fr-par.env dc-de-fra.env [...]
  python3 k8s/scripts/check-fleet.py --strict dc-fr-par.env dc-de-fra.env
"""
import os
import sys

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, SCRIPT_DIR)
import envtool  # noqa: E402


def active_snowflake_ranges(resolved):
    ranges = []
    api_start = int(resolved["API_NODE_ID_START"])
    api_end = api_start + int(resolved["API_MAX_REPLICAS"]) - 1
    ranges.append(("API", api_start, api_end))

    ws_replicas = int(resolved["WS_REPLICAS"])
    if ws_replicas > 0:
        ws_start = int(resolved["WS_NODE_ID_START"])
        ranges.append(("WS", ws_start, ws_start + ws_replicas - 1))

    worker_replicas = int(resolved["WORKER_REPLICAS"])
    if worker_replicas > 0:
        w_start = int(resolved["WORKER_NODE_ID_START"])
        ranges.append(("Worker", w_start, w_start + worker_replicas - 1))

    return ranges


def validate_fleet(paths, strict=False):
    errors = []
    entries = []

    for path in paths:
        if not os.path.isfile(path):
            errors.append(f"{path}: file not found")
            continue
        resolved, env_errs, _ = envtool.resolve(path, strict=strict)
        if env_errs:
            for e in env_errs:
                errors.append(f"{path}: {e}")
            continue
        entries.append((path, resolved))

    if errors:
        return entries, errors

    unique_fields = [
        ("DC_ID", "datacenter identifier"),
        ("DC_INDEX", "Snowflake datacenter index"),
        ("KUBE_CONTEXT", "kubectl context"),
        ("ENVIRONMENT_NAME", "environment / backup host identifier"),
    ]
    for field, desc in unique_fields:
        seen = {}
        for path, resolved in entries:
            val = resolved[field]
            if val in seen:
                errors.append(
                    f"fleet collision on {field} ({desc}): value {val!r} is used by both {seen[val]} and {path}"
                )
            else:
                seen[val] = path

    all_ranges = []
    for path, resolved in entries:
        dc_id = resolved["DC_ID"]
        for role_label, start, end in active_snowflake_ranges(resolved):
            all_ranges.append((dc_id, path, role_label, start, end))

    for i in range(len(all_ranges)):
        dc1, p1, r1, s1, e1 = all_ranges[i]
        for j in range(i + 1, len(all_ranges)):
            dc2, p2, r2, s2, e2 = all_ranges[j]
            if s1 <= e2 and s2 <= e1:
                errors.append(
                    f"Snowflake NODE_ID collision: {dc1}/{r1} ({s1}..{e1} in {p1}) "
                    f"overlaps {dc2}/{r2} ({s2}..{e2} in {p2})"
                )

    return entries, errors


def main(argv):
    strict = False
    paths = []
    for arg in argv[1:]:
        if arg == "--strict":
            strict = True
        elif arg in ("-h", "--help"):
            print(__doc__.strip())
            return 0
        else:
            paths.append(arg)

    if len(paths) < 2:
        print("FLEET ERROR: provide at least 2 datacenter .env files to validate cross-DC uniqueness.", file=sys.stderr)
        print("Usage: check-fleet.py [--strict] <dc1.env> <dc2.env> [...]", file=sys.stderr)
        return 2

    entries, errors = validate_fleet(paths, strict=strict)
    if errors:
        for e in errors:
            print("FLEET ERROR:", e, file=sys.stderr)
        return 1

    print(f"fleet OK: {len(entries)} datacenters validated with zero collisions")
    print(f"{'DC_ID':<12} {'IDX':<4} {'BLOCK':<10} {'PROFILE':<15} {'ROLE':<10} {'NODE_ID RANGES'}")
    for _, R in entries:
        idx = int(R["DC_INDEX"])
        block = f"{idx * 128}..{idx * 128 + 127}"
        ranges_str = ", ".join(f"{lbl}:{s}..{e}" for lbl, s, e in active_snowflake_ranges(R))
        print(
            f"{R['DC_ID']:<12} {idx:<4} {block:<10} {R['CLUSTER_PROFILE']:<15} "
            f"{R['API_SERVER_ROLE']:<10} {ranges_str}"
        )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
