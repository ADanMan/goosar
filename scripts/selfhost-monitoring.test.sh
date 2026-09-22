#!/usr/bin/env bash
set -euo pipefail

# Tests for #396 — the monitoring overlay, the shipped dashboard and the alert
# rules.
#
#   bash scripts/selfhost-monitoring.test.sh
#
# Phase 1 is hermetic (`docker compose config`, `helm template`, file parsing):
#   * the overlay renders, turns the metrics listener on, and publishes
#     Prometheus / Grafana on 127.0.0.1 only — never the backend metrics port;
#   * the dashboard JSON parses and references only metric names that appear in
#     the SELF_HOSTING.md inventory (which the Go test
#     TestMetricInventoryIsDocumented keeps equal to the registry);
#   * the Compose rules file and the Helm PrometheusRule carry the same alerts.
#
# Phase 2 boots a throwaway stack with the overlay and asserts Prometheus has
# the backend target UP and Grafana serves the provisioned dashboard. Skipped
# when SKIP_DOCKER_RUN=1. Its own COMPOSE_PROJECT_NAME, its own high ports, and
# `down -v` on exit, so it cannot collide with a stack already on the host.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BASE=docker-compose.selfhost.yml
OVERLAY=docker-compose.selfhost.monitoring.yml
DASHBOARD=deploy/helm/goosar/dashboards/goosar-overview.json
ALERTS=deploy/monitoring/prometheus/alerts.yml
RULE=deploy/helm/goosar/templates/prometheusrule.yaml
DOC=SELF_HOSTING.md

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

require() {
  local haystack=$1 needle=$2
  # `--` so a needle that starts with a dash (a `-f compose.yml` line) is not
  # read as a grep option.
  grep -Fq -e "$needle" -- - <<<"$haystack" || fail "missing expected value: $needle"
}

# ---------------------------------------------------------------------------
# Phase 1a — compose rendering and port exposure
# ---------------------------------------------------------------------------

tmp_env="$(mktemp)"
trap 'rm -f "$tmp_env"' EXIT
{
  cat .env.example
  # Both have no compose default on purpose; the example env ships them empty.
  printf '\nJWT_SECRET=ci-synthetic-jwt-secret-for-compose-config-test\n'
  printf 'GRAFANA_ADMIN_PASSWORD=ci-synthetic-grafana-password\n'
} >"$tmp_env"

overlay_config="$(docker compose --env-file "$tmp_env" -f "$BASE" -f "$OVERLAY" config)"

require "$overlay_config" 'METRICS_ADDR: 0.0.0.0:9090'

# .env.example documents METRICS_ADDR=127.0.0.1:9090 for the bare binary. If
# the overlay let that through, the whole stack would come up scraping nothing.
override_env="$(mktemp)"
{ cat "$tmp_env"; printf '\nMETRICS_ADDR=127.0.0.1:9090\n'; } >"$override_env"
override_config="$(docker compose --env-file "$override_env" -f "$BASE" -f "$OVERLAY" config)"
rm -f "$override_env"
require "$override_config" 'METRICS_ADDR: 0.0.0.0:9090'
require "$overlay_config" 'prom/prometheus:'
require "$overlay_config" 'grafana/grafana:'

# Digest pins, like the pgvector image in the base file.
for svc in prom/prometheus grafana/grafana; do
  grep -Eq "image: $svc:[^@]+@sha256:[0-9a-f]{64}" <<<"$overlay_config" \
    || fail "$svc is not pinned by digest in $OVERLAY"
done

# The overlay must not start the metrics listener without also being able to
# refuse an empty Grafana password.
#
# Real temp files, not `--env-file <(...)`: docker compose reads the path more
# than once and a process substitution is empty on the second read, so every
# such invocation fails regardless of its content — a negative test written
# that way passes vacuously.
no_grafana_env="$(mktemp)"
grep -v '^GRAFANA_ADMIN_PASSWORD=' "$tmp_env" >"$no_grafana_env"
if docker compose --env-file "$no_grafana_env" \
     -f "$BASE" -f "$OVERLAY" config >/dev/null 2>&1; then
  rm -f "$no_grafana_env"
  fail "$OVERLAY renders with an empty GRAFANA_ADMIN_PASSWORD"
fi
rm -f "$no_grafana_env"

# Every published port must be loopback-bound, and the backend metrics port
# must not be published at all.
docker compose --env-file "$tmp_env" -f "$BASE" -f "$OVERLAY" config --format json |
  python3 -c '
