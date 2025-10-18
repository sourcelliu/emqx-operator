# 在同一集群部署多个 EMQX Operator

本文介绍如何在同一个 Kubernetes 集群中运行多个 `emqx-operator` Release。常见场景是将负责管理 CRD 等集群级资源的 **控制平面实例** 与多个关注业务命名空间的工作实例分离。

## 前置条件

- 已安装 `helm` v3.8 及以上版本。
- 拥有目标 Kubernetes 集群的 `kubectl` 访问权限。

## 步骤一：安装控制平面实例

首次安装保持默认的 `controlPlane=true`，由该实例负责创建 CRD 等共享资源。

```bash
helm upgrade --install emqx-operator-cp emqx/emqx-operator \
  --namespace emqx-operator-system \
  --create-namespace

# 等待控制器就绪
kubectl wait --for=condition=Ready pods -l "control-plane=controller-manager" -n emqx-operator-system
```

## 步骤二：安装工作实例

工作实例复用控制平面创建的 CRD，因此在安装时禁用控制平面能力：

```bash
helm upgrade --install emqx-operator-workload emqx/emqx-operator \
  --namespace emqx-operator-workload \
  --create-namespace \
  --set controlPlane=false
```

如需更多工作实例，可为不同命名空间重复该步骤。

## 步骤三：检查部署状态

```bash
helm list -A | grep emqx-operator
kubectl get pods -n emqx-operator-system
kubectl get pods -n emqx-operator-workload
```

若只需快速验证模板渲染结果，可在代码仓库根目录执行：

```bash
make helm-multi-instance-test
```

该脚本会渲染控制平面与工作实例模板，确认仅控制平面输出包含 CRD。

如需在真实 Kubernetes 环境中进行端到端验证，可借助 [kind](https://kind.sigs.k8s.io/) 搭建临时集群：

```bash
make helm-multi-instance-kind
```

脚本会自动创建 kind 集群，安装控制平面与工作实例，并确认只有控制平面持有集群级资源。测试结束后会清理所有资源。

## 升级流程

1. 先升级控制平面实例：
   ```bash
   helm upgrade emqx-operator-cp emqx/emqx-operator \
     --namespace emqx-operator-system
   ```
2. 再升级各个工作实例，确保保留 `controlPlane=false`。

## 卸载流程

1. 先删除工作实例：
   ```bash
   helm uninstall emqx-operator-workload -n emqx-operator-workload
   ```
2. 最后删除控制平面实例，避免在清理期间缺失 CRD：
   ```bash
   helm uninstall emqx-operator-cp -n emqx-operator-system
   ```

若集群中仍存在其他 EMQX 工作负载，请勿手动删除 CRD。
