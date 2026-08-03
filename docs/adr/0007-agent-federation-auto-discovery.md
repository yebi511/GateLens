# ADR 0007：每集群 Agent 汇总并自动发现跨集群关系

- 状态：Accepted
- 日期：2026-07-28
- Supersedes：ADR 0005 中由用户登记 `ClusterLink` 的部分

## 背景

网关集群与 GPU 推理集群可能独立部署。中央服务不能依赖远端 kubeconfig，用户也不应重复维护一份容易漂移的集群关联关系。

网关已有的上游配置和远端入口配置包含可用于关联的事实，例如 ExternalName、Higress registry domain、Gateway status address 和 Listener hostname。

## 决策

每个 Kubernetes 集群部署一个只读 Agent。Agent 从本集群采集规范化快照，并通过带 Bearer token 的 HTTP 接口上报中央 Server。浏览器只访问中央 Server。

中央 Server 不提供用户可配置的 `ClusterLink`。它将出站 `TransitHop`、Registry 或 ExternalName Service 的目标，与其他集群 Gateway/Listener 的地址或域名做规范化后的精确匹配：

1. 只有唯一匹配才创建 `cross-cluster` 边；
2. 多个同优先级入口匹配时产生告警，不猜测目标；
3. 没有匹配时保留出站边界，不虚构远端内部路径；
4. 自动边必须记录目标、远端入口和配置来源；
5. 每个集群保留独立快照时间，Server 标记时间偏差和 Agent stale 状态。

## 后果

部署只需要为每个 Agent 配置稳定的 `clusterID`、中央 Server 地址和上传凭据，不需要维护关系对象。关联结果会随真实网关配置自动变化。

该关联是静态配置证据，只能说明“出站配置指向已接入集群的某个入口”。只有 Trace、请求 ID 或可靠访问日志才能证明实际请求经过该路径。
