# 主动探测与实际流量路径观测设计

> 实现状态（2026-08-13）：阶段 A 已实现，使用 Higress JSON 访问日志，不依赖 eBPF。当前覆盖源 Agent 发起一次 HTTP 请求、按联邦拓扑在多个已接入 Gateway 上只读采集同一 GateLens probe ID 或 trace ID、分段展示 Route/upstream、`ai_log` 和证据缺口；基于 Span 父子关系的分布式 Trace、ext_proc Filter State、Hubble/eBPF 仍为后续工作。

## 0. 首版使用说明

### 0.1 Higress 日志输出位置

Higress 当前官方 Helm `helm/core/templates/configmap.yaml` 的分支是：

```yaml
accessLogEncoding: TEXT
{{- if .Values.global.o11y.enabled }}
accessLogFile: "/var/log/proxy/access.log"
{{- else }}
accessLogFile: "/dev/stdout"
{{- end }}
```

可用日志来源如下：

| Higress 输出 | GateLens 读取方式 | 条件 |
| --- | --- | --- |
| `/dev/stdout` | Kubernetes `pods/log` | 默认且推荐；Agent 需要 `pods/log get` |
| `/dev/stderr` | Kubernetes `pods/log` | Envoy 配置为 stderr 时同样适用；Higress Helm 当前默认不用此分支 |
| `/var/log/proxy/access.log` | Agent 本地文件增量读取 | 必须将同一日志文件通过共享 PVC、hostPath 或 sidecar 挂载到 Agent |
| 其他普通文件 | Agent 本地文件增量读取 | 同上；Agent 不会进入 Gateway 容器读取 |

普通文件不是节点全局可见路径。仅在 Agent 设置 `GATELENS_PROBE_LOG_FILE` 而没有对应挂载时，探测会明确返回“读取访问日志文件失败”。

### 0.2 日志格式要求

GateLens 不使用 Envoy 的 `x-request-id` 关联探测。Higress/Envoy 可以生成、保留或重写该字段，均不影响 GateLens。

每个参与探测链路的 Gateway 都必须使用单行 JSON access log，并把专用请求头输出为 JSON 字段 `gatelens_probe_id`。在现有 `accessLogFormat` JSON 对象中追加：

```text
"gatelens_probe_id":"%REQ(X-GATELENS-PROBE-ID)%"
```

例如，原格式以 `"ai_log":"%FILTER_STATE(wasm.ai_log:PLAIN)%"}` 结尾时，应改为：

```text
"ai_log":"%FILTER_STATE(wasm.ai_log:PLAIN)%","gatelens_probe_id":"%REQ(X-GATELENS-PROBE-ID)%"}
```

这是对**现有 JSON 格式的字段追加**，不要用上述片段替换完整 `accessLogFormat`。修改 Higress 配置后应确认数据面已更新，并从实际日志验证字段存在。GateLens 所需关键字段至少为：

```text
gatelens_probe_id, trace_id, start_time, method, authority, path,
route_name, upstream_cluster, upstream_host,
response_code, response_flags, response_code_details, duration
```

字段与来源的固定映射如下：

| JSON 字段 | Envoy 格式值 | 用途 |
| --- | --- | --- |
| `gatelens_probe_id` | `%REQ(X-GATELENS-PROBE-ID)%` | 主关联键，必须显式添加 |
| `trace_id` | 保留当前 Higress 格式中的 trace ID 输出 | 兜底关联键 |
| `request_id` | 可保留现状，GateLens 不读取 | Envoy/Higress 自身诊断 |

Agent 只注入 `X-GateLens-Probe-ID` 和 B3 trace header，不注入 `X-Request-ID`。跨多个网关时，必须确保路由、WasmPlugin、鉴权插件和 Header 改写规则不会删除或覆盖 `x-gatelens-probe-id`；每一跳都要输出同名 JSON 字段。

`ai_log=%FILTER_STATE(wasm.ai_log:PLAIN)%` 是可选证据。GateLens 不解析或推测其业务语义，只原样展示非空结果。默认格式没有 ext_proc Filter State，因此 BBR/EPP 的调用阶段、gRPC 状态和内部选择理由不会自动出现。

