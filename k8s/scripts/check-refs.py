#!/usr/bin/env python3
"""Cross-reference checker for the rendered manifests.

Usage: kustomize build . | python3 scripts/check-refs.py 01-config/secret-templates.yaml

Verifies what schema validation cannot: every Secret/ConfigMap key referenced by a pod exists, Services select real
pods and ports, Ingress backends exist, ServiceAccounts/HPA targets/PDB selectors resolve, and every
numan.io/uses-* pod label is matched by the NetworkPolicies; the declared workloads fit the ResourceQuota; the Redis
container memory limit covers maxmemory. It proves manifests are CONSISTENT, not that they RUN.
"""
import os, re, sys
import yaml
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from envtool import parse_quantity

rendered = [d for d in yaml.safe_load_all(sys.stdin) if d]
secrets = {}
for d in yaml.safe_load_all(open(sys.argv[1])):
    if d and d["kind"] == "Secret":
        secrets[d["metadata"]["name"]] = set(d.get("stringData", {}) or {}) | set(d.get("data", {}) or {})

by_kind = {}
for d in rendered:
    by_kind.setdefault(d["kind"], {})[d["metadata"]["name"]] = d
cms = {n: set(d.get("data", {}) or {}) for n, d in by_kind.get("ConfigMap", {}).items()}
sas = set(by_kind.get("ServiceAccount", {}))
errors = []

def pod_template(d):
    k = d["kind"]
    if k in ("Deployment", "StatefulSet"):
        return d["spec"]["template"]
    if k == "CronJob":
        return d["spec"]["jobTemplate"]["spec"]["template"]
    if k == "Job":
        return d["spec"]["template"]
    return None

workloads = {}
for d in rendered:
    t = pod_template(d)
    if not t:
        continue
    name = f'{d["kind"]}/{d["metadata"]["name"]}'
    workloads[name] = t
    spec = t["spec"]
    if spec.get("serviceAccountName") not in sas:
        errors.append(f"{name}: serviceAccount {spec.get('serviceAccountName')!r} missing")
    for c in spec.get("containers", []) + spec.get("initContainers", []):
        if "resources" not in c or "limits" not in c["resources"] or "requests" not in c["resources"]:
            errors.append(f"{name}/{c['name']}: requests+limits required")
        for e in c.get("env", []):
            vf = e.get("valueFrom", {})
            if "secretKeyRef" in vf:
                r = vf["secretKeyRef"]
                if r["name"] not in secrets:
                    errors.append(f"{name}: secret {r['name']} not in templates")
                elif r["key"] not in secrets[r["name"]]:
                    errors.append(f"{name}: key {r['key']} missing in secret {r['name']}")
            if "configMapKeyRef" in vf:
                r = vf["configMapKeyRef"]
                if r["name"] not in cms:
                    errors.append(f"{name}: configmap {r['name']} missing")
                elif r["key"] not in cms[r["name"]]:
                    errors.append(f"{name}: key {r['key']} missing in configmap {r['name']}")
        for ef in c.get("envFrom", []):
            n = ef.get("configMapRef", {}).get("name")
            if n and n not in cms:
                errors.append(f"{name}: envFrom configmap {n} missing")
    for v in spec.get("volumes", []):
        if "configMap" in v and v["configMap"]["name"] not in cms:
            errors.append(f"{name}: volume configmap {v['configMap']['name']} missing")

def ports_of(t):
    out = {}
    for c in t["spec"]["containers"]:
        for p in c.get("ports", []):
            out[p.get("name")] = p["containerPort"]; out[p["containerPort"]] = p["containerPort"]
    return out

def match(sel, labels):
    return all(labels.get(k) == v for k, v in sel.items())

svc_ports = {}
for n, s in by_kind.get("Service", {}).items():
    sel = s["spec"].get("selector") or {}
    targets = [(w, t) for w, t in workloads.items() if not w.startswith(("CronJob", "Job")) and match(sel, t["metadata"]["labels"])]
    if not targets:
        errors.append(f"Service {n}: selector matches no workload"); continue
    for p in s["spec"].get("ports", []):
        tp = p.get("targetPort", p["port"])
        for w, t in targets:
            if tp not in ports_of(t):
                errors.append(f"Service {n}: targetPort {tp} not declared by {w}")
    svc_ports[n] = {p["port"] for p in s["spec"].get("ports", [])}

for n, ing in by_kind.get("Ingress", {}).items():
    for r in ing["spec"].get("rules", []):
        for p in r["http"]["paths"]:
            sv = p["backend"]["service"]
            if sv["name"] not in svc_ports or sv["port"]["number"] not in svc_ports[sv["name"]]:
                errors.append(f"Ingress {n}: backend {sv['name']}:{sv['port']['number']} not a Service port")
    mw = ing["metadata"].get("annotations", {}).get("traefik.ingress.kubernetes.io/router.middlewares", "")
    for m in filter(None, mw.split(",")):
        mname = m.split("@")[0].removeprefix("numan-")
        if mname not in by_kind.get("Middleware", {}):
            errors.append(f"Ingress {n}: middleware {m} not defined")

for n, h in by_kind.get("HorizontalPodAutoscaler", {}).items():
    ref = h["spec"]["scaleTargetRef"]
    if ref["name"] not in by_kind.get(ref["kind"], {}):
        errors.append(f"HPA {n}: target {ref['kind']}/{ref['name']} missing")

for n, p in by_kind.get("PodDisruptionBudget", {}).items():
    sel = p["spec"]["selector"]["matchLabels"]
    if not any(match(sel, t["metadata"]["labels"]) for t in workloads.values()):
        errors.append(f"PDB {n}: selector matches nothing")

