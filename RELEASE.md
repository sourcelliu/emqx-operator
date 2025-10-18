# Release Note 🍻

EMQX Operator 2.2.30 has been released.

## Supported version
+ apps.emqx.io/v2beta1

  + EMQX at 5.1.1 and later
  + EMQX Enterprise at 5.1.1 and later

+ apps.emqx.io/v1beta4

  + EMQX at 4.4.14 and later
  + EMQX Enterprise at 4.4.14 and later

## Fix 🐞

+ Fix the issue that the replicas of the statefulSet is not current when the `emqx` CR is updated

+ Correct TopologySpreadConstraints reference in generateReplicaSet function

## Chore 🏗

+  Helm chart

   + Add `controlPlane` switch to support multi-release Helm deployments

   + Remove admission webhooks; manifests and controller no longer require TLS secrets


## How to install/upgrade EMQX Operator 💡

```
helm repo add emqx https://repos.emqx.io/charts
helm repo update
helm upgrade --install emqx-operator emqx/emqx-operator \
  --namespace emqx-operator-system \
  --create-namespace \
  --version 2.2.30
kubectl wait --for=condition=Ready pods -l "control-plane=controller-manager" -n emqx-operator-system
```

## Warning 🚨
`apps.emqx.io/v1beta3` and `apps.emqx.io/v2alpha1` will be dropped soon