### 0.3 部署 Agent

中央 Server 必须显式开启探测 API。启用前应先在 Web/API 前配置身份认证、HTTPS 和操作审计：

```yaml
- name: GATELENS_ENABLE_ACTIVE_PROBES
  value: "true"
```

不再配置目标主机白名单。Agent 从 Kubernetes 快照中自动发现 selector 命中 Ready Gateway Pod 的 Service，并且只接受端口名或 `appProtocol` 明确声明 HTTP/HTTPS 的入口。Server 仍需通过上述开关显式开启真实探测。

stdout/stderr 模式不需要卷，只需确认 ClusterRole 含有：

```yaml
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
```

若 `global.o11y.enabled=true`，Higress 写 `/var/log/proxy/access.log`，需要把日志卷以只读方式挂到 Agent，并设置 Agent 可见路径：

```yaml
env:
  - name: GATELENS_PROBE_LOG_FILE
    value: /var/log/higress/access.log
volumeMounts:
  - name: higress-access-log
    mountPath: /var/log/higress
    readOnly: true
```

具体 volume 类型必须与 Higress 的日志持久化方式一致。不同 Pod 的 `emptyDir` 不能直接共享；这种情况下应保持 `/dev/stdout`，或将日志交给集中日志系统后实现新的 Connector。

### 0.4 发起探测

Web 中打开“实时探测”，选择自动发现的网关 Service 入口，填写 Path、可选 HTTP `Host` 和 API Key：

```text
入口: higress-system/higress-gateway:80 (HTTP)
Path: /v1/chat/completions
Host: api.example.com
```

也可以调用 API：

```bash
curl -X POST http://gatelens.example.com/api/v1/probes \
  -H 'Content-Type: application/json' \
  -d '{
    "sourceCluster":"edge-prod",
    "gatewayID":"edge-prod::gateway/higress-system/higress",
	"entryID":"edge-prod::gateway/higress-system/higress/probe-entry/higress-system/higress-gateway/80",
    "method":"POST",
    "path":"/v1/chat/completions",
    "host":"api.example.com",
	"apiKey":"sk-...",
    "contentType":"application/json",
    "body":"{\"model\":\"qwen\"}",
    "timeoutSeconds":15
  }'
```

请求 Body 最多 64 KiB，API Key 最多 8 KiB。Path、Host、Content-Type 和 Body 等草稿按集群保存在当前标签页的 `sessionStorage`，刷新或切换页面后可恢复，关闭标签页或点击“清空草稿”后失效。API Key 默认不进入草稿并在探测后清空；只有用户显式选择“本次标签页保留”时才存入该标签页的 `sessionStorage`。

提交后，API Key 只在 Server 到源 Agent 的短期命令中存在；Agent 将其注入为 `Authorization: Bearer <key>`。API Key 不返回、不写入探测结果，也不发送给远端只读观测 Agent。Server 不保存请求正文或响应正文，当前只在内存中保存探测结果，重启后丢失。

### 0.5 首版工作原理