import json, sys

cfg = json.load(sys.stdin)
problems = []
for name, svc in cfg["services"].items():
    for port in svc.get("ports", []) or []:
        host_ip = port.get("host_ip", "")
        if host_ip not in ("127.0.0.1", "::1"):
            problems.append(name + " publishes " + str(port.get("published")) + " on " + (host_ip or "0.0.0.0"))
        if name == "backend" and str(port.get("target")) == "9090":
            problems.append("backend publishes its metrics port to the host")
if problems:
    sys.exit("compose port exposure: " + "; ".join(problems))
' || exit 1

echo "ok   compose overlay renders, digest-pinned, loopback-only"

# ---------------------------------------------------------------------------
# Phase 1b — dashboard JSON parses and uses only documented metric names
# ---------------------------------------------------------------------------

python3 - "$DASHBOARD" "$DOC" <<'PY' || exit 1
import json, re, sys

dashboard_path, doc_path = sys.argv[1], sys.argv[2]

with open(dashboard_path, encoding="utf-8") as f:
    dashboard = json.load(f)

if not dashboard.get("panels"):
    sys.exit("dashboard has no panels")
if not dashboard.get("uid") or not dashboard.get("title"):
    sys.exit("dashboard needs a uid and a title for provisioning")

doc = open(doc_path, encoding="utf-8").read()
begin = doc.index("<!-- goosar-metric-inventory:begin -->")
end = doc.index("<!-- goosar-metric-inventory:end -->")
inventory = set(re.findall(r"^\| `(goosar_[a-z0-9_]+)` \|", doc[begin:end], re.M))
if not inventory:
    sys.exit("no metric inventory rows found in " + doc_path)

referenced = set()
for panel in dashboard["panels"]:
    for target in panel.get("targets", []) or []:
        referenced.update(re.findall(r"\bgoosar_[a-z0-9_]+\b", target.get("expr", "")))

unknown = []
for name in sorted(referenced):
    if name in inventory:
        continue
    # Histogram/summary series expose derived names the registry never
    # declares; accept them only when the base metric is documented.
    base = re.sub(r"_(bucket|sum|count)$", "", name)
    if base != name and base in inventory:
        continue
    unknown.append(name)

if unknown:
    sys.exit("dashboard references metrics that are not in the inventory: " + ", ".join(unknown))

print(f"ok   dashboard parses, {len(dashboard['panels'])} panels, {len(referenced)} metric references all documented")
PY

# ---------------------------------------------------------------------------
# Phase 1c — alert rules: same names on both delivery paths, documented metrics
# ---------------------------------------------------------------------------

compose_alerts="$(grep -oE '^\s+- alert: [A-Za-z]+' "$ALERTS" | awk '{print $3}' | sort)"
helm_alerts="$(grep -oE '^\s+- alert: [A-Za-z]+' "$RULE" | awk '{print $3}' | sort)"
if [ "$compose_alerts" != "$helm_alerts" ]; then
  echo "Alert names differ between $ALERTS and $RULE:" >&2
  diff <(echo "$compose_alerts") <(echo "$helm_alerts") >&2 || true
  exit 1
fi
[ -n "$compose_alerts" ] || fail "no alerts found in $ALERTS"

# Every acceptance-criteria alert must exist by name.
for alert in GoosarBackendDown GoosarNotReady GoosarMigrationsOutOfDate \
             GoosarNoOnlineRuntime GoosarTaskFailureRateHigh \
             GoosarDBPoolSaturated GoosarHTTPErrorRateHigh GoosarDiskSpaceLow; do
  grep -Fq "$alert" <<<"$compose_alerts" || fail "alert $alert is missing"
done

python3 - "$ALERTS" "$RULE" "$DOC" <<'PY' || exit 1
import re, sys

doc = open(sys.argv[3], encoding="utf-8").read()
begin = doc.index("<!-- goosar-metric-inventory:begin -->")
end = doc.index("<!-- goosar-metric-inventory:end -->")
inventory = set(re.findall(r"^\| `(goosar_[a-z0-9_]+)` \|", doc[begin:end], re.M))

unknown = set()
for path in sys.argv[1:3]:
    for name in re.findall(r"\bgoosar_[a-z0-9_]+\b", open(path, encoding="utf-8").read()):
        if name in inventory:
            continue
        base = re.sub(r"_(bucket|sum|count)$", "", name)
        if base != name and base in inventory:
            continue
        unknown.add(name)

