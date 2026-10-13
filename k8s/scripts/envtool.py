#!/usr/bin/env python3
"""envtool - the deployment-variable engine behind `make render` (Python 3.9+, standard library only,
except `inventory`/`lint` which use PyYAML when present).

Sub-commands
  lint                      template <-> sources consistency (T == E), syntax rules, collisions, secret-name rule
  check-env FILE [--strict-deploy]
                            parse + validate an environment file (types, ranges, cross-field rules)
  emit FILE                 NUL-separated NAME=VALUE of every template-scope variable (defaults applied)
  allowlist                 the envsubst allowlist: '${A} ${B} ...' for template-scope variables
  sources                   the exact list of template files the renderer processes (one per line)
  resolved FILE             sorted dotenv of ALL resolved variables (audit copy; contains no secrets by construction)
  inventory                 markdown variable -> file/resource/field inventory generated from the real sources

Exit codes: 0 ok | 1 validation/lint failure | 2 usage or environment error.

Nothing here evaluates shell code. Values are data: they are validated against a strict character set and are never
re-expanded.
"""
import os, re, sys
from fractions import Fraction

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
TEMPLATE = os.path.join(ROOT, "infrastructure.env.template")
SENTINEL = "__REQUIRED__"
GUARD = "__TEMPLATE_NOT_RENDERED__"
NAME_RE = re.compile(r"^[A-Z][A-Z0-9_]*$")
TOKEN_RE = re.compile(r"\$\{([^}]*)\}")           # any brace expansion
PURE_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
ANNOTATIONS = {"purpose", "type", "required", "scope", "env-specific", "constraints", "depends", "change-impact", "allow-empty"}
MANDATORY_ANN = ("purpose", "type", "required", "scope", "env-specific", "change-impact")
SECRET_NAME_RE = re.compile(r"(PASSWORD|SECRET|TOKEN|CREDENTIAL|PRIVATE|(^|_)KEY($|_))")
PLACEHOLDER_HOST_RE = re.compile(r"(^|\.)(example\.(com|org|net)|example|invalid|test|localhost|local)$")

# files that are rendered (templates) - everything else is never substituted
EXCLUDE_DIRS = {"build", "docs", "scripts", ".git"}
EXCLUDE_FILES = {"secret-templates.yaml"}


# ----------------------------------------------------------------------------------------- quantities
_SUFFIX = {"": Fraction(1), "n": Fraction(1, 10**9), "u": Fraction(1, 10**6), "m": Fraction(1, 1000),
           "k": Fraction(10**3), "M": Fraction(10**6), "G": Fraction(10**9), "T": Fraction(10**12),
           "P": Fraction(10**15), "E": Fraction(10**18), "Ki": Fraction(2**10), "Mi": Fraction(2**20),
           "Gi": Fraction(2**30), "Ti": Fraction(2**40), "Pi": Fraction(2**50), "Ei": Fraction(2**60)}
_QTY_RE = re.compile(r"^([0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:([eE][+-]?[0-9]+)|(Ki|Mi|Gi|Ti|Pi|Ei|n|u|m|k|M|G|T|P|E)?)$")


def parse_quantity(s):
    """Kubernetes resource.Quantity (positive) -> Fraction, or raise ValueError."""
    m = _QTY_RE.match(s or "")
    if not m:
        raise ValueError(f"not a valid Kubernetes quantity: {s!r}")
    num = Fraction(m.group(1))
    if m.group(2):
        return num * Fraction(10) ** int(m.group(2)[1:])
    return num * _SUFFIX[m.group(3) or ""]


# ----------------------------------------------------------------------------------------- type checks
def _hostname(v):
    if len(v) > 253 or "." not in v or v != v.lower():
        return "must be a lowercase DNS name with at least one dot (<=253 chars)"
    for lab in v.split("."):
        if not re.match(r"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$", lab):
            return f"invalid DNS label {lab!r}"
    if re.match(r"^[0-9.]+$", v):
        return "IP addresses are not accepted"