1. Server 校验源集群、Gateway、入口 ID、HTTP Method、Path、Body 大小和超时。
2. Server 生成随机 128 位 probe ID 和 128 位 B3 trace ID；不生成或设置 `x-request-id`。
3. Agent 经既有长轮询领取只包含 `entryID + path` 的 `probe-http` 命令，并再次用本地 Kubernetes 快照校验入口属于所选 Gateway；URL 由 Agent 自行构造。
4. 文件日志模式先记录文件偏移；随后 Agent 发出请求，注入 `X-GateLens-Probe-ID` 和 `X-B3-TraceId`。
5. 请求完成后，Agent 读取所有 Ready Gateway Pod 的短时间日志，或读取文件偏移后的新增内容。
6. 解析器优先接受 `gatelens_probe_id` 与本次 probe ID 精确相等的 JSON 行；只有该字段为空时才使用精确相等的 `trace_id`。非空但不匹配的 `gatelens_probe_id` 会直接拒绝，不能被 trace ID 覆盖。命中后将 `route_name`、`upstream_cluster`、`upstream_host`、响应状态和 `ai_log` 标为 `Observed`；Path 的查询参数在保存前移除。
7. 源请求完成后，Server 向所有已接入集群中关联了 Ready 数据面 Pod 的 Gateway 下发只读 `probe-observe` 命令，不依赖静态路由或跨集群边决定观测目标。同集群的第二个 Gateway 也会被检查；纯配置对象不会产生无效查询。
8. 实际匹配 probe ID/trace ID 的 Gateway 显示为 `Observed` 后续网关段，并按日志时间排序。
9. 对没有匹配日志的 Gateway，Server 通过可扩展的关联规则使用已观测 hop 的 `upstream_host` 和 `upstream_cluster` 反查网关入口：地址精确命中为高置信候选；Service DNS、Gateway `status.addresses` 或所属 Listener hostname 精确命中为中置信候选。Higress McpBridge 是首个配置链规则：它把 `.dns` 解释为 `<registry.name>.<registry.type>` 目标标识，而不是 Service 域名后缀；例如 `outbound|80||llm-inference-providers.internal.dns` 会先命中对应 Registry，再沿 `Registry -> Service` 配置关系映射到选择该 Service 的同集群 Gateway。后续其他资源关联只需增加规则；证据强度、去重和歧义处理由统一聚合器完成。若同一值匹配多个网关则标记为歧义候选。若只有跨集群配置边，则仅在该边的上一网关已经被观测时显示低置信配置候选，不越过缺失段继续推断。
10. 候选网关明确显示为“候选下一跳”和证据 gap，不计作已观测路径。没有地址、Cluster 或直接配置关系的无关广播网关继续隐藏；日志读取错误也只在网关已被判定为候选时返回，避免全局广播产生无关告警。

首版不读取 eBPF Flow，因此无法证明 TCP 建连、NAT、NetworkPolicy verdict，也不能用网络证据验证 Envoy 报告的 upstream。`upstream_host` 只证明上一跳 Envoy 选择或尝试连接了该地址，不证明候选网关已经处理请求。多网关只有在各网关原样传播 `x-gatelens-probe-id`（或至少传播 B3 trace ID）、输出对应关联字段且相应 Agent 能读取日志时，才能形成完整的已观测多跳请求路径。GateLens 会广播只读日志观测，但无法识别未接入集群；Header 被删除、请求未到达、日志延迟、日志格式缺少字段和读取权限不足都可能表现为同一个证据 gap，产品不得把 gap 单独解释成某一种原因。

### 0.6 BBR、EPP 与 ext_proc 边界

默认 Higress 日志可以显示 Wasm 已写入的 `ai_log` 和最终路由结果，但没有以下 ext_proc 运行时字段：

```text
request_header_call_count
request_body_call_count
request_header_latency_us
request_body_latency_us
ext_proc gRPC status
processor decision metadata
```

因此首版可能看到“BBR/EPP 执行后的最终 Route/Endpoint”，或者看到其主动写入 `ai_log` 的摘要，但不能仅凭默认日志证明具体调用了哪个外部处理器。后续可在 Envoy 支持的版本中把 `%FILTER_STATE(envoy.filters.http.ext_proc:TYPED)%` 加入访问日志，并要求 BBR/EPP 通过 allowlist dynamic metadata/Filter State 输出 `processor`、`rule_id`、`selected_pool/endpoint` 和 `reason_code`。这些属于 L7 证据扩展，不依赖 eBPF。

## 1. 背景

GateLens 仍展示 Kubernetes 和 Envoy 配置拓扑，用于核验声明关系和资源健康状态，但不再根据配置快照预测一条请求的最终 Route 或 upstream。

WasmPlugin 可以根据 Header、Body、动态元数据、外部服务响应、缓存或运行时状态修改 Path、Host 等路由输入，并清除 Envoy route cache 触发重新匹配。对于第三方推理平台，GateLens 不掌握插件代码、Host ABI、外部依赖和运行时状态，因此不能可靠预测最终路由。

产品能力边界如下：

