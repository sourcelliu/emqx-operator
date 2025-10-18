# Deploy EMQX Cluster in k8s with restricted access

Here we are assuming k8s cluster does not have access to the internet, and the user does not have permissions to create and/or use `ClusterRole`. Both `emqx-operator` and `emqx` are installed in the same namespace. Cert-manager might already exist in the cluster, but the operator no longer depends on it. The `emqx-operator` is configured to use a private docker registry, and the `emqx` is configured to use a custom `securityContext`.

## Task Target

- Push necessary images to a private docker registry
- Manually install EMQX Operator CRDs
- Override default parameters of `emqx-operator` to use private registry, single namespace, and custom `securityContext`
- Use custom `securityContext` for EMQX

## Push emqx-operator and emqx-enterprise images to a private docker registry

```bash
export EMQX_OPERATOR_VERSION='2.2.26'
export EMQX_VERSION='5.8.4'
export REGISTRY='my.private.registry'

pull_retag_push() {
    local source=$1
    local target=$2
    docker pull "$source"
    docker tag "$source" "$target"
    docker push "$target"
}

pull_retag_push "emqx/emqx-enterprise:$EMQX_VERSION" "$REGISTRY/emqx/emqx-enterprise:$EMQX_VERSION"
pull_retag_push "emqx/emqx-operator-controller:$EMQX_OPERATOR_VERSION" "$REGISTRY/emqx/emqx-operator-controller:$EMQX_OPERATOR_VERSION"
```

## Deploy EMQX Operator

### Deploy CRDs manually from release assets

```bash
kubectl -n emqx apply -f https://github.com/emqx/emqx-operator/releases/download/$EMQX_OPERATOR_VERSION/crds.yaml
```

### Deploy emqx-operator

In this example `podSecurityContext` and `containerSecurityContext` contain default values, override as necessary.

```bash
helm repo add emqx https://repos.emqx.io/charts
helm repo update
helm upgrade --install emqx-operator emqx/emqx-operator \
  --namespace emqx \
  --create-namespace \
  --set singleNamespace=true \
  --set crds.enabled=false \
  --set-json='podSecurityContext={"runAsNonRoot":true}' \
  --set-json='containerSecurityContext={"allowPrivilegeEscalation":false}' \
  --set image.repository=$REGISTRY/emqx/emqx-operator-controller \
  --set image.tag=$EMQX_OPERATOR_VERSION
```

### Ensure emqx-operator is up and running

```bash
kubectl -n emqx wait --for=condition=Ready pods -l "control-plane=controller-manager"
```

## Configure EMQX Cluster

`apps.emqx.io/v2beta1 EMQX` supports configuring the Core node of the EMQX cluster through the `.spec.coreTemplate` field, and configuring the Replicant node of the EMQX cluster using the `.spec.replicantTemplate` field. For more information, please refer to: [API Reference](../reference/v2beta1-reference.md#emqxspec).

+ Save the following content as a YAML file and deploy it with the `kubectl apply` command

  ```yaml
  apiVersion: apps.emqx.io/v2beta1
  kind: EMQX
  metadata:
    name: emqx
    namespace: emqx
  spec:
    image: ${REGISTRY}/emqx/emqx-enterprise:${EMQX_VERSION}
  ```

+ Wait for the EMQX cluster to be ready, you can check the status of EMQX cluster through `kubectl get` command, please make sure `STATUS` is `Running`, this may take some time

  ```bash
  $ kubectl get emqx emqx
  NAME   IMAGE                                            STATUS    AGE
  emqx   my.private.registry/emqx/emqx-enterprise:5.8.4   Running   10m
  ```
