# 多实例 emqx-operator Helm 部署改造计划

## 背景与现状
- 目前 Helm Chart (`deploy/charts/emqx-operator`) 在安装时默认渲染并下发所有 CRD 模板（`crd.*.yaml`），导致第二个 Release 安装时因 CRD 已存在而失败。
- 除 CRD 外，其他集群级资源（如 RBAC）虽然可复用（名称带 Release 前缀），但缺少明确的部署模式说明，容易造成误用。
- 项目缺少自动化验证多实例场景的测试用例和部署指导。

## 目标
1. 支持在同一个 Kubernetes 集群中通过 Helm 安装多个 emqx-operator Release（不同命名空间或场景），且部署流程清晰、一致。
2. 提供清晰的控制参数，使用户能够区分“集群级资源控制平面实例”和“业务实例”。
3. 为多实例部署提供验证手段与文档，包括升级/回滚时的注意事项。

## 方案概述
- 引入显式的控制平面开关：`global.controlPlane`（命名待确认），默认只有第一个实例开启，用来下发 CRD 等集群级资源；其他实例只部署 Controller Deployment 及其命名空间内资源。
- 对现有 `skipCRDs` 等参数重新梳理：保留向后兼容，同时将 CRD 渲染统一受 `controlPlane` 控制，避免多处手动配置。
- 按照 Helm 最佳实践，将 CRD 模板迁移到 `crds/` 目录，同时保留模板门控逻辑，确保升级/卸载过程可控。
- 新增 e2e/helm 测试验证两个 Release（一个 controlPlane=true，另一个 false）可以在同一集群成功部署、升级及卸载。
- 补充官方文档与 README，给出部署矩阵及注意事项。

## 工作分解
### 阶段 1：调研 & 设计（1-2 天）
- 清点现有模板中所有集群级资源（CRD、Webhook、ClusterRole 等），明确哪些需要在所有实例中保留，哪些只在控制平面部署。
- 输出设计文档：参数设计、模板结构改造、兼容性分析、迁移策略。

### 阶段 2：Helm Chart 改造（3-4 天）
- 调整 `values.yaml` 与 `_helpers.tpl`，引入新的控制参数并保证默认行为与现状一致（单实例场景无感知）。
- 重构模板：
  - 将 CRD 移至 `crds/` 目录，并通过 `controlPlane` 及 `skipCRDs` 双重门控。
  - 校验 RBAC/ServiceAccount/LeaderElection 等资源名称，确保 release 级隔离。
- 更新 `Chart.yaml`/`values.schema.json`（若存在）及 README。

### 阶段 3：测试与验证（2-3 天）
- 编写脚本或复用现有 e2e 框架，使用 Kind 或集群模拟两个 Helm Release 的安装、升级、卸载流程。
- 在 CI 中增加新的工作流或扩展现有 Helm 测试，确保多实例场景稳定。
- 本地验证在 `singleNamespace=true/false` 情况下的行为差异。

### 阶段 4：文档与发布（1 天）
- 在 `docs/` 或 `deploy/charts/**/README.md` 中新增多实例部署指南（控制平面实例安装步骤、业务实例安装步骤、常见问题）。
- 更新 `CHANGELOG/RELEASE`，说明向后兼容点与可能的手动操作（如已有部署升级时需先设置 `controlPlane=true`）。
- 准备回滚策略说明：如何仅调整 Helm Values 即可恢复单实例行为。

## 交付物
- 更新后的 Helm Chart（支持多实例部署）。
- 多实例自动化验证脚本/CI 流程。
- 配套文档与发布说明。

## 风险与缓解
- **升级风险**：现有用户升级到新 Chart 可能未显式设置 `controlPlane`。→ 默认保持兼容（首次安装仍下发 CRD），并在文档中强调多实例时需调整参数。
- **CRD 管理复杂度**：多实例卸载时避免误删 CRD。→ 明确 Helm hooks/资源注释，并在控制平面卸载时提示手动确认。

## 时间预估
- 总计约 7-10 个工作日，具体取决于测试/CI 集成复杂度与文档评审周期。

## 后续扩展
- 评估是否需要将 CRD 独立打包为 Side Chart 以便由集群管理员统一管理。
- 考虑引入 Helm Library Chart 或 Operator Lifecycle Manager (OLM) 支持，进一步标准化部署。