| 能力 | 回答的问题 | 证据 |
| --- | --- | --- |
| 配置拓扑核验 | 配置声明了哪些 Gateway、Route、Service 和 Endpoint 关系？ | 配置快照，不输出请求路径结论 |
| 实际流量观测 | 一条受控请求实际上经过了哪里？ | 日志、Trace、网络流，`Observed` |

实际流量观测不尝试理解任意 WasmPlugin 的业务逻辑，而是记录其执行后的真实结果。

## 2. 目标与非目标

### 2.1 目标

- 从指定位置发送一条受控的真实 HTTP/gRPC 请求。
- 展示请求实际经过的 Gateway、代理、Route、upstream cluster、Service 和 Endpoint。
- 支持跨命名空间和跨集群的多跳路径。
- 识别重试、镜像、hedging 和并行推理形成的分支路径。
- 以证据等级和时间标记区分已观测、配置解析、静态推断和未知部分。
- 尽量不修改业务应用或第三方 WasmPlugin。
- 遥测过载时允许丢失观测事件，不能阻塞业务流量。

### 2.2 非目标

- 不承诺在零接入条件下解释 WasmPlugin 内部执行了哪条代码分支。
- 不通过反编译或沙箱执行第三方 Wasm 来预测行为。
- 不把时间相近的网络连接自动声明为同一请求。
- 不采集 Cookie、提示词、请求正文或响应正文；API Key 仅用于本次请求的 Authorization Header，禁止写入日志和结果。
- 不保证观察到云负载均衡器、托管网关或未接入集群的内部实现。

## 3. 基本判断

### 3.1 eBPF 观测的是连接，不天然等于请求

eBPF 可以从节点内核获得真实的 L3/L4 网络事实，例如源/目标 Pod、IP、端口、TCP 状态、转发或丢弃结果。但 Envoy 会终止下游连接并通过连接池建立或复用上游连接：

```mermaid
flowchart LR
  CLIENT["Probe client"] -->|"connection A"| E1["Envoy A"]
  E1 -->|"pooled connection B"| E2["Envoy B"]
  E2 -->|"HTTP/2 connection C"| MODEL["Model endpoint"]
```

连接 A、B、C 的五元组不同，上游连接可能早已建立，一个 HTTP/2 连接也可能同时承载多个请求。因此，仅靠 eBPF 不能可靠证明这些连接属于同一个 HTTP 请求。

### 3.2 请求级关联必须依赖 L7 标识

要精确跟踪某个请求，各代理至少需要传播一个稳定的关联标识：

- `x-gatelens-probe-id`，GateLens 主动探测的主关联键，必须防止伪造和越权查询；
- W3C `traceparent` 或 B3 trace ID，作为追踪系统和日志关联的兜底标识。

`x-request-id` 由 Envoy/Higress 管理，GateLens 不注入、不读取，也不依赖其跨网关稳定性。

Envoy 或现有遥测系统需要输出与该标识关联的 Route、upstream cluster 和 upstream host。eBPF 作为网络路径验证证据，而不是唯一的请求追踪器。

## 4. 总体架构

```mermaid
flowchart LR
  UI["GateLens Web"] --> API["GateLens Server"]
  API -->|"probe command"| AGENT["Source cluster Agent"]
  AGENT -->|"probe ID + trace context"| GW1["Gateway / Envoy A"]
  GW1 --> GW2["Gateway / Envoy B"]
  GW2 --> EP["Model endpoint"]

  K8S["Kubernetes snapshots"] --> CORR["Evidence correlator"]
  ENVOY["Envoy access logs / OTel"] --> CORR
  FLOW["Hubble / eBPF / flow source"] --> CORR
  API --> CORR
  CORR --> PATH["Observed path tree"]
```

### 4.1 组件职责

