#!/usr/bin/env bash
set -euo pipefail

KIND_BIN="${KIND_BIN:-kind}"
KUBECTL_BIN="${KUBECTL_BIN:-kubectl}"
HELM_BIN="${HELM_BIN:-helm}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v1.34.0}"
CLUSTER_NAME="${KIND_CLUSTER_NAME:-emqx-operator-multi}"
CHART_DIR="$(dirname "$(readlink -f "$0")")/../deploy/charts/emqx-operator"
CONTROL_NAMESPACE="${CONTROL_NAMESPACE:-emqx-operator-system}"
WORKLOAD_NAMESPACE="${WORKLOAD_NAMESPACE:-emqx-operator-workload}"
CONTROL_RELEASE="${CONTROL_RELEASE:-control-plane}"
WORKLOAD_RELEASE="${WORKLOAD_RELEASE:-workload}"

cleanup() {
  set +e
  "$HELM_BIN" uninstall "$WORKLOAD_RELEASE" -n "$WORKLOAD_NAMESPACE" >/dev/null 2>&1
  "$HELM_BIN" uninstall "$CONTROL_RELEASE" -n "$CONTROL_NAMESPACE" >/dev/null 2>&1
  "$KUBECTL_BIN" delete namespace "$WORKLOAD_NAMESPACE" --wait=false >/dev/null 2>&1
  "$KUBECTL_BIN" delete namespace "$CONTROL_NAMESPACE" --wait=false >/dev/null 2>&1
  "$KIND_BIN" delete cluster --name "$CLUSTER_NAME" >/dev/null 2>&1
}
trap cleanup EXIT

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command '$1' not found in PATH" >&2
    exit 1
  fi
}

require_cmd "$KIND_BIN"
require_cmd "$KUBECTL_BIN"
require_cmd "$HELM_BIN"
echo ">>> Creating kind cluster '$CLUSTER_NAME'"
"$KIND_BIN" create cluster --name "$CLUSTER_NAME" --image "$KIND_NODE_IMAGE"

echo ">>> Creating namespaces"
"$KUBECTL_BIN" create namespace "$CONTROL_NAMESPACE"
"$KUBECTL_BIN" create namespace "$WORKLOAD_NAMESPACE"

echo ">>> Installing control-plane Helm release"
"$HELM_BIN" upgrade --install "$CONTROL_RELEASE" "$CHART_DIR" \
  --namespace "$CONTROL_NAMESPACE" \
  --create-namespace

for crd in emqxbrokers.apps.emqx.io emqxenterprises.apps.emqx.io emqxes.apps.emqx.io emqxplugins.apps.emqx.io rebalances.apps.emqx.io; do
  "$KUBECTL_BIN" get crd "$crd" >/dev/null
done

if "$KUBECTL_BIN" get validatingwebhookconfiguration --no-headers 2>/dev/null | grep -q emqx-operator; then
  echo "error: control-plane installation created unexpected validating webhooks" >&2
  exit 1
fi
if "$KUBECTL_BIN" get mutatingwebhookconfiguration --no-headers 2>/dev/null | grep -q emqx-operator; then
  echo "error: control-plane installation created unexpected mutating webhooks" >&2
  exit 1
fi

echo ">>> Installing workload Helm release"
"$HELM_BIN" upgrade --install "$WORKLOAD_RELEASE" "$CHART_DIR" \
  --namespace "$WORKLOAD_NAMESPACE" \
  --create-namespace \
  --set controlPlane=false

echo ">>> Validating workload release isolation"
if "$KUBECTL_BIN" get validatingwebhookconfiguration "${WORKLOAD_RELEASE}-emqx-operator-validating-webhook-configuration" >/dev/null 2>&1; then
  echo "error: workload release created its own ValidatingWebhookConfiguration" >&2
  exit 1
fi
if "$KUBECTL_BIN" get mutatingwebhookconfiguration "${WORKLOAD_RELEASE}-emqx-operator-mutating-webhook-configuration" >/dev/null 2>&1; then
  echo "error: workload release created its own MutatingWebhookConfiguration" >&2
  exit 1
fi

echo ">>> E2E multi-instance Helm verification PASSED"
