# 快速开始

在本文中，我们将指导您完成高效设置 EMQX Operator 环境、安装 EMQX Operator，然后使用它部署 EMQX 所需的步骤。通过遵循本节中概述的指南，您将能够使用 EMQX Operator 有效地安装和管理 EMQX。

## 准备环境

在部署 EMQX Operator 之前，请确认以下组件已经准备就绪：

- 一个正在运行的 [Kubernetes 集群](https://kubernetes.io/docs/concepts/overview/)，关于 Kubernetes 的版本，请查看[如何选择 Kubernetes 版本](../index.md)

- 一个可以访问 Kubernetes 集群的 [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) 工具。您可以使用 `kubectl cluster-info` 命令检查 Kubernetes 集群的状态。

- [Helm](https://helm.sh) 3 或更高

## 安装 EMQX Operator

::: tip
从 EMQX Operator 2.2.29 版本开始，准入 Webhook 已经移除，Operator 不再依赖 cert-manager。仅当您的集群需要为其他工作负载提供证书管理时，才需要安装 cert-manager。
:::

1. 运行以下命令来安装 EMQX Operator。

   ```bash
   $ helm repo add emqx https://repos.emqx.io/charts
   $ helm repo update
   $ helm upgrade --install emqx-operator emqx/emqx-operator \
     --namespace emqx-operator-system \
     --create-namespace
   ```

2. 等待 EMQX Operator 就绪。

   ```bash
   $ kubectl wait --for=condition=Ready pods -l "control-plane=controller-manager" -n emqx-operator-system

   pod/emqx-operator-controller-manager-57bd7b8bd4-h2mcr condition met
   ```

现在你已经成功的安装 EMQX Operator，你可以继续下一步了。在部署 EMQX 部分中，您将学习如何使用 EMQX Operator 来部署 EMQX。

## 部署 EMQX

:::: tabs type:card

::: tab EMQX Enterprise 5

1. 将下面的 YAML 配置文件保存为 `emqx.yaml`。

   ```yaml
   apiVersion: apps.emqx.io/v2beta1
   kind: EMQX
   metadata:
      name: emqx-ee
   spec:
      image: emqx/emqx-enterprise:5.8
   ```

   并使用 `kubectl apply` 命令来部署 EMQX。

   ```bash
   $ kubectl apply -f emqx.yaml
   ```

   关于 EMQX 自定义资源的更多信息，请查看 [API 参考](../reference/v2beta1-reference.md)

2. 检查 EMQX 集群状态，请确保 STATUS 为 Running，这可能需要一些时间等待 EMQX 集群准备就绪。

   ```bash
   $ kubectl get emqx

   NAME      IMAGE                        STATUS    AGE
   emqx-ee   emqx/emqx-enterprise:5.8.6   Running   2m55s
   ```
:::

::: tab EMQX Open Source 5

1. 将下面的 YAML 配置文件保存为 `emqx.yaml`。

   ```yaml
   apiVersion: apps.emqx.io/v2beta1
   kind: EMQX
   metadata:
      name: emqx
   spec:
      image: emqx/emqx:latest
   ```

   并使用 `kubectl apply` 命令来部署 EMQX。

   ```bash
   $ kubectl apply -f emqx.yaml
   ```

   关于 EMQX 自定义资源的更多信息，请查看 [API 参考](../reference/v2beta1-reference.md)

2. 检查 EMQX 集群状态，请确保 STATUS 为 Running，这可能需要一些时间等待 EMQX 集群准备就绪。

   ```bash
   $ kubectl get emqx

   NAME   IMAGE              STATUS    AGE
   emqx   emqx/emqx:latest   Running   2m55s
   ```
:::

::: tab EMQX Enterprise 4
1. 将下面的 YAML 配置文件保存为 `emqx.yaml`。

   ```yaml
   apiVersion: apps.emqx.io/v1beta4
   kind: EmqxEnterprise
   metadata:
      name: emqx-ee
   spec:
      template:
        spec:
          emqxContainer:
            image:
              repository: emqx/emqx-ee
              version: 4.4.19
   ```

   并使用 `kubectl apply` 命令来部署 EMQX。

   ```bash
   $ kubectl apply -f emqx.yaml
   ```

   关于 EMQX 自定义资源的更多信息，请查看 [API 参考](../reference/v1beta4-reference.md)

2. 等待 EMQX 集群就绪。

   ```bash
   $ kubectl get emqxenterprises

   NAME      STATUS   AGE
   emqx-ee   Running  8m33s
   ```

  请确保 `STATUS` 为 `Running`，这可能需要一些时间等待 EMQX 集群准备就绪。
:::

::: tab EMQX Open Source 4
1. 将下面的 YAML 配置文件保存为 `emqx.yaml`。

   ```yaml
   apiVersion: apps.emqx.io/v1beta4
   kind: EmqxBroker
   metadata:
      name: emqx
   spec:
      template:
        spec:
          emqxContainer:
            image:
              repository: emqx
              version: 4.4.19
   ```

   并使用 `kubectl apply` 命令来部署 EMQX。

   ```bash
   $ kubectl apply -f emqx.yaml
   ```

   关于 EMQX 自定义资源的更多信息，请查看 [API 参考](../reference/v1beta4-reference.md)

2. 等待 EMQX 集群就绪。

   ```bash
   $ kubectl get emqxbrokers

   NAME   STATUS   AGE
   emqx   Running  8m33s
   ```

  请确保 `STATUS` 为 `Running`，这可能需要一些时间等待 EMQX 集群准备就绪。
:::

::::