| 组件 | 职责 |
| --- | --- |
| GateLens Server | 校验探测请求、创建任务、下发命令、收集证据、关联路径、持久化结果 |
| 集群 Agent | 在指定网络位置发请求；读取本集群允许访问的运行时证据；不上传敏感凭据 |
| Probe Runner | 限制协议、目标、超时、请求大小和并发；记录发送与响应摘要 |
| Envoy/OTel Connector | 接收或查询请求级 Route、Cluster、Upstream Host 和响应信息 |
| Flow Connector | 接入 Hubble、现有 eBPF 平台、云 Flow Logs 等连接级事实 |
| Evidence Correlator | 按 GateLens probe ID/trace ID 关联 L7 事件，使用网络流和 Kubernetes 对象进行验证与映射 |

## 5. 证据模型

沿用 GateLens 的证据分类，并增加路径事件的来源和完整性：

| 等级 | 含义 | 示例 |
| --- | --- | --- |
| `Declared` | 配置中声明 | HTTPRoute 指向 Service |
| `Resolved` | 引用已解析 | Service 对应 EndpointSlice |
| `Observed` | 运行时证据明确证明 | Envoy 日志记录 route、cluster 和 upstream host |
| `Inferred` | 根据配置或弱关联推断 | 未输出 route name，只能从 config dump 推断候选 Route |
| `Unknown` | 无足够证据 | TLS 内部字段不可见、远端集群未接入 |

每条路径边至少保存：

```text
probeID / traceID
source and destination object references
clusterID / node / namespace / workload
observedAt and monotonic or source timestamp when available
protocol / address / port
routeName / upstreamCluster / upstreamHost when available
responseCode / responseFlags / duration
evidenceType / evidenceSource / confidence
```

没有明确标识的网络流只能作为某条已知 L7 跳的验证证据。仅凭时间窗口和五元组关联时必须标记为 `Inferred`，不能升级为 `Observed`。

## 6. 主动探测流程

1. 用户选择入口、源集群、探测位置、Method、Host、Path 和可选的安全请求模板。
2. Server 执行 RBAC、快照入口归属和风险校验，生成 `probeID` 和 trace context。
3. Server 通过现有 Agent 长轮询通道下发 `probe-http` 命令。
4. Agent 在集群内部发出请求。凭据只通过集群内 Secret 引用读取，不回传明文。
5. 各 Envoy 和遥测组件记录该请求的 L7 决策；Flow Source 同时记录相关连接。
6. Server 在有限时间窗口内等待证据，按 probe ID、Trace 父子关系和 trace ID 组装路径。
7. Correlator 将 upstream IP 映射到 Service、EndpointSlice、Pod、Node 和集群快照。
8. 结果以路径树展示。重试、镜像和并发上游分别形成分支。
9. 超时后返回已有证据，并明确列出未观测边界，不补画虚构路径。

### 6.1 探测位置

探测结果依赖请求从哪里发出，至少支持：

- GateLens 所在集群的 Agent；
- 指定源集群 Agent；
- 指定 Namespace 中受控的临时 Probe Pod；
- 外部探测器，作为后续可选能力。

UI 必须显示探测位置，不能把集群内请求等同于公网用户请求。

## 7. Envoy 与 Wasm 的观测边界

推荐 Envoy 结构化访问日志或 OTel Span 至少包含：

```text
timestamp
GateLens probe ID / trace ID
proxy, cluster and workload identity
request method, authority, original path and final path when available
route name
upstream cluster
upstream host
response code and response flags
duration
selected dynamic metadata/filter state allowlist
```

这些字段可以证明 Wasm 执行后的最终路由结果，但通常不能证明具体由哪个 WasmPlugin、哪条代码分支进行了修改。

若平台希望展示：

```text
原始 Path -> 某过滤器修改 Path -> 清除 route cache -> 二次路由
```

至少需要满足一种扩展观测契约：

- WasmPlugin 将允许公开的变换摘要写入 dynamic metadata 或 filter state；
- 在过滤器链关键位置安装统一观测 Filter；
- 平台现有 Trace/日志包含过滤器级事件；
- GateLens 为特定平台提供专用 Connector。

否则只展示“入口字段”和“最终路由”，中间变换标记为 `Unknown`。

## 8. eBPF、Cilium 与 Hubble

### 8.1 eBPF DaemonSet 的作用

