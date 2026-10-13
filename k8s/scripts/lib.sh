#!/usr/bin/env bash
# Shared helpers (sourced by the scripts; never executed on its own).

die()  { echo "ERROR: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "required tool not found: $1 ($2)"; }

# Hash of everything that will be applied + the resolved variables. Deterministic.
render_hash() { # $1 = build dir
  ( cd "$1" && LC_ALL=C find manifests resolved.env -type f | LC_ALL=C sort | while read -r f; do
      if command -v sha256sum >/dev/null 2>&1; then sha256sum "$f"; else shasum -a 256 "$f"; fi
    done ) | { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi; } | cut -d' ' -f1
}

# read_var NAME BUILD_DIR -> value from build/resolved.env (the audited, fully resolved variable set)
read_var() { grep -m1 "^$1=" "$2/resolved.env" | cut -d= -f2-; }

kbuild() { if command -v kustomize >/dev/null 2>&1; then kustomize build "$@"; else kubectl kustomize "$@"; fi; }
