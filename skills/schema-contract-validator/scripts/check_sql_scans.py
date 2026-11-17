#!/usr/bin/env python3
"""
Lightweight static contract checker for numan-backend.
Compares SQL functions (`RETURNS TABLE (...)` and `RETURNS SETOF <view>`)
defined in schema.sql against `SELECT * FROM ...` calls and `rows.Scan(...)`
or `dest := []any{...}` in internal/repository/postgres/.
"""

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parents[3]
SCHEMA_PATH = ROOT / "schema.sql"
PG_REPO_DIR = ROOT / "internal" / "repository" / "postgres"


def split_top_level_commas(text: str) -> list[str]:
    """Split string on top-level commas (outside parentheses)."""
    items: list[str] = []
    depth = 0
    current: list[str] = []
    for ch in text:
        if ch == "(":
            depth += 1
            current.append(ch)
        elif ch == ")":
            depth = max(0, depth - 1)
            current.append(ch)
        elif ch == "," and depth == 0:
            item = "".join(current).strip()
            if item:
                items.append(item)
            current = []
        else:
            current.append(ch)
    tail = "".join(current).strip()
    if tail:
        items.append(tail)
    return items


def strip_sql_comments(text: str) -> str:
    text = re.sub(r"/\*.*?\*/", " ", text, flags=re.DOTALL)
    lines = [re.sub(r"--.*$", "", line) for line in text.splitlines()]
    return " ".join(lines)


def extract_sql_views(schema_text: str) -> dict[str, int]:
    """Index column counts of `CREATE [OR REPLACE] VIEW schema.name AS SELECT ... FROM ...`."""
    views: dict[str, int] = {}
    pattern = re.compile(
        r"CREATE\s+(?:OR\s+REPLACE\s+)?VIEW\s+([a-zA-Z0-9_]+\.[a-zA-Z0-9_]+)\s+AS\s+SELECT\s+(.*?)\s+FROM\b",
        re.IGNORECASE | re.DOTALL,
    )
    for match in pattern.finditer(schema_text):
        view_name = match.group(1)
        select_list = strip_sql_comments(match.group(2))
        views[view_name] = len(split_top_level_commas(select_list))
    return views


def extract_sql_functions(schema_text: str, views: dict[str, int]) -> dict[str, int]:
    """Index column counts returned by `RETURNS TABLE(...)` and `RETURNS SETOF <view>`."""
    results: dict[str, int] = {}

    table_pattern = re.compile(
        r"CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([a-zA-Z0-9_]+\.[a-zA-Z0-9_]+)\s*\([^)]*\)\s*RETURNS\s+TABLE\s*\((.*?)\)\s*(?:LANGUAGE|AS|\$\$)",
        re.IGNORECASE | re.DOTALL,
    )
    for match in table_pattern.finditer(schema_text):
        func_name = match.group(1)
        table_def = strip_sql_comments(match.group(2))
        results[func_name] = len(split_top_level_commas(table_def))

    setof_pattern = re.compile(
        r"CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([a-zA-Z0-9_]+\.[a-zA-Z0-9_]+)\s*\([^)]*\)\s*RETURNS\s+SETOF\s+([a-zA-Z0-9_]+\.[a-zA-Z0-9_]+)",
        re.IGNORECASE | re.DOTALL,
    )
    for match in setof_pattern.finditer(schema_text):
        func_name = match.group(1)
        target_view = match.group(2)
        if target_view in views:
            results[func_name] = views[target_view]

    return results


def count_scan_destinations(go_text: str) -> int | None:
    """Extract destination count from `dest := []any{...}` or `rows.Scan(...)`."""
    dest_match = re.search(r"dest\s*:=\s*\[\](?:any|interface\{\})\s*\{(.*?)\}", go_text, re.DOTALL)
    if dest_match:
        body = re.sub(r"//.*$", "", dest_match.group(1), flags=re.MULTILINE)
        return len(split_top_level_commas(body))

    scan_match = re.search(r"\b(?:rows|row)\.Scan\s*\((.*?)\)", go_text, re.DOTALL)
    if scan_match:
        args = scan_match.group(1).strip()
        if args.startswith("dest"):
            return None
        body = re.sub(r"//.*$", "", args, flags=re.MULTILINE)
        return len(split_top_level_commas(body))

    return None


def inspect_go_repositories(sql_funcs: dict[str, int]) -> int:
    mismatches = 0
    checked = 0
    for go_file in sorted(PG_REPO_DIR.glob("*.go")):
        if go_file.name.endswith("_test.go"):
            continue
        text = go_file.read_text(encoding="utf-8")
        func_calls = re.findall(r"SELECT\s+\*\s+FROM\s+([a-zA-Z0-9_]+\.[a-zA-Z0-9_]+)\s*\(", text, re.IGNORECASE)
        scan_count = count_scan_destinations(text)
        if not func_calls or scan_count is None:
            continue

        for sql_func in dict.fromkeys(func_calls):
            expected = sql_funcs.get(sql_func)
            if expected is None:
                continue
            checked += 1
            if expected != scan_count:
                mismatches += 1
                print(
                    f"[MISMATCH] {go_file.relative_to(ROOT)} -> {sql_func}: "
                    f"schema.sql returns {expected} columns, Go Scan expects {scan_count} destinations"
                )
            else:
                print(f"[OK] {go_file.relative_to(ROOT)} -> {sql_func} ({scan_count} columns)")

    print(f"\nSummary: {checked} function(s) verified, {mismatches} mismatch(es) detected.")
    return 1 if mismatches > 0 else 0


def main() -> int:
    if not SCHEMA_PATH.exists():
        print(f"Error: {SCHEMA_PATH} not found.", file=sys.stderr)
        return 2
    schema_text = SCHEMA_PATH.read_text(encoding="utf-8")
    views = extract_sql_views(schema_text)
    sql_funcs = extract_sql_functions(schema_text, views)
    print(f"Indexed views: {len(views)} | Indexed SQL functions: {len(sql_funcs)}")
    return inspect_go_repositories(sql_funcs)


if __name__ == "__main__":
    sys.exit(main())