独立 eBPF Sensor 通常以 DaemonSet 在每个节点运行，加载经过内核验证的 eBPF 程序，在 socket、cgroup、TC 或 tracepoint 等位置记录网络事件。它提供：

- Pod/进程到目标 IP:Port 的实际连接；
- TCP 建连、关闭、重传或失败等信号；
- 节点、方向、字节量和转发/丢弃结果；
- 在数据源支持时提供 NAT 前后、策略 verdict 和服务身份。

它不应承担任意 TLS 解密、全量 Payload 抓取或 Wasm 语义解释。

### 8.2 Cilium/Hubble 接入

Cilium 是 Kubernetes CNI 和 eBPF 数据面。Hubble Server 使用 Cilium 已产生的流事件，Hubble Relay 聚合所有节点。因此：

- 集群已使用 Cilium 时，GateLens 应只读接入 Hubble Relay，不重复部署 eBPF Sensor；
- 只安装 Hubble Relay 不能为非 Cilium 集群产生流数据；
- 不应为了 GateLens 观测而要求用户更换现有 CNI；
- 非 Cilium 集群通过统一 `FlowSource` 接口接入现有网络观测系统或后续的独立 Sensor。

建议接口：

```text
FlowSource
|- CiliumHubbleSource
|- ExistingEBPFSource
|- CloudFlowLogSource
|- CNIFlowSource
`- GateLensEBPFSensor (later)
```

## 9. 性能设计

eBPF 程序会在内核网络路径上同步执行，因此不是零成本。大流量平台必须遵循以下原则：

- 默认只采集聚合后的 L3/L4 Flow 或连接事件，不做全量逐包追踪。
- 在内核或数据源侧按集群、Namespace、Workload、端口和时间窗口尽早过滤。
- 主动探测时短时间提高目标路径的采集粒度，结束后恢复默认模式。
- 缓冲区、队列和存储均有界；过载时丢遥测并报告 `lostEvents`，不反压业务流量。
- 对高基数 Header、URL 和标签实行 allowlist、脱敏和长度限制。
- 如果已有 Hubble 或企业级流平台，复用现有数据源，避免重复内核探针。
- 上线前测量开启前后的吞吐、CPU、p95/p99 延迟、Agent CPU、事件丢失率和存储增长。

GateLens 不规定一个通用的“可接受开销”数字。不同内核、CNI、流量模式和采集粒度差异很大，必须在目标环境灰度验证。

## 10. 安全与副作用控制

主动探测会产生真实流量，必须视为高风险能力：

- 禁止任意 URL，只允许从当前快照选择已发现的 Gateway Service HTTP/HTTPS 入口；Agent 独立复核且不跟随重定向。
- 使用独立 RBAC 权限，例如 `probe:execute`、`probe:read` 和 `probe:admin`。
- 限制 Method、请求大小、响应读取大小、超时、并发和每用户速率。
- 默认不允许具有明显副作用的请求；推理平台优先使用测试租户、测试模型或约定的 dry-run 能力。
- Secret 只在 Agent 所在集群读取和使用，Server、日志和 UI 不保存明文。
- 不记录 Authorization、Cookie、API Key、提示词和响应正文。API Key 只存在于源 Agent 的内存请求上下文中。
- `x-gatelens-probe-id` 必须由 Server 签发并限制有效期，不能允许用户查询其他租户的同名 ID。
- 对所有探测操作记录审计日志，包括操作者、目标、时间、模板和结果摘要。

## 11. API 与领域模型草案

现有 Agent 命令只支持 `envoy-config`。后续可增加：

```text
AgentCommand.kind = probe-http
```

建议新增 API：

| API | 用途 |
| --- | --- |
| `POST /api/v1/probes` | 创建主动探测任务 |
| `GET /api/v1/probes/{id}` | 获取状态和证据完整度 |
| `GET /api/v1/probes/{id}/path` | 获取规范化路径树 |
| `POST /api/v1/telemetry/envoy-events` | 接收结构化 Envoy/OTel 事件，具体部署可替换为消息系统 |

建议领域对象：

| 类型 | 关键字段 |
| --- | --- |
| `ProbeRequest` | sourceCluster, gatewayID, entryID, method, host, path, apiKey, contentType, body, timeout |
| `ProbeExecution` | id, traceID, state, startedAt, completedAt, responseSummary |
| `TrafficEvidence` | type, source, timestamp, clusterID, attributes, redactionState |
| `ObservedHop` | from, to, route, upstream, evidenceRefs, confidence |
| `ObservedPath` | probeID, snapshotRefs, branches, gaps, completeness |

## 12. 前端表达

前端只提供 `实时探测` 请求入口：发送真实请求，显示风险提示、探测位置和实际证据。配置页面仍展示静态资源关系，但不提供“配置预检”或预测路径结论。

路径视图按事件顺序或路径树展示：

```text
Probe source
  -> Gateway A
     route=chat, observed
  -> Gateway B
     route=qwen, observed
  -> Service inference/qwen
  -> Pod qwen-7c9d