def _email(v):
    if not re.match(r"^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$", v) or len(v) > 254:
        return "not a valid e-mail address"


def _registry(v):
    if not re.match(r"^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?(/[a-z0-9]+([._-][a-z0-9]+)*)*$", v):
        return "expected host[:port][/path], lowercase, no scheme, no trailing slash"


def _tag(v):
    if not re.match(r"^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$", v):
        return "not a valid image tag"
    if v.lower() == "latest":
        return "'latest' is a mutable tag and is rejected"


def _imageref(v):
    m = re.match(r"^([a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?/)?([a-z0-9]+([._-][a-z0-9]+)*/)*[a-z0-9]+([._-][a-z0-9]+)*"
                 r"(:([A-Za-z0-9_][A-Za-z0-9_.-]{0,127}))?(@sha256:[a-f0-9]{64})?$", v)
    if not m:
        return "not a valid image reference"
    if not m.group(8) and not m.group(9):
        return "image reference must carry a tag or a sha256 digest"
    if m.group(8) and m.group(8).lower() == "latest" and not m.group(9):
        return "'latest' is a mutable tag and is rejected"


def _cron(v):
    f = v.split(" ")
    if len(f) != 5:
        return "expected 5 space-separated fields"
    rng = [(0, 59), (0, 23), (1, 31), (1, 12), (0, 7)]
    for part, (lo, hi) in zip(f, rng):
        for item in part.split(","):
            m = re.match(r"^(\*|(\d+)(-(\d+))?)(/(\d+))?$", item)
            if not m:
                return f"invalid cron field {part!r}"
            for g in (m.group(2), m.group(4)):
                if g is not None and not (lo <= int(g) <= hi):
                    return f"cron value {g} outside {lo}-{hi}"
            if m.group(6) is not None and int(m.group(6)) == 0:
                return "cron step must be > 0"


def _timezone(v):
    if v == "Local" or not re.match(r"^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$", v):
        return "not an IANA time-zone name ('Local' is not allowed)"
    try:
        import zoneinfo
        try:
            zoneinfo.ZoneInfo(v)
        except zoneinfo.ZoneInfoNotFoundError:
            return "unknown to this machine's tz database (the cluster's database may differ: verify there too)"
        except Exception:
            pass
    except ImportError:
        pass


def check_type(t, v):
    """Return an error string or None."""
    if t.startswith("int:"):
        _, lo, hi = t.split(":")
        if not re.match(r"^(0|[1-9][0-9]*)$", v):
            return "must be a non-negative integer without sign or leading zeros"
        if not (int(lo) <= int(v) <= int(hi)):
            return f"must be between {lo} and {hi}"
        return None
    if t.startswith("enum:"):
        allowed = t[5:].split("|")
        return None if v in allowed else f"must be one of: {', '.join(allowed)}"
    if t == "int-or-empty":
        if v == "":
            return None
        return check_type("int:1:36500", v)
    if t == "retention-mode":
        return None if v in ("", "GOVERNANCE", "COMPLIANCE") else "must be empty, GOVERNANCE or COMPLIANCE"
    simple = {
        "identifier": lambda x: None if re.match(r"^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])$", x) else "lowercase letters, digits, '-' (2-32 chars)",
        "bucket": lambda x: None if re.match(r"^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", x) and ".." not in x else "invalid S3 bucket name",
        "k8s-name": lambda x: None if re.match(r"^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$", x) else "not a valid Kubernetes resource name",
        "kube-context": lambda x: None if re.match(r"^[A-Za-z0-9_.:@/-]+$", x) else "unexpected characters in context name",
        "semver": lambda x: None if re.match(r"^[0-9]+\.[0-9]+\.[0-9]+$", x) else "expected MAJOR.MINOR.PATCH",
        "bool": lambda x: None if x in ("true", "false") else "must be exactly true or false",
        "hostname": _hostname, "email": _email, "image-registry": _registry, "image-tag": _tag,
        "image-ref": _imageref, "cron": _cron, "timezone": _timezone,
    }
    if t == "quantity":
        try:
            if parse_quantity(v) <= 0:
                return "must be greater than zero"
        except ValueError as e:
            return str(e)
        return None
    if t in simple:
        return simple[t](v)
    return f"unknown type {t!r} in template"


