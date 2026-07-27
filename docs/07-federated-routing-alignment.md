# 联邦路由设计对齐说明

本文使 [跨命名空间与跨集群推理流量设计](06-federated-routing.md) 成为既有设计文档的补充与优先约束；当本文与早期“单集群 Gateway -> Service -> Endpoint”表述冲突时，以 ADR 0005 和 `06-federated-routing.md` 为准。

## 对系统架构的影响

- 单集群部署仍是 MVP 的交付形态，但必须支持跨命名空间的 Route/BackendRef/ReferenceGrant 解析。
- FQDN、负载均衡地址、网格 East-West Gateway 或未接入远端，均表示为 `TransitHop`；不得作为当前集群的 Service 或 Endpoint 伪造。
- 多集群接入后，各集群保留独立 `TopologySnapshot`，API 用 `FederatedSnapshot` 关联它们；它不是全局原子快照。

## 对领域模型的影响

除 `TopologySnapshot`、节点、边、策略、候选和证据之外，必须新增或等价表达：

| 类型 | 目的 |
| --- | --- |
| `ClusterIdentity` + `NamespaceScope` | 以 `clusterID + namespace` 唯一定位对象 |
| Gateway 运行时元数据 | 合并到对应 Gateway；没有 Gateway API 对象时由 Higress/Istio Deployment 生成规范化 Gateway 节点 |
| `TransitHop` | 表达跨集群或外部上游传输边界 |
| `ClusterLink` | 记录已知集群连接及其发现、信任状态 |
| `FederatedSnapshot` | 固定一次多跳解释使用的各集群快照和一致性 |

## 对前端的影响

- “集群”控件语义改为“入口集群”；关联集群只在已接入且与当前路径有关时显示。
- 图按集群分区、分区内按命名空间分组。跨命名空间边展示 `namespace/name` 和 ReferenceGrant/状态证据。
- 跨集群边采用带目标和传输方式标签的虚线；远端未接入时显示“远端未接入”，不能继续展示虚构的 Istio Route 或模型 Endpoint。
- 节点详情并列显示：配置对象位置、数据面工作负载位置、上游服务位置。它们不同不是错误。
- 请求模拟按 Hop 分段。每段显示所属集群、快照时间和证据类型；快照时间差大、目标不可解析或远端未接入会降低置信度。
- 健康检查新增：缺少 ReferenceGrant、远端目标不可解析、远端入口不可观测、联邦快照时间偏差过大。

## 对 MVP 边界的影响

MVP 的“单集群”只意味着 GateLens 服务首次部署到一个集群，不意味着所有被展示的上游都在同一集群：

1. MVP 必须正确解释 Higress `higress-system`、业务 Route 和 `inference` Service 分处不同命名空间的情况。
2. MVP 必须把独立推理集群的 Istio 入口显示为可验证的远端边界。
3. 只有 P1 接入两个只读集群后，才静态拼接 Higress -> 远端 Istio Gateway -> inference Service 的配置路径。
4. 只有 P2 接入 Trace/日志/指标后，才能声称实际请求跨越了两个网关。

## 继续实现前的样本要求

- Higress 工作负载在 `higress-system`、推理 Service 在 `inference` 的真实匿名化配置。
- 一个成功的跨命名空间 BackendRef 和一个缺少 `ReferenceGrant` 的失败样本。
- Higress 出口指向独立推理集群 Istio IngressGateway 的目标配置及证据来源。
- 推理集群中 Istio API 或 Gateway API 的版本和样本资源。
- 成功和失败请求各一条可关联 trace/request ID，确认跨网关关联字段。
