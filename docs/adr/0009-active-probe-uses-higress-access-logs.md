# ADR 0009：首版主动探测使用 Higress 访问日志

- 状态：Accepted
- 日期：2026-08-13

## 背景

静态快照无法证明任意 Wasm 执行后的最终 Route 和 Endpoint。首版需要在不部署 eBPF、不要求业务应用接入 Trace SDK 的条件下，观测一条受控请求的实际 Envoy 路由结果。

Higress 默认访问日志包含 `request_id`、`trace_id`、`route_name`、`upstream_cluster`、`upstream_host`、响应码、耗时和 `wasm.ai_log` Filter State。`request_id` 由 Envoy/Higress 管理，可能被生成或重写，因此不能作为 GateLens 的稳定关联键。当前官方 Helm 模板在未启用 `global.o11y` 时写 `/dev/stdout`，启用时写 `/var/log/proxy/access.log`。

## 决策

首版复用 Agent 主动长轮询通道执行 `probe-http`：

1. Server 生成唯一 probe ID 和 B3 trace ID，不设置 `x-request-id`；
2. Server 只接受 Agent 快照中自动发现的 Gateway Service 入口 ID；Agent 再以本地当前快照校验入口归属并自行构造 URL 后发出真实 HTTP 请求；
3. 默认通过 Kubernetes `pods/log` 读取 Gateway 容器的 stdout/stderr；
4. 可选从 Agent 本地挂载的普通文件读取探测期间新增内容；
5. Agent 注入 `x-gatelens-probe-id`；各 Gateway 必须使用 `%REQ(X-GATELENS-PROBE-ID)%` 将其输出为 JSON 字段 `gatelens_probe_id`；
6. 优先接受 `gatelens_probe_id` 精确相等的日志；仅当该字段为空时才用 `trace_id` 兜底，非空但不匹配时直接拒绝；
7. 不保存请求正文、响应正文、Authorization、Cookie 或 API Key；
8. 不使用 eBPF，不把仅有配置或时间邻近关系标记为 `Observed`。

普通文件必须通过共享 PVC、hostPath 或日志 sidecar 暴露给 Agent。Agent 不使用 `pods/exec` 读取另一个容器的文件。

## 后果

首版能证明 Higress/Envoy 对探测请求产生的最终 L7 决策，但不能验证内核连接、NAT、NetworkPolicy verdict 或未接入网关的内部转发。默认日志必须显式追加 `gatelens_probe_id` 才能使用主关联键。默认日志也不能证明 BBR/EPP 内部每个 `ext_proc` 阶段；只有处理器明确写入 `wasm.ai_log` 或后续在访问日志中增加 ext_proc Filter State 时，才展示对应决策证据。

主动探测具有真实副作用，因此 Server 默认不开启探测 API。可选入口仅来自 selector 命中 Ready Gateway Pod、且端口名或 `appProtocol` 明确表示 HTTP/HTTPS 的 Kubernetes Service；请求命令不携带任意 URL，Agent 不跟随 HTTP 重定向。访问日志 Path 的查询参数在保存前移除。