# NetworkPolicy coverage: every uses-* label must have an allow-ingress + allow-egress policy
netpols = by_kind.get("NetworkPolicy", {})
used = set()
for w, t in workloads.items():
    for k, v in t["metadata"]["labels"].items():
        if k.startswith("numan.io/uses-") and v == "true":
            used.add(k.removeprefix("numan.io/uses-"))
for u in used:
    for pol in (f"allow-ingress-to-{u}", f"allow-egress-to-{u}"):
        if pol not in netpols:
            errors.append(f"label uses-{u} has no NetworkPolicy {pol}")
for w, t in workloads.items():
    if t["metadata"]["labels"].get("numan.io/egress-internet") == "true" and "allow-egress-internet-https" not in netpols:
        errors.append(f"{w}: egress-internet label without policy")

# pod anti-pattern guards
for d in rendered:
    if d["kind"] == "ConfigMap" and d["metadata"]["name"] == "numan-config":
        if d["data"].get("CLEAN_DB_ON_STARTUP") != "false":
            errors.append("CLEAN_DB_ON_STARTUP must be 'false'")
        for k in d["data"]:
            if any(x in k for x in ("PASSWORD", "SECRET", "TOKEN", "_KEY")):
                errors.append(f"numan-config contains secret-looking key {k}")


# ---- semantic checks on the rendered values --------------------------------------------------------------------------
def qty(v):
    return parse_quantity(str(v))

def pod_effective(spec, key):
    """Pod effective request/limit: max(sum(containers), max(init containers))."""
    def one(c):
        v = (c.get("resources", {}).get(key, {}) or {})
        return v
    tot = {"cpu": 0, "memory": 0}
    for c in spec["containers"]:
        for r in tot:
            if r in one(c): tot[r] += qty(one(c)[r])
    for c in spec.get("initContainers", []):
        for r in tot:
            if r in one(c): tot[r] = max(tot[r], qty(one(c)[r]))
    return tot

quota = next(iter(by_kind.get("ResourceQuota", {}).values()), None)
hpa_max = {(h["spec"]["scaleTargetRef"]["kind"], h["spec"]["scaleTargetRef"]["name"]): h["spec"]["maxReplicas"] for h in by_kind.get("HorizontalPodAutoscaler", {}).values()}
if quota:
    hard = quota["spec"]["hard"]
    for label, repl in (("initial replicas", lambda d: d["spec"].get("replicas", 1)), ("HPA maximum", lambda d: hpa_max.get((d["kind"], d["metadata"]["name"]), d["spec"].get("replicas", 1)))):
        sums = {"requests.cpu": 0, "requests.memory": 0, "limits.cpu": 0, "limits.memory": 0}
        pods = 0
        for d in rendered:
            if d["kind"] in ("Deployment", "StatefulSet"):
                n = int(repl(d)); spec = d["spec"]["template"]["spec"]; pods += n
                rq, lm = pod_effective(spec, "requests"), pod_effective(spec, "limits")
                sums["requests.cpu"] += n * rq["cpu"]; sums["requests.memory"] += n * rq["memory"]
                sums["limits.cpu"] += n * lm["cpu"]; sums["limits.memory"] += n * lm["memory"]
        for k, used in sums.items():
            if k in hard and used > qty(hard[k]):
                msg = f"ResourceQuota {k}={hard[k]} is smaller than the declared workloads at {label} ({float(used):.3g})"
                if label == "initial replicas": errors.append(msg)
                else: print("WARNING: " + msg + " - the HPA could not reach its maximum")
        if "pods" in hard and pods > int(hard["pods"]):
            msg = f"ResourceQuota pods={hard['pods']} is smaller than the declared pods at {label} ({pods})"
            if label == "initial replicas": errors.append(msg)
            else: print("WARNING: " + msg)
    # StatefulSet volumes vs storage quota
    st = 0
    for d in rendered:
        if d["kind"] == "StatefulSet":
            for t in d["spec"].get("volumeClaimTemplates", []):
                st += qty(t["spec"]["resources"]["requests"]["storage"]) * int(d["spec"].get("replicas", 1) or 0 or 1)
    if "requests.storage" in hard and st > qty(hard["requests.storage"]):
        errors.append("ResourceQuota requests.storage is smaller than the StatefulSet volumes")

# Redis: container memory limit must cover maxmemory (AOF rewrite needs additional headroom)
cm = by_kind.get("ConfigMap", {}).get("numan-config")
rd = by_kind.get("StatefulSet", {}).get("redis")
if cm and rd:
    m = re.match(r"^(\d+)(k|kb|m|mb|g|gb)?$", cm["data"].get("REDIS_MAXMEMORY", "").lower())
    if m:
        mult = {None: 1, "k": 1000, "kb": 1024, "m": 1000**2, "mb": 1024**2, "g": 1000**3, "gb": 1024**3}[m.group(2)]
        maxmem = int(m.group(1)) * mult
        lim = qty(rd["spec"]["template"]["spec"]["containers"][0]["resources"]["limits"]["memory"])
        if lim < maxmem:
            errors.append(f"Redis memory limit ({float(lim)/2**20:.0f}Mi) is below REDIS_MAXMEMORY ({cm['data']['REDIS_MAXMEMORY']}): the container would be OOM-killed")
        elif lim < maxmem * 1.05:
            print("WARNING: Redis memory limit leaves <5% headroom above maxmemory; AOF rewrite (fork + copy-on-write) can exceed it")

# no leftover gateway objects
for kind, items in by_kind.items():
    for n in items:
        if "gateway" in n:
            errors.append(f"{kind}/{n}: obsolete gateway object still rendered")

print(f"checked {len(rendered)} objects, {len(workloads)} pod templates, {len(secrets)} secret templates")
if errors:
    print("\n".join("ERROR: " + e for e in errors)); sys.exit(1)
print("OK: references consistent")
