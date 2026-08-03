# ADR 0008：联邦运行时查询使用 Agent 主动长轮询

- 状态：Accepted
- 日期：2026-07-28

## 背景

中央 Server 部署在 Kubernetes 集群外，不持有各成员集群的 kubeconfig，也不能主动连接位于集群网络内的 Agent。Envoy `config_dump` 是按需运行时数据，不能仅依靠周期拓扑快照得到。

## 决策

中央 Server 为运行时查询维护短期内存命令队列。浏览器查询联邦 Gateway 时，Server 从全局 Gateway ID 确定归属集群，将集群内 Gateway ID 放入该集群队列，并等待有截止时间的结果。

每集群 Agent 使用与快照上报相同的 Bearer token，主动长轮询 Server：

1. Agent 只领取自己 `clusterID` 的命令；
2. 收到 Envoy 查询后，Agent 在本集群临时 port-forward 到 Ready Envoy Pod；
3. Agent 将解析结果和原始 `config_dump` 主动回传 Server；
4. Server 用命令 ID 将结果交给等待中的浏览器请求；
5. 命令超时、Agent 离线或本集群读取失败时，Server 返回明确错误。

Server 不主动连接 Agent，不保存远端集群凭据，也不把命令或结果持久化。当前命令通道只支持只读 Envoy 配置查询。

## 后果

Envoy 页面可以在联邦模式下查询任意已接入且在线集群的 Gateway，同时保持 Agent 主动出站连接模型。查询依赖 Server 与 Agent 都升级到支持命令通道的版本，并受网络延迟、Agent 在线状态和 Envoy Admin 可达性的影响。

共享 Bearer token 仍属于 MVP 认证方式；后续若引入每 Agent 身份或 mTLS，应将 `clusterID` 与凭据绑定，避免一个 Agent 领取其他集群的命令。
