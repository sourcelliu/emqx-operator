#!/usr/bin/env bash
set -euo pipefail

HELM_BIN="${HELM_BIN:-helm}"
CHART_DIR="$(dirname "$(readlink -f "$0")")/../deploy/charts/emqx-operator"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

# Ensure helm is available.
if ! command -v "$HELM_BIN" >/dev/null 2>&1; then
  echo "error: helm binary not found (set HELM_BIN to override)" >&2
  exit 1
fi

# Basic smoke test for control-plane release rendering.
"$HELM_BIN" template control-plane "$CHART_DIR" \
  >"$WORKDIR/control-plane.yaml"

if ! grep -q "kind: CustomResourceDefinition" "$WORKDIR/control-plane.yaml"; then
  echo "error: control-plane template does not contain CRDs" >&2
  exit 1
fi

# Webhook resources should no longer be rendered.
if grep -q "WebhookConfiguration" "$WORKDIR/control-plane.yaml"; then
  echo "error: control-plane template unexpectedly contains webhook configuration" >&2
  exit 1
fi

if grep -q "containerPort: 9443" "$WORKDIR/control-plane.yaml"; then
  echo "error: control-plane template exposes deprecated webhook port 9443" >&2
  exit 1
fi

if grep -q "ENABLE_WEBHOOKS" "$WORKDIR/control-plane.yaml"; then
  echo "error: control-plane deployment still references ENABLE_WEBHOOKS env" >&2
  exit 1
fi

# Secondary release should not render cluster-scoped resources by default.
"$HELM_BIN" template workload "$CHART_DIR" \
  --set controlPlane=false \
  >"$WORKDIR/workload.yaml"

if grep -q "kind: CustomResourceDefinition" "$WORKDIR/workload.yaml"; then
  echo "error: workload template contains CRDs but controlPlane=false" >&2
  exit 1
fi

if grep -q "containerPort: 9443" "$WORKDIR/workload.yaml"; then
  echo "error: workload deployment exposes webhook port 9443 unexpectedly" >&2
  exit 1
fi

if grep -q "ENABLE_WEBHOOKS" "$WORKDIR/workload.yaml"; then
  echo "error: workload deployment still references ENABLE_WEBHOOKS env" >&2
  exit 1
fi

echo "Helm multi-instance smoke test PASSED"
