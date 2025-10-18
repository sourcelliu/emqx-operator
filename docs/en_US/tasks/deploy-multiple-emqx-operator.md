# Deploy Multiple EMQX Operator Releases

This guide explains how to run more than one `emqx-operator` release in the same Kubernetes cluster.
Use this pattern when you want to separate a **control-plane** instance that manages cluster-scoped
resources (for example CRDs) from one or more workload-focused instances that operate in their own
namespaces.

## Prerequisites

- `helm` v3.8 or later.
- A Kubernetes cluster with `kubectl` access.

## Step 1 — Install the Control-Plane Release

Install the first release with `controlPlane=true` (default). It is responsible for installing CRDs
and other shared cluster-scoped resources.

```bash
helm upgrade --install emqx-operator-cp emqx/emqx-operator \
  --namespace emqx-operator-system \
  --create-namespace

# Wait until the controller manager is ready
kubectl wait --for=condition=Ready pods -l "control-plane=controller-manager" -n emqx-operator-system
```

## Step 2 — Install a Workload Release

Workload releases reuse the CRDs created by the control-plane instance. Disable the control-plane
capabilities:

```bash
helm upgrade --install emqx-operator-workload emqx/emqx-operator \
  --namespace emqx-operator-workload \
  --create-namespace \
  --set controlPlane=false
```

You can repeat this step to deploy additional workload releases in other namespaces.

## Step 3 — Verify the Installation

List Helm releases and check that the control-plane and workload instances are both healthy:

```bash
helm list -A | grep emqx-operator
kubectl get pods -n emqx-operator-system
kubectl get pods -n emqx-operator-workload
```

If you need a quick manifest-level check without touching a cluster, run the repository helper:

```bash
make helm-multi-instance-test
```

This renders both configurations and ensures CRDs only appear in the control-plane output.

For an end-to-end validation against a real Kubernetes cluster, provision a temporary
[kind](https://kind.sigs.k8s.io/) cluster:

```bash
make helm-multi-instance-kind
```

The script creates a kind cluster, installs both Helm releases, and confirms only the control-plane
release owns cluster-scoped resources. All resources are removed after the test.

## Upgrades

1. Upgrade the control-plane release first:
   ```bash
   helm upgrade emqx-operator-cp emqx/emqx-operator \
     --namespace emqx-operator-system
   ```
2. Upgrade each workload release afterwards, preserving `controlPlane=false`.

## Uninstall

1. Remove workload releases:
   ```bash
   helm uninstall emqx-operator-workload -n emqx-operator-workload
   ```
2. Remove the control-plane release last so CRDs remain available during the cleanup of other releases:
   ```bash
   helm uninstall emqx-operator-cp -n emqx-operator-system
   ```

Manually delete CRDs only if no other EMQX workloads require them.