if unknown:
    sys.exit("alert rules reference metrics that are not in the inventory: " + ", ".join(sorted(unknown)))
print("ok   alert rules match between compose and Helm, and use documented metrics only")
PY

# ---------------------------------------------------------------------------
# Phase 1d — the docs carry the exact commands an operator has to type
# ---------------------------------------------------------------------------

doc_text="$(cat "$DOC")"
require "$doc_text" 'docker compose -f docker-compose.selfhost.yml \'
require "$doc_text" '-f docker-compose.selfhost.monitoring.yml up -d'
require "$doc_text" 'GRAFANA_ADMIN_PASSWORD'
grep -Fq "$OVERLAY" .github/workflows/ci.yml \
  || echo "note: $OVERLAY is not in the CI path filter" >&2

echo "ok   SELF_HOSTING.md documents the overlay command"

if [ "${SKIP_DOCKER_RUN:-0}" = "1" ]; then
  echo "SKIP_DOCKER_RUN=1 — skipping the live monitoring stack."
  exit 0
fi

# ---------------------------------------------------------------------------
# Phase 2 — live throwaway stack
# ---------------------------------------------------------------------------

PROJECT="goosar-mon-$(date +%s)-$$"
live_env="$(mktemp)"
{
  cat "$tmp_env"
  # High, unlikely-to-collide ports. Nothing here reuses an operator port.
  printf '\nBACKEND_PORT=18396\nFRONTEND_PORT=13396\nPROMETHEUS_PORT=19396\nGRAFANA_PORT=15396\n'
  printf 'POSTGRES_PASSWORD=ci-throwaway-postgres-password\n'
} >"$live_env"

compose_live() {
  COMPOSE_PROJECT_NAME="$PROJECT" docker compose --env-file "$live_env" \
    -f "$BASE" -f "$OVERLAY" "$@"
}

live_cleanup() {
  compose_live down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$tmp_env" "$live_env"
}
trap live_cleanup EXIT

echo "==> booting throwaway monitoring stack ($PROJECT)"
compose_live up -d postgres backend prometheus grafana

wait_for() {
  local what=$1 url=$2 tries=${3:-60}
  for _ in $(seq "$tries"); do
    if curl -fsS --max-time 3 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  fail "$what never became reachable at $url"
}

wait_for "Prometheus" "http://127.0.0.1:19396/-/ready"
wait_for "Grafana" "http://127.0.0.1:15396/api/health"

# The backend target must be UP — this is the whole point of the issue.
# A plain `for ... done || fail` cannot work here: the loop exits with the
# status of its last command (`sleep`), so the failure branch would never run.
target_up=0
for _ in $(seq 60); do
  if curl -fsS --max-time 5 'http://127.0.0.1:19396/api/v1/targets?state=active' |
       python3 -c '
import json, sys
data = json.load(sys.stdin)["data"]["activeTargets"]
sys.exit(0 if any(t["labels"]["job"] == "goosar-backend" and t["health"] == "up" for t in data) else 1)
'; then
    target_up=1
    echo "ok   Prometheus scrapes goosar-backend"
    break
  fi
  sleep 2
done
[ "$target_up" = "1" ] || fail "Prometheus never reported the goosar-backend target as up"

# A metric only this backend produces must actually have arrived.
# goosar_build_info rather than goosar_ready: the live phase runs against the
# published image, which may predate a newly added metric.
curl -fsS --max-time 5 'http://127.0.0.1:19396/api/v1/query?query=goosar_build_info' |
  python3 -c '
import json, sys
result = json.load(sys.stdin)["data"]["result"]
sys.exit(0 if result else "goosar_build_info never reached Prometheus")
' || exit 1
echo "ok   backend metrics are in Prometheus"

# Grafana must have provisioned both the datasource and the dashboard.
GRAFANA_AUTH="admin:ci-synthetic-grafana-password"
curl -fsS --max-time 5 -u "$GRAFANA_AUTH" \
  'http://127.0.0.1:15396/api/dashboards/uid/goosar-overview' |
  python3 -c '
import json, sys
d = json.load(sys.stdin)
title = d.get("dashboard", {}).get("title")
sys.exit(0 if title else "Grafana did not provision the goosar-overview dashboard")
' || exit 1
echo "ok   Grafana serves the provisioned dashboard"

echo "all selfhost-monitoring tests passed"
