# GateLens 产品范围与用户旅程

## 愿景

让不熟悉 Gateway API、推理扩展或特定网关实现的运维和平台工程师，能在几分钟内回答：**一个 AI 请求会怎么走、为什么这么走、实际是否这么走、出了问题在哪里。**

GateLens 是解释和诊断层，不替代 Gateway Controller、Service Mesh、Kubernetes API Server、Prometheus 或追踪系统。

## 用户与问题

| 用户 | 常见问题 | GateLens 交付物 |
| --- | --- | --- |
| 平台工程师 | 新路由是否生效，跨命名空间引用是否允许？ | 有效配置图、引用与状态诊断 |
| SRE/运维 | 某模型请求为何 5xx 或超时？ | 请求时间线、候选后端、失败阶段 |
| AI 基础设施工程师 | 流量为何没有进入预期推理池？ | 推理后端选择解释、端点与健康状态 |
| 应用开发者 | Host/path/header/model 请求匹配到哪条规则？ | 输入请求模拟器、匹配详情 |

## 核心旅程

### 从配置理解流量

选择集群和 Gateway 后，GateLens 展示 Listener、绑定 Route、匹配条件、过滤器、后端引用、Service/Endpoint 和推理策略构成的有向图。每条边显示引用是否合法、资源是否 Accepted/ResolvedRefs，以及 Kubernetes `resourceVersion`。

### 从请求解释决策

用户提供方法、主机、路径、请求头、可选模型标识和命名空间上下文。离线解释器返回命中的 Listener/Route、未命中候选及理由、过滤器顺序、后端候选与推理选择、可信度和资源证据。

它只回答**按可见配置应如何流转**，不声称等同于实时实现行为。

### 从异常请求回溯

用户用 request ID、trace ID、时间范围或错误码检索。系统关联访问日志、指标和追踪，将入口、路由、上游连接、重试、响应或错误叠加到逻辑路径；缺失区间明确显示为未知。

## MVP 范围

- 只读 Kubernetes 接入：Gateway API 核心资源、Service、EndpointSlice、Namespace、ReferenceGrant。
- 已安装的 Gateway API Inference Extension CRD 非结构化采集与显示；关键语义由版本化适配器解析。
- 通用 HTTP 路由解释：Host、Path、Method、Header、Query、权重后端、跨命名空间引用。
- 先支持一种团队实际使用最多的网关；不假设 Envoy、Istio、Higress 的扩展语义相同。
- 拓扑、资源详情、请求模拟、配置健康检查、时间范围内证据视图。
- OpenTelemetry Trace 与 Prometheus 的可选只读连接器。

## 非目标

- 替代 Gateway Controller 的配置下发、准入或发布。
- MVP 自动修复、修改 Kubernetes 资源、注入或代理业务流量。
- 仅依靠静态 YAML 精确复刻动态重试、负载均衡和私有扩展行为。
- 默认存储完整提示词、响应正文、令牌或密钥。

## 成功标准

- 用户能在一个界面定位支持资源集中的逻辑路径和每一步证据。
- 不合法引用、未解析后端、无可用 Endpoint、状态未 Accepted 可主动标记。
- 每个“为什么”结论带来源资源、观察时间和推断规则版本。
