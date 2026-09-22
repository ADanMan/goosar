#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="$ROOT_DIR/deploy/helm/goosar"

require_rendered_value() {
  local rendered=$1
  local expected=$2

  if ! grep -Fq "$expected" <<<"$rendered"; then
    echo "Missing expected Helm-rendered config value:"
    echo "  $expected"
    exit 1
  fi
}

# pod_volume_names <rendered> — the names under .spec.template.spec.volumes[].
# grep alone cannot tell a volume from the volumeMount that references it: both
# render `name: next-cache`, and a mount without its volume is exactly the
# combination the API server rejects at install time. So walk the volumes block
# by indentation instead (no yq on the release machine).
pod_volume_names() {
  awk '
    /^      volumes:[[:space:]]*$/ { in_volumes = 1; next }
    in_volumes && /^        - name: / { print $NF; next }
    in_volumes && /^          / { next }
    in_volumes { in_volumes = 0 }
  ' <<<"$1"
}

require_pod_volume() {
  local rendered=$1
  local name=$2

  if ! pod_volume_names "$rendered" | grep -Fxq "$name"; then
    echo "Missing pod volume in .spec.template.spec.volumes[]:"
    echo "  $name"
    echo "  (a volumeMount without its volume renders and lints fine, then fails at install)"
    exit 1
  fi
}

helm lint "$CHART_DIR"

default_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml
)"
require_rendered_value "$default_config" 'GOOSAR_VCS_INTEGRATION_ENABLED: "true"'

disabled_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.vcsIntegrationEnabled=false
)"
require_rendered_value "$disabled_config" 'GOOSAR_VCS_INTEGRATION_ENABLED: "false"'


# #231: the provisioning store variables must render into the backend
# ConfigMap — a values entry that never reaches env is the exact failure
# mode that shipped the compose gap.
require_rendered_value "$default_config" 'GOOSAR_PROVISIONING_STORE: ""'

store_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.provisioningStore=local \
    --set backend.config.provisioningLocalPrefix=provisioning
)"
require_rendered_value "$store_config" 'GOOSAR_PROVISIONING_STORE: "local"'
require_rendered_value "$store_config" 'GOOSAR_PROVISIONING_LOCAL_PREFIX: "provisioning"'

# #213: the deployment-admin seed list must render into the backend
# ConfigMap the same way the provisioning variables do (#231 lesson).
require_rendered_value "$default_config" 'GOOSAR_DEPLOYMENT_ADMIN_EMAILS: ""'

admins_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.deploymentAdminEmails=ops@corp.example
)"
require_rendered_value "$admins_config" 'GOOSAR_DEPLOYMENT_ADMIN_EMAILS: "ops@corp.example"'

# #484: the role-workspace switch must render into the backend ConfigMap
# too, or a chart install cannot turn provisioning off.
require_rendered_value "$default_config" 'GOOSAR_ROLE_WORKSPACES: ""'

roles_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.roleWorkspaces=off
)"
require_rendered_value "$roles_config" 'GOOSAR_ROLE_WORKSPACES: "off"'

# #530: the ADR-0015 stand type and the `dev` profile's relaxed limits must
# render too. Compose forwards both; a chart install that cannot express them
# makes SELF_HOSTING.md's profile table false on Kubernetes.
require_rendered_value "$default_config" 'GOOSAR_DEPLOYMENT_PROFILE: ""'
require_rendered_value "$default_config" 'RATE_LIMIT_API: ""'
require_rendered_value "$default_config" 'RATE_LIMIT_AUTH: ""'

profile_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.deploymentProfile=dev \
    --set backend.config.rateLimitApi=6000 \
    --set backend.config.rateLimitAuth=100
)"
require_rendered_value "$profile_config" 'GOOSAR_DEPLOYMENT_PROFILE: "dev"'
require_rendered_value "$profile_config" 'RATE_LIMIT_API: "6000"'
require_rendered_value "$profile_config" 'RATE_LIMIT_AUTH: "100"'

