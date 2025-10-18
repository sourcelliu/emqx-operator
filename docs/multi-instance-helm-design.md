# EMQX Operator Helm 多实例部署设计

本设计文档对应 `dev_plan.md` 阶段 1 的产出，用于指导后续 Helm Chart 改造、测试与文档更新工作。

## 1. 需求梳理

- 同一 Kubernetes 集群内安装多个 `emqx-operator` Release。
- 将 CRD 等集群级资源限定在“控制平面实例”中渲染。
- 保持对现有单实例部署的向后兼容，避免升级时出现破坏性变化。
- 为多实例部署提供明确的参数说明与操作指引。

## 2. 现有模板资源盘点

| 资源类型 | 模板文件 | 作用域 | 备注 |
| --- | --- | --- | --- |
| ServiceAccount | `templates/controller-manager-rbac.yaml` | Namespaced | 受 `serviceAccount.create` 控制 |
| Role / ClusterRole | 同上 | Namespaced / Cluster | 根据 `singleNamespace` 切换；默认 ClusterRole |
| RoleBinding / ClusterRoleBinding | 同上 | Namespaced / Cluster | 同上 |
| Deployment | `templates/controller-manager.yaml` | Namespaced | Operator 主控制器 |
| Service (metrics) | `templates/controller-manager-metrics-service.yaml` | Namespaced | 供 Prometheus 抓取 |
| CRD (5 个) | `templates/crd.*.yaml` | Cluster | 受 `skipCRDs` 控制，默认创建 |
| NOTES | `templates/NOTES.txt` | - | 部署后提示 |

当前问题：

1. CRD 位于 `templates/`，会随 Helm render 运行 `helm uninstall` 时被清理，且第二个 Release 安装会因 CRD 冲突失败。
2. `skipCRDs` 与 `singleNamespace` 的组合无法表达“控制平面实例”这一概念。

## 3. 参数设计与行为定义

新增/重构参数建议：

```yaml
global:
  controlPlane: null    # bool，可在父 Chart 统一配置，单 Chart 则使用 .Values.controlPlane

controlPlane: true      # 首个实例默认 true，其余实例需显式设为 false
skipCRDs: null          # 当 controlPlane=false 时强制 true；保留显式覆盖能力以兼容旧用法
```

行为规则：

1. `effectiveControlPlane := pickFirst(.Values.global.controlPlane, .Values.controlPlane, true)`。
2. 当 `effectiveControlPlane=false` 时：
   - 所有 CRD 模板均跳过渲染。
   - ClusterRole/ClusterRoleBinding 仍保留，以便控制器跨命名空间工作；若 `singleNamespace=true` 则生成 Role/RoleBinding。
3. 当 `skipCRDs=true` 且 `effectiveControlPlane=true`：
   - 仅跳过 CRD 渲染，其他控制平面资源照常创建（兼容旧参数语义）。
4. 在 chart 渲染输出中标注控制平面状态（例如在 Deployment 注解里写入）。

## 4. 模板结构改造

1. **CRD 迁移**：将 `crd.*.yaml` 移到 `deploy/charts/emqx-operator/crds/`。采用 Helm v3 约定，初始安装时由 `helm install` 自动创建，且不会在 `helm uninstall` 时删除 CRD。模板中保留 `{{- if not .Values.skipCRDs }}` 逻辑，但需在 `crds/` 中以 Helm CRD 文件语法实现（纯清单无模板函数）。
2. **RBAC 调整**：
   - ClusterRole/ClusterRoleBinding 名称仍带 Release 前缀，确保多实例隔离。
   - 评估是否需要拆分为控制平面/业务实例两套 RBAC；若权限完全一致则不必拆分，仅确保二次安装不会冲突。
3. **Values Schema 与 README**：若存在 `values.schema.json` 则同步更新，以限制参数类型并提供描述。

## 5. 升级与兼容性策略

- 默认行为与当前 Chart 一致：首次安装时 `controlPlane=true`，`skipCRDs=false`。
- 旧使用者若依赖 `skipCRDs=true`（例如通过外部统一管理 CRD），在升级后仍然生效。
- 建议在 Chart `NOTES.txt` 和 README 中提示：安装多个实例时需保证至少一个实例 `controlPlane=true`；卸载控制平面实例前需要手动确认是否保留集群级资源。

## 6. 测试计划（阶段 3 输入）

- 使用 Kind 或 minikube，执行以下流程：
  1. `helm install emqx-operator cp --set controlPlane=true`
  2. `helm install emqx-operator workload --set controlPlane=false`
  3. 验证两个 Release 均成功，CRD 仅由 cp 实例创建。
  4. 测试升级与回滚：切换 `controlPlane` 参数并观察 CRD 行为。
- 将脚本纳入 `e2e/helm`，供 CI 自动执行。
- 增加模板层面的冒烟校验脚本（`scripts/test-helm-multi-instance.sh`），在 CI 中无需集群即可确认多实例渲染结果。
- 提供基于 kind 的端到端验证脚本（`scripts/kind-verify-helm-multi-instance.sh`），快速验证控制平面与工作实例在真实集群中协同工作。

## 7. 后续工作分解

1. **Stage 2（Helm 改造）**：依据本设计实现参数和模板调整，包括 `_helpers.tpl`、`values.yaml`、`templates/` 与 `crds/` 目录。
2. **Stage 3（测试）**：补充 Kind/Helm 集成测试，并在 CI 工作流中触发。
3. **Stage 4（文档）**：更新 `deploy/charts/emqx-operator/README.md`、项目文档和发布说明，提供多实例部署指南与注意事项。

> 以上内容完成了 dev_plan.md 阶段 1 的调研与设计输出，可作为后续开发工作的依据。
