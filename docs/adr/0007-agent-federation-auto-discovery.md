# ADR 0007：每集群 Agent 汇总并自动发现跨集群关系

- 状态：Accepted
- 日期：2026-07-28
- Supersedes：ADR 0005 中由用户登记 `ClusterLink` 的部分

## 背景

网关集群与 GPU 推理集群可能独立部署。中央服务不能依赖远端 kubeconfig，用户也不应重复维护一份容易漂移的集群关联关系。

网关已有的上游配置和远端入口配置包含可用于关联的事实，例如 ExternalName、Higress registry domain、Gateway status address 和 Listener hostname。

## 决策

每个 Kubernetes 集群部署一个只读 Agent。Agent 从本集群采集规范化快照，并通过带 Bearer token 的 HTTP 接口上报中央 Server。浏览器只访问中央 Server。

中央 Server 不提供用户可配置的 `ClusterLink`。它将出站 `TransitHop`、Registry 或 ExternalName Service 的目标，与其他集群 Gateway、Listener 或 Ingress 的地址或域名做规范化后的精确匹配：

1. 只有唯一匹配才创建 `cross-cluster` 边；
2. 多个同优先级入口匹配时产生告警，不猜测目标；
3. 没有匹配时保留出站边界，不虚构远端内部路径；
4. 自动边必须记录目标、远端入口和配置来源；
5. 每个集群保留独立快照时间，Server 标记时间偏差和 Agent stale 状态。

匹配实现采用有序规则链。每条规则只负责识别某类出站配置并提供候选入口，唯一匹配、Listener 优先、歧义告警和跨集群边生成由公共层处理。当前规则顺序为：

1. `higress-mcpbridge`：McpBridge registry 本身是 Higress 控制器转换为 ServiceEntry 的已声明上游，因此其 `domain` 可与远端 Gateway `status.addresses`、Listener `hostname` 或 Kubernetes Ingress 的 host/LoadBalancer 地址精确匹配；若还能观测到 Higress Ingress 通过 `higress.io/destination` 选中该 registry，则把选择关系一并记录为更强证据；传输协议优先来自 `higress.io/backend-protocol`，否则使用 registry `protocol`；
2. `exact-configuration-address`：保留通用的 `Destination`、`Domain`、`ExternalName` 到远端 `Address`、`Hostname` 的精确匹配。

入口候选优先级为 Listener、Gateway、Ingress。新增网关或服务发现机制时增加规则并插入规则链，不修改公共消歧逻辑；更具体的规则必须位于通用规则之前。

## 后果

部署只需要为每个 Agent 配置稳定的 `clusterID`、中央 Server 地址和上传凭据，不需要维护关系对象。关联结果会随真实网关配置自动变化。

该关联是静态配置证据，只能说明“出站配置指向已接入集群的某个入口”。只有 Trace、请求 ID 或可靠访问日志才能证明实际请求经过该路径。