# #428: the goosar-secrets example must carry the encryption keys. A Helm
# install whose Secret lacks GOOSAR_MCP_SECRET_KEY stores every integration
# credential as plaintext, and refuses to start on the perimeter profile.
for key in GOOSAR_MCP_SECRET_KEY GOOSAR_VCS_SECRET_KEY; do
  if ! grep -Fq "$key" "$CHART_DIR/values.yaml"; then
    echo "values.yaml goosar-secrets example does not include $key"
    exit 1
  fi
done

# The documented `kubectl cp` catalog-loading recipe (SELF_HOSTING.md) targets this
# exact mount path inside the backend pod; a chart change that moves it would
# silently break the recipe.
backend_rendered="$(helm template goosar "$CHART_DIR" --show-only templates/backend.yaml)"
require_rendered_value "$backend_rendered" 'mountPath: /app/data/uploads'
if ! grep -Fq '/app/data/uploads' "$ROOT_DIR/SELF_HOSTING.md"; then
  echo "SELF_HOSTING.md no longer documents the uploads mount path used by the catalog recipe"
  exit 1
fi

# #434: a chart deployment must be able to pick the log format and level, and
# json is the recommended production default (the cluster rotates the files).
require_rendered_value "$default_config" 'LOG_FORMAT: "json"'
require_rendered_value "$default_config" 'LOG_LEVEL: ""'

log_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set backend.config.logFormat=text \
    --set backend.config.logLevel=warn
)"
require_rendered_value "$log_config" 'LOG_FORMAT: "text"'
require_rendered_value "$log_config" 'LOG_LEVEL: "warn"'

# #396: monitoring is off by default and must render without the Prometheus
# Operator CRDs; with the switches on it must produce a ServiceMonitor, a
# PrometheusRule and a dashboard ConfigMap, and it must never route the metrics
# port through the Ingress.
require_rendered_value "$default_config" 'METRICS_ADDR: ""'
default_all="$(helm template goosar "$CHART_DIR")"
for kind in "kind: ServiceMonitor" "kind: PrometheusRule" "grafana_dashboard" "prometheus.io/scrape"; do
  if grep -Fq "$kind" <<<"$default_all"; then
    echo "monitoring must be off by default, but the chart rendered: $kind"
    exit 1
  fi
done

monitoring_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set monitoring.enabled=true
)"
require_rendered_value "$monitoring_config" 'METRICS_ADDR: "0.0.0.0:9090"'

monitoring_all="$(
  helm template goosar "$CHART_DIR" \
    --set monitoring.enabled=true \
    --set monitoring.serviceMonitor.enabled=true \
    --set monitoring.prometheusRule.enabled=true \
    --set monitoring.dashboard.enabled=true
)"
require_rendered_value "$monitoring_all" 'kind: ServiceMonitor'
require_rendered_value "$monitoring_all" 'kind: PrometheusRule'
require_rendered_value "$monitoring_all" 'apiVersion: monitoring.coreos.com/v1'
require_rendered_value "$monitoring_all" 'grafana_dashboard: "1"'
require_rendered_value "$monitoring_all" 'prometheus.io/scrape: "true"'
# The ServiceMonitor selects the port by name, so the Service must expose it.
require_rendered_value "$monitoring_all" 'name: metrics'
require_rendered_value "$monitoring_all" 'targetPort: metrics'
# Every acceptance-criteria alert must be present.
for alert in GoosarBackendDown GoosarNotReady GoosarMigrationsOutOfDate \
             GoosarNoOnlineRuntime GoosarTaskFailureRateHigh GoosarDBPoolSaturated; do
  require_rendered_value "$monitoring_all" "alert: $alert"
done
# The dashboard ConfigMap must carry the real JSON, not an empty .Files.Get.
require_rendered_value "$monitoring_all" '"uid": "goosar-overview"'

ingress_rendered="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/ingress.yaml \
    --set monitoring.enabled=true
)"
if grep -Fq "9090" <<<"$ingress_rendered"; then
  echo "the Ingress must never route the metrics port"
  exit 1
fi

# Both switches must also lint with monitoring on, not only with the defaults.
helm lint "$CHART_DIR" \
  --set monitoring.enabled=true \
  --set monitoring.serviceMonitor.enabled=true \
  --set monitoring.prometheusRule.enabled=true \
  --set monitoring.dashboard.enabled=true