# ----------------------------------------------------------------------------------------- template parsing
class Var:
    def __init__(self, name, default, ann, section, line):
        self.name, self.default, self.ann, self.section, self.line = name, default, ann, section, line

    def __getitem__(self, k):
        return self.ann.get(k, "")


def parse_value(raw):
    v = raw.rstrip("\n")
    if len(v) >= 2 and v[0] == v[-1] and v[0] in "\"'":
        v = v[1:-1]
    return v


def parse_template(path=TEMPLATE):
    if not os.path.isfile(path):
        raise SystemExit(f"template not found: {path}")
    vars_, ann, section, errs = [], {}, "", []
    for i, line in enumerate(open(path, encoding="utf-8"), 1):
        s = line.rstrip("\n")
        if not s.strip():
            ann = {}
            continue
        m = re.match(r"^#\s*([a-z-]+):\s*(.*)$", s)
        if s.startswith("#"):
            hdr = re.match(r"^#\s+(\d+\. .*)$", s)
            if hdr:
                section = hdr.group(1)
            if m and m.group(1) in ANNOTATIONS:
                ann[m.group(1)] = m.group(2).strip()
            continue
        m = re.match(r"^([A-Za-z_][A-Za-z0-9_]*)=(.*)$", s)
        if not m:
            errs.append(f"{path}:{i}: not a NAME=value line: {s[:60]!r}")
            continue
        vars_.append(Var(m.group(1), parse_value(m.group(2)), ann, section, i))
        ann = {}
    return vars_, errs