```

每一跳必须显示：

- 集群、Namespace、工作负载和时间；
- `Observed`、`Resolved`、`Inferred` 或 `Unknown` 标签；
- Envoy、Trace、eBPF 或 Kubernetes 等证据来源；
- 未观测边界、事件丢失和快照时间偏差；
- 重试、镜像或并发上游产生的独立分支。

## 13. 分阶段落地

### 阶段 A：Envoy 请求级证据

- 增加 `probe-http` Agent 命令和受控请求执行器。
- 定义 Envoy access log/OTel 最小字段契约。
- 按 GateLens probe ID/trace ID 组装 Route、Cluster 和 Upstream Host。
- 将 IP 映射到现有 Kubernetes 拓扑。
- 先覆盖单集群和双网关的成功/失败样本。

### 阶段 B：连接级验证

- 定义 `FlowSource` 接口和规范化 Flow 事件。
- 首先实现 Hubble Connector，不自研 CNI。
- 用网络流验证 Envoy 报告的代理和 Endpoint 连接。
- 展示 NetworkPolicy 丢弃、连接失败和未观测边界。

### 阶段 C：多集群与分支路径

- 跨集群关联同一 Trace，处理时钟偏差和重复事件。
- 展示重试、镜像、hedging、并发推理和部分失败。
- 增加数据保留、租户隔离和规模化存储。

### 阶段 D：可选的过滤器级解释

- 定义 dynamic metadata/filter state allowlist 契约。
- 为愿意接入的平台提供统一观测 Filter 或专用 Connector。
- 仍不把缺少证据的 Wasm 内部行为标记为已观测。

## 14. 验收标准

首个可用版本至少满足：

1. 用户可从指定 Agent 发出一条受控请求，并获得唯一 probe ID 和 trace ID。
2. 单个 Envoy 能展示实际 Route、upstream cluster、upstream host、响应码和耗时。
3. 双网关场景能用同一 Trace 关联两段请求，缺失一段时明确显示 gap。
4. upstream IP 能映射到正确的 Service、EndpointSlice 和 Pod。
5. Hubble 可用时展示连接验证；不可用时不影响 L7 探测结果。
6. 任意 Wasm 存在时不进行代码级推断，只展示观测到的输入和最终结果。
7. 遥测事件丢失、快照不一致和时间偏差会降低完整度，不生成虚构路径。
8. 探测凭据、请求正文和响应正文不进入 Server 持久化或日志。

## 15. 待验证问题

- 目标 Higress、Istio 和 Envoy 版本实际可输出哪些日志格式字段。
- 多个网关是否原样传播 `x-gatelens-probe-id` 和 trace context，Wasm 是否会修改或删除它们。
- 推理平台是否存在测试模型、dry-run 或无副作用探测约定。
- 各目标集群当前使用的 CNI 和已有 Flow/Trace 平台。
- Envoy access log、ALS、OTel Collector 和消息系统中哪一种接入方式运维成本最低。
- HTTP/2、gRPC streaming、连接复用和长响应下，探测完成条件如何定义。
- 跨集群时钟偏差、Trace 采样策略和日志保留时间是否满足关联要求。