# --- #397: multi-replica topology -------------------------------------------
require_absent() {
  local rendered=$1
  local unexpected=$2
  if grep -Fq "$unexpected" <<<"$rendered"; then
    echo "Unexpected Helm-rendered value: $unexpected"
    exit 1
  fi
}

single_backend="$(helm template goosar "$CHART_DIR" --show-only templates/backend.yaml)"
# One replica keeps Recreate (the uploads PVC is ReadWriteOnce) and must NOT
# get a PodDisruptionBudget — an unsatisfiable PDB blocks every node drain.
require_rendered_value "$single_backend" "type: Recreate"
require_rendered_value "$single_backend" 'name: GOOSAR_REPLICAS'
require_absent "$single_backend" "kind: PodDisruptionBudget"
require_absent "$single_backend" "podAntiAffinity"
# Liveness must not be readiness: a DB blip must not restart every pod.
require_rendered_value "$single_backend" "path: /health"
require_rendered_value "$single_backend" "path: /readyz"

ha_backend="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/backend.yaml \
    --set backend.replicas=3
)"
require_rendered_value "$ha_backend" "replicas: 3"
require_rendered_value "$ha_backend" "type: RollingUpdate"
require_rendered_value "$ha_backend" "maxUnavailable: 0"
require_rendered_value "$ha_backend" "kind: PodDisruptionBudget"
require_rendered_value "$ha_backend" "minAvailable: 1"
require_rendered_value "$ha_backend" "podAntiAffinity"
require_absent "$ha_backend" "type: Recreate"

# An explicit backend.affinity must win over the chart's default spread rule.
custom_affinity="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/backend.yaml \
    --set backend.replicas=3 \
    --set backend.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key=kubernetes.io/os \
    --set backend.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator=In \
    --set backend.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].values[0]=linux
)"
require_absent "$custom_affinity" "podAntiAffinity"

# Redis is what makes replicas > 1 safe; it must reach the backend as env.
redis_config="$(
  helm template goosar "$CHART_DIR" \
    --show-only templates/configmap.yaml \
    --set redis.url=redis://goosar-redis:6379/0
)"
require_rendered_value "$redis_config" 'REDIS_URL: "redis://goosar-redis:6379/0"'
require_rendered_value "$default_config" 'REDIS_URL: ""'

helm lint "$CHART_DIR" --set backend.replicas=3 --set redis.url=redis://goosar-redis:6379/0

# #520 (trivy KSV-0014 / KSV-0118): every workload in the chart runs with a
# read-only root filesystem and an explicit container securityContext. The
# frontend needs writable /tmp and a Next.js cache dir, postgres needs /tmp and
# its unix-socket directory — without those emptyDirs the flag is a crash loop,
# so assert the mounts too, not just the flag.
frontend_workload="$(
  helm template goosar "$CHART_DIR" --show-only templates/frontend.yaml
)"
require_rendered_value "$frontend_workload" "readOnlyRootFilesystem: true"
require_rendered_value "$frontend_workload" "mountPath: /tmp"
require_rendered_value "$frontend_workload" "mountPath: /app/apps/web/.next/cache"
# A volumeMount without its volume renders fine and lints fine, and is then
# rejected by the API server at install time — assert the emptyDirs exist as
# pod volumes, not merely as some `name:` somewhere in the manifest.
require_pod_volume "$frontend_workload" "next-cache"
require_pod_volume "$frontend_workload" "tmp"
require_rendered_value "$frontend_workload" "emptyDir: {}"

postgres_workload="$(
  helm template goosar "$CHART_DIR" --show-only templates/postgres.yaml
)"
require_rendered_value "$postgres_workload" "readOnlyRootFilesystem: true"
require_rendered_value "$postgres_workload" "allowPrivilegeEscalation: false"
require_rendered_value "$postgres_workload" "runAsNonRoot: true"
require_rendered_value "$postgres_workload" "mountPath: /var/run/postgresql"
require_rendered_value "$postgres_workload" "mountPath: /tmp"
require_pod_volume "$postgres_workload" "run"
require_pod_volume "$postgres_workload" "tmp"
require_rendered_value "$postgres_workload" "emptyDir: {}"

echo "helm config rendering ok"