def parse_envfile(path):
    """Strict dotenv parse. Returns (dict, errors)."""
    data, errs = {}, []
    try:
        lines = open(path, encoding="utf-8").read().split("\n")
    except OSError as e:
        raise SystemExit(f"cannot read env file: {e}")
    for i, line in enumerate(lines, 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        m = re.match(r"^([A-Za-z_][A-Za-z0-9_]*)=(.*)$", line)
        if not m:
            errs.append(f"{path}:{i}: not a NAME=value line (no 'export', no spaces around '=')")
            continue
        n, v = m.group(1), parse_value(m.group(2))
        if n in data:
            errs.append(f"{path}:{i}: duplicate variable {n}")
        data[n] = v
    return data, errs


# ----------------------------------------------------------------------------------------- sources
def source_files():
    out = []
    for d, dirs, files in os.walk(ROOT):
        rel_dir = os.path.relpath(d, ROOT)
        dirs[:] = sorted(x for x in dirs if not (rel_dir == "." and x in EXCLUDE_DIRS) and x != "__pycache__")
        for f in sorted(files):
            if f.endswith((".yaml", ".yml")) and f not in EXCLUDE_FILES:
                out.append(os.path.relpath(os.path.join(d, f), ROOT))
    return sorted(out)


def scan_sources():
    """-> (tokens: {name: [(file,line)]}, bad: [(file,line,text)], bare: [(file,line,name)])"""
    tokens, bad, lines_by_file = {}, [], {}
    for rel in source_files():
        text = open(os.path.join(ROOT, rel), encoding="utf-8").read()
        lines_by_file[rel] = text.split("\n")
        for ln, line in enumerate(lines_by_file[rel], 1):
            for m in TOKEN_RE.finditer(line):
                inner = m.group(1)
                if PURE_RE.match(inner):
                    tokens.setdefault(inner, []).append((rel, ln))
                else:
                    bad.append((rel, ln, m.group(0)))
    return tokens, bad, lines_by_file


def runtime_names(lines_by_file):
    """Names that belong to the application/runtime layer: ConfigMap keys, container env names, Secret keys."""
    names = set()
    for rel, lines in lines_by_file.items():
        for line in lines:
            m = re.match(r"^\s+- name: ([A-Z][A-Z0-9_]*)\s*$", line)
            if m:
                names.add(m.group(1))
            m = re.match(r"^  ([A-Z][A-Z0-9_]*): ", line)       # ConfigMap data keys (2-space indented UPPER keys)
            if m:
                names.add(m.group(1))
    sec = os.path.join(ROOT, "01-config", "secret-templates.yaml")
    if os.path.isfile(sec):
        for line in open(sec, encoding="utf-8"):
            m = re.match(r"^  ([A-Z][A-Z0-9_]*): ", line)
            if m:
                names.add(m.group(1))
    return names


# ----------------------------------------------------------------------------------------- lint
def cmd_lint():
    errs = []
    vars_, perr = parse_template()
    errs += perr
    seen = {}
    for v in vars_:
        if not NAME_RE.match(v.name):
            errs.append(f"template:{v.line}: variable name {v.name!r} must match [A-Z][A-Z0-9_]*")
        if v.name in seen:
            errs.append(f"template:{v.line}: duplicate variable {v.name} (first at line {seen[v.name]})")
        seen[v.name] = v.line
        if SECRET_NAME_RE.search(v.name):
            errs.append(f"template:{v.line}: {v.name} looks like a secret; secrets never belong in this file")
        for k in MANDATORY_ANN:
            if not v[k]:
                errs.append(f"template:{v.line}: {v.name} lacks the '{k}' annotation")
        if v["scope"] not in ("template", "cli"):
            errs.append(f"template:{v.line}: {v.name} scope must be template|cli")
        if v["required"] not in ("yes", "no"):
            errs.append(f"template:{v.line}: {v.name} required must be yes|no")
        if re.search(r"[\"'`$\\#]", v.default) or any(ord(c) < 32 for c in v.default):
            errs.append(f"template:{v.line}: {v.name} default contains a forbidden character")
        if v["required"] == "yes" and v.default != SENTINEL:
            errs.append(f"template:{v.line}: {v.name} is required but its value is not {SENTINEL}")
        if v["required"] == "no":
            if v.default == SENTINEL:
                errs.append(f"template:{v.line}: {v.name} is optional but set to {SENTINEL}")
            elif v.default == "" and v["allow-empty"] != "yes":
                errs.append(f"template:{v.line}: {v.name} has an empty default without 'allow-empty: yes'")
            elif v.default != "" or v["allow-empty"] == "yes":
                e = check_type(v["type"], v.default)
                if e and not (v.default == "" and v["allow-empty"] == "yes"):
                    errs.append(f"template:{v.line}: default of {v.name} invalid: {e}")
    E_t = {v.name for v in vars_ if v["scope"] == "template"}
    E_c = {v.name for v in vars_ if v["scope"] == "cli"}

    tokens, bad, lines_by_file = scan_sources()
    for rel, ln, txt in bad:
        errs.append(f"{rel}:{ln}: {txt} is not a plain deployment variable. Brace form is reserved for deployment variables; "
                    "use $NAME for shell variables and $(NAME) for Kubernetes references")
    T = set(tokens)
    for n in sorted(T - E_t):
        loc = ", ".join(f"{f}:{l}" for f, l in tokens[n][:3])
        errs.append(f"T\\E: ${{{n}}} is used ({loc}) but not defined (as scope=template) in the template")
    for n in sorted(E_t - T):
        errs.append(f"E\\T: {n} is defined in the template but never used by any manifest")
    # CLI-scope variables must be consumed by the scripts/Makefile
    script_text = ""
    for base in ("scripts", ""):
        d = os.path.join(ROOT, base)
        for f in sorted(os.listdir(d)):
            p = os.path.join(d, f)
            if os.path.isfile(p) and (base == "scripts" or f == "Makefile"):
                try:
                    script_text += open(p, encoding="utf-8").read()
                except UnicodeDecodeError:
                    pass
    for n in sorted(E_c):
        if n not in script_text:
            errs.append(f"CLI variable {n} is not referenced by any script or the Makefile")
        if n in T:
            errs.append(f"CLI variable {n} must not be substituted into manifests")
    # collisions: bare $NAME of an allowlisted variable (envsubst would substitute it)
    for rel, lines in lines_by_file.items():
        for ln, line in enumerate(lines, 1):
            for m in re.finditer(r"\$([A-Za-z_][A-Za-z0-9_]*)", line):
                if m.group(1) in E_t:
                    errs.append(f"{rel}:{ln}: bare ${m.group(1)} collides with a deployment variable and would be substituted")
    for n in sorted(runtime_names(lines_by_file) & (E_t | E_c)):
        errs.append(f"name collision: {n} is both a deployment variable and a runtime (ConfigMap/env/Secret) name")
    # every template must itself be valid YAML (an unquoted placeholder inside a flow mapping, for example, is not)
    try:
        import yaml
        for rel in source_files():
            try:
                list(yaml.safe_load_all(open(os.path.join(ROOT, rel), encoding="utf-8")))
            except yaml.YAMLError as e:
                errs.append(f"{rel}: template is not valid YAML before substitution: {str(e).splitlines()[0]} ({str(e).splitlines()[-1].strip()[:80]})")
    except ImportError:
        print("note: PyYAML not installed - template YAML syntax check skipped", file=sys.stderr)
    # template must not hold executable content on non-comment lines
    for i, line in enumerate(open(TEMPLATE, encoding="utf-8"), 1):
        if not line.startswith("#") and re.search(r"[`]|\$\(", line):
            errs.append(f"template:{i}: command substitution is not allowed")
    # guard marker must exist only in kustomization files
    for rel, lines in lines_by_file.items():
        for ln, line in enumerate(lines, 1):
            if GUARD in line and not rel.endswith("kustomization.yaml"):
                errs.append(f"{rel}:{ln}: guard marker outside a kustomization file")
    if errs:
        print("\n".join("LINT ERROR: " + e for e in errs))
        return 1
    print(f"lint OK: {len(E_t)} template-scope + {len(E_c)} cli-scope variables; T == E ({len(T)} variables used in {len(source_files())} source files)")
    return 0


# ----------------------------------------------------------------------------------------- env resolution
def resolve(path, strict=False):
    """-> (resolved dict, errors list, notes list)"""
    vars_, perr = parse_template()
    if perr:
        return {}, perr, []
    by = {v.name: v for v in vars_}
    data, errs = parse_envfile(path)
    notes = []
    for n in sorted(data):
        if n not in by:
            errs.append(f"unknown variable {n} (typo? not defined in infrastructure.env.template)")
    resolved, defaulted = {}, []
    for v in vars_:
        if v.name in data:
            val = data[v.name]
        else:
            val = v.default
            if val != SENTINEL:
                defaulted.append(v.name)
        resolved[v.name] = val
    for v in vars_:
        val = resolved[v.name]
        if val == SENTINEL or (val == "" and v["allow-empty"] != "yes" and v.name in data) or (val == "" and v["required"] == "yes"):
            errs.append(f"{v.name}: required value missing (still {SENTINEL if val == SENTINEL else 'empty'}); {v['purpose'][:90]}")
            continue
        if val == "" and v["allow-empty"] == "yes":
            continue
        if re.search(r"[\"'`$\\#]", val) or any(ord(c) < 32 for c in val) or val != val.strip() or (" " in val and v["type"] != "cron"):
            errs.append(f"{v.name}: value contains a forbidden character or whitespace (not allowed: $ ` \" ' \\ # control chars; spaces only in cron)")
            continue
        e = check_type(v["type"], val)
        if e:
            errs.append(f"{v.name}: {e} (got {val!r}; constraints: {v['constraints']})")
    if defaulted:
        notes.append(f"{len(defaulted)} variable(s) not set in the env file use the template default")
    if errs:
        return resolved, errs, notes
    R = resolved
    # ---- cross-field rules (only reached when every individual value is valid)
    def q(n):
        return parse_quantity(R[n])
    for n in sorted(R):
        if n.endswith("_CPU_REQUEST") or n.endswith("_MEMORY_REQUEST"):
            lim = n.replace("_REQUEST", "_LIMIT")
            if lim in R and q(n) > q(lim):
                errs.append(f"{n} ({R[n]}) must be <= {lim} ({R[lim]})")
        if n.endswith("_MIN_REPLICAS"):
            mx = n.replace("_MIN_", "_MAX_")
            if mx in R and int(R[n]) > int(R[mx]):
                errs.append(f"{n} ({R[n]}) must be <= {mx} ({R[mx]})")
            if int(R[n]) < 1:
                errs.append(f"{n} must be >= 1 (an HPA minimum of 0 is invalid)")
    if q("BACKUP_CPU_LIMIT") < parse_quantity("100m"):
        errs.append("BACKUP_CPU_LIMIT must be >= 100m (the fixed request)")
    if q("BACKUP_MEMORY_LIMIT") < parse_quantity("128Mi"):
        errs.append("BACKUP_MEMORY_LIMIT must be >= 128Mi (the fixed request)")
    for a, b in (("QUOTA_REQUESTS_CPU", "QUOTA_LIMITS_CPU"), ("QUOTA_REQUESTS_MEMORY", "QUOTA_LIMITS_MEMORY")):
        if q(a) > q(b):
            errs.append(f"{a} ({R[a]}) must be <= {b} ({R[b]})")
    total = sum(q(n) for n in R if n.startswith("PVC_SIZE_"))
    if total > q("QUOTA_REQUESTS_STORAGE"):
        errs.append(f"sum of PVC_SIZE_* exceeds QUOTA_REQUESTS_STORAGE ({R['QUOTA_REQUESTS_STORAGE']})")
    lock, mode, days = R["MINIO_COLD_OBJECT_LOCK"], R["MINIO_COLD_RETENTION_MODE"], R["MINIO_COLD_RETENTION_DAYS"]
    if (mode or days) and lock != "true":
        errs.append("MINIO_COLD_RETENTION_MODE/DAYS require MINIO_COLD_OBJECT_LOCK=true")
    if bool(mode) != bool(days):
        errs.append("MINIO_COLD_RETENTION_MODE and MINIO_COLD_RETENTION_DAYS must be set together (or both empty)")
    if lock == "true" and R["MINIO_COLD_VERSIONING"] != "true":
        errs.append("MINIO_COLD_OBJECT_LOCK=true requires MINIO_COLD_VERSIONING=true")
    if int(R["API_MAX_REPLICAS"]) >= 11:
        errs.append("API_MAX_REPLICAS must be < 11 (worker NODE_IDs start at 11)")
    if strict:
        for n in ("NUMAN_DOMAIN", "IMAGE_REGISTRY"):
            host = R[n].split("/")[0].split(":")[0]
            if PLACEHOLDER_HOST_RE.search(host):
                errs.append(f"{n}: {R[n]!r} is a reserved/placeholder domain; refusing to deploy")
        dom = R["ACME_EMAIL"].split("@")[1]
        if PLACEHOLDER_HOST_RE.search(dom):
            errs.append(f"ACME_EMAIL: domain {dom!r} is a reserved/placeholder domain; refusing to deploy")
        if R["TLS_ISSUER"] == "letsencrypt-staging":
            notes.append("TLS_ISSUER=letsencrypt-staging: certificates will NOT be trusted by browsers")
    return R, errs, notes


def cmd_check(path, strict):
    R, errs, notes = resolve(path, strict)
    for n in notes:
        print("note:", n, file=sys.stderr)
    if errs:
        print("\n".join("ENV ERROR: " + e for e in errs))
        return 1
    print(f"env OK: {path}")
    return 0


def cmd_emit(path):
    R, errs, _ = resolve(path)
    if errs:
        print("\n".join("ENV ERROR: " + e for e in errs), file=sys.stderr)
        return 1
    vars_, _ = parse_template()
    for v in vars_:
        if v["scope"] == "template":
            sys.stdout.write(f"{v.name}={R[v.name]}\0")
    return 0


def cmd_allowlist():
    vars_, _ = parse_template()
    print(" ".join("${%s}" % v.name for v in vars_ if v["scope"] == "template"))
    return 0


def cmd_resolved(path):
    R, errs, _ = resolve(path)
    if errs:
        print("\n".join("ENV ERROR: " + e for e in errs), file=sys.stderr)
        return 1
    for n in sorted(R):
        print(f"{n}={R[n]}")
    return 0


# ----------------------------------------------------------------------------------------- inventory
def walk(node, path, hits, token_re):
    if isinstance(node, dict):
        for k, v in node.items():
            walk(v, path + [str(k)], hits, token_re)
    elif isinstance(node, list):
        for i, v in enumerate(node):
            walk(v, path + [f"[{i}]"], hits, token_re)
    elif isinstance(node, str):
        for m in token_re.finditer(node):
            hits.append((m.group(1), ".".join(path).replace(".[", "[")))


def cmd_inventory():
    try:
        import yaml
    except ImportError:
        print("inventory needs PyYAML (pip install pyyaml)", file=sys.stderr)
        return 2
    vars_, _ = parse_template()
    refs = {v.name: [] for v in vars_}
    token_re = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")
    for rel in source_files():
        text = open(os.path.join(ROOT, rel), encoding="utf-8").read()
        for doc in yaml.safe_load_all(text):
            if not isinstance(doc, dict):
                continue
            kind = doc.get("kind", "?")
            name = (doc.get("metadata") or {}).get("name", "-")
            hits = []
            walk(doc, [], hits, token_re)
            for n, fld in hits:
                if n in refs:
                    refs[n].append((rel, f"{kind}/{name}", fld))
    out = ["# Deployment-variable inventory", "",
           "*Generated by `make inventory` (`scripts/envtool.py inventory`) from the real template and sources - do not edit by hand.*", "",
           f"Variables: **{len(vars_)}** ({sum(1 for v in vars_ if v['scope']=='template')} substituted into manifests, "
           f"{sum(1 for v in vars_ if v['scope']=='cli')} consumed by the CLI scripts only; "
           f"{sum(1 for v in vars_ if v.default == SENTINEL)} require operator input).", "",
           "Legend: **Req** = operator input required (no safe default). **Immutable/impact** is the `change-impact` annotation.", ""]
    cur = None
    for v in vars_:
        if v.section != cur:
            cur = v.section
            out += ["", f"## {cur}", "", "| Variable | Req | Default | Type | Used in (file → resource → field) | Validation | Change impact |", "|---|---|---|---|---|---|---|"]
        uses = refs[v.name]
        if v["scope"] == "cli":
            used = "CLI scripts (`scripts/`, `Makefile`)"
        else:
            used = "<br>".join(f"`{f}` → {r} → `{fld}`" for f, r, fld in uses)
        dflt = "**operator input**" if v.default == SENTINEL else (f"`{v.default}`" if v.default else "*(empty)*")
        out.append(f"| `{v.name}` | {'yes' if v['required']=='yes' else 'no'} | {dflt} | `{v['type']}` | {used} | {v['constraints'] or '-'} | {v['change-impact']} |".replace("\n", " "))
    print("\n".join(out))
    return 0


def main(argv):
    if len(argv) < 2:
        print(__doc__)
        return 2
    c = argv[1]
    if c == "lint":
        return cmd_lint()
    if c == "allowlist":
        return cmd_allowlist()
    if c == "sources":
        print("\n".join(source_files()))
        return 0
    if c == "inventory":
        return cmd_inventory()
    if c in ("check-env", "emit", "resolved") and len(argv) >= 3:
        if c == "check-env":
            return cmd_check(argv[2], "--strict-deploy" in argv[3:])
        return cmd_emit(argv[2]) if c == "emit" else cmd_resolved(argv[2])
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
