# Istio/Higress ext_proc 访问日志接入手册

本文记录 GateLens 实时探测所需的 Istio/Higress 访问日志配置，以及 BBR
EnvoyFilter 的修改方式。目标是在不修改 BBR、EPP 和推理服务自身日志的前提下，
从同一条 Envoy access log 中区分 BBR、EPP 和最终 upstream。

适用边界：

- 首版不接入 Hubble/eBPF。
- 日志是请求级运行时证据；Kubernetes Ready 状态和 McpBridge/Service 映射是快照证据。
- `upstream_host` 为 `-` 时，日志没有提供实际 Endpoint 地址。此时只能展示
  `upstream_cluster` 和配置映射，不能确认实际 Pod。

## 1. Istio/Higress 访问日志配置

### 1.1 输出位置与编码

推荐把网关 access log 输出到 stdout，GateLens Agent 通过 Kubernetes `pods/log`
只读采集：

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  meshConfig:
    accessLogFile: /dev/stdout
    accessLogEncoding: TEXT
    accessLogFormat: |
      {"gatelens_probe_id":"%REQ(X-GATELENS-PROBE-ID)%","trace_id":"%REQ(X-B3-TRACEID)%","start_time":"%START_TIME%","downstream_remote_address":"%DOWNSTREAM_REMOTE_ADDRESS%","method":"%REQ(:METHOD)%","authority":"%REQ(:AUTHORITY)%","path":"%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%","route_name":"%ROUTE_NAME%","upstream_cluster":"%UPSTREAM_CLUSTER%","upstream_host":"%UPSTREAM_HOST%","upstream_service_time":"%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%","response_code":"%RESPONSE_CODE%","response_flags":"%RESPONSE_FLAGS%","response_code_details":"%RESPONSE_CODE_DETAILS%","upstream_transport_failure_reason":"%UPSTREAM_TRANSPORT_FAILURE_REASON%","duration":"%DURATION%","ai_log":"%FILTER_STATE(wasm.ai_log:PLAIN)%","ext_proc_bbr":"%FILTER_STATE(gatelens.filters.http.ext_proc.bbr:TYPED)%","ext_proc_epp":"%FILTER_STATE(envoy.filters.http.ext_proc:TYPED)%"}
```

这里继续使用 `TEXT`，是因为 Higress 的 `accessLogFormat` 本身是一行完整 JSON。
如果现网已经使用结构化 `JSON` provider，可保留现有 provider，只需增加第 1.3 节的
同名字段。

Higress 使用 Helm 或 Operator 管理时，应修改其 values/mesh config 源配置，不要只修改
数据面 Pod 内的临时 Envoy 配置。若 `global.o11y.enabled=true` 导致日志写入
`/var/log/proxy/access.log`，必须把同一文件挂载给 Agent，并配置
`GATELENS_PROBE_LOG_FILE`；不同 Pod 的 `emptyDir` 不能直接共享。

Agent 使用 stdout/stderr 时需要以下只读权限：

```yaml
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
```

### 1.2 GateLens 基础字段

在现有单行 JSON `accessLogFormat` 中保留或增加以下字段：

```json
{
  "gatelens_probe_id": "%REQ(X-GATELENS-PROBE-ID)%",
  "trace_id": "%REQ(X-B3-TRACEID)%",
  "start_time": "%START_TIME%",
  "downstream_remote_address": "%DOWNSTREAM_REMOTE_ADDRESS%",
  "method": "%REQ(:METHOD)%",
  "authority": "%REQ(:AUTHORITY)%",
  "path": "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%",
  "route_name": "%ROUTE_NAME%",
  "upstream_cluster": "%UPSTREAM_CLUSTER%",
  "upstream_host": "%UPSTREAM_HOST%",
  "upstream_service_time": "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%",
  "response_code": "%RESPONSE_CODE%",
  "response_flags": "%RESPONSE_FLAGS%",
  "response_code_details": "%RESPONSE_CODE_DETAILS%",
  "upstream_transport_failure_reason": "%UPSTREAM_TRANSPORT_FAILURE_REASON%",
  "duration": "%DURATION%",
  "ai_log": "%FILTER_STATE(wasm.ai_log:PLAIN)%"
}
```

这是字段清单，不要求覆盖现有 Higress 完整格式。尤其不要删除现有运维字段。
GateLens 使用 `gatelens_probe_id` 作为主关联键，只在该字段为空时才以 `trace_id`
兜底。网关链路上的插件和 Header 重写规则不能删除
`x-gatelens-probe-id`/B3 trace header。

#### 1.2.1 Istio Telemetry provider 中追加字段

仓库的 `deploy/telemetry.yaml` 引用 `gatelens-envoy` provider，日志格式在 Istio 的
`meshConfig.extensionProviders` 中定义，不在 `Telemetry.spec.accessLogging` 中定义。
在现有同名 provider 的 `envoyFileAccessLog.logFormat.labels` 中追加：

```yaml
meshConfig:
  extensionProviders:
    - name: gatelens-envoy
      envoyFileAccessLog:
        path: /dev/stdout
        logFormat:
          labels:
            # 保留现有日志字段，在此追加
            downstream_remote_address: "%DOWNSTREAM_REMOTE_ADDRESS%"
```

这是配置位置示意，须合并进现有 provider，保留其他字段和其他 provider。
如果该 provider 使用 `logFormat.text` 输出单行 JSON，则在原 JSON 对象中追加
`"downstream_remote_address":"%DOWNSTREAM_REMOTE_ADDRESS%"`；`text` 与 `labels`
不能同时设置。通过 Helm 管理 Istio 时应修改对应 values 源配置。

该值包含下游地址和端口，例如 `10.0.0.1:52068`。它可能受 X-Forwarded-For
信任配置或 PROXY protocol 影响；需要直接连接对端地址时使用
`%DOWNSTREAM_DIRECT_REMOTE_ADDRESS%`。
GateLens 已支持解析 `downstream_remote_address`，无需修改解析代码。
应用源配置后，发起一次经过目标网关的请求，检查新访问日志是否包含该字段。

### 1.3 增加 BBR/EPP Filter State

在同一个 JSON 对象中追加：

```text
"ext_proc_bbr":"%FILTER_STATE(gatelens.filters.http.ext_proc.bbr:TYPED)%",
"ext_proc_epp":"%FILTER_STATE(envoy.filters.http.ext_proc:TYPED)%"
```

同时确保对象中已有且只有一个
`"gatelens_probe_id":"%REQ(X-GATELENS-PROBE-ID)%"`，不要生成重复 JSON key。

例如原格式结尾为：

```text
"ai_log":"%FILTER_STATE(wasm.ai_log:PLAIN)%"}
```

应改成：

```text
"ai_log":"%FILTER_STATE(wasm.ai_log:PLAIN)%","ext_proc_bbr":"%FILTER_STATE(gatelens.filters.http.ext_proc.bbr:TYPED)%","ext_proc_epp":"%FILTER_STATE(envoy.filters.http.ext_proc:TYPED)%","gatelens_probe_id":"%REQ(X-GATELENS-PROBE-ID)%"}
```

上例假定原格式尚未包含 `gatelens_probe_id`；如果已经包含，只追加前两个
`ext_proc_*` 字段。

`TYPED` 是首选，因为 Envoy 会输出完整 `ExtProcLoggingInfo`。不同日志链路可能把它保留为
JSON 对象，也可能编码成 JSON 字符串；GateLens 两种都兼容。Envoy 官方还支持：

```text
%FILTER_STATE(<filter-name>:PLAIN)%
%FILTER_STATE(<filter-name>:FIELD:<field-name>)%
```

若当前 Envoy 不支持 `TYPED`，可以按字段输出，至少保留：

```text
request_header_latency_us
request_header_call_status
request_body_call_count
request_body_total_latency_us
request_body_last_call_status
response_header_latency_us
response_header_call_status
failed_open
grpc_status_before_first_call
received_immediate_response
```

不要把 `request_header + request_body + response_header` 的数量解释为“调用了三次
BBR/EPP”。它表示同一个 ext_proc 流中处理了三类 Envoy 消息。

## 2. 修改 BBR EnvoyFilter

### 2.1 必须修改的字段

一个 HTTP filter 对应一个 Filter State namespace。BBR 和 EPP 都使用默认名称
`envoy.filters.http.ext_proc` 时，请求级统计会写到同一个 key，无法可靠区分。

只把 EnvoyFilter 注入的 BBR `name` 改成唯一名称：

```yaml
name: gatelens.filters.http.ext_proc.bbr
typed_config:
  "@type": type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
```

`typed_config.@type` 仍然是 Envoy 内置 ExternalProcessor，因此修改实例名称不会改变
filter factory。不要修改 InferencePool 控制器生成的 EPP filter 名称，否则可能使其
`typed_per_filter_config` 失配。

### 2.2 BBR 完整配置示例

下面是 BBR `patch.value` 的推荐形态。EnvoyFilter 的 workload selector、listener/filter
chain 匹配条件和插入位置沿用现网配置：

```yaml
name: gatelens.filters.http.ext_proc.bbr
typed_config:
  "@type": type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
  allow_mode_override: true
  grpc_service:
    envoy_grpc:
      cluster_name: outbound|9004||body-based-router.inference.svc.cluster.local
  processing_mode:
    request_header_mode: SEND
    request_body_mode: FULL_DUPLEX_STREAMED
    request_trailer_mode: SEND
    response_header_mode: SKIP
    response_trailer_mode: SKIP
```

修改前后对照：

```diff
- name: envoy.filters.http.ext_proc
+ name: gatelens.filters.http.ext_proc.bbr
   typed_config:
     "@type": type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
```

其余 BBR 的 `grpc_service`、`processing_mode`、超时和 fail-open 策略保持原值。

### 2.3 `stat_prefix` 和 `emit_filter_state_stats`

`stat_prefix: bbr` 是可选项，只用于区分 Envoy `/stats` 中的聚合指标前缀：

```yaml
typed_config:
  "@type": type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
  stat_prefix: bbr
```

它不会修改 Filter State key，也不能代替唯一的 HTTP filter `name`。请求级访问日志仍要读取：

```text
%FILTER_STATE(gatelens.filters.http.ext_proc.bbr:TYPED)%
```

当前 GateLens 已验证的数据面在没有配置 `emit_filter_state_stats` 时就会产生
`ExtProcLoggingInfo`，因此不要求添加：

```yaml
emit_filter_state_stats: true
```

不同 Envoy/Higress 版本的 proto 字段可能不同。只有在目标 Envoy 的配置描述符明确支持、
且实测没有 Filter State 时才考虑添加；不支持该字段的版本会直接拒绝配置。无论是否配置
该开关，BBR/EPP 分流仍由 HTTP filter `name` 决定。

## 3. EPP 与多个 InferencePool

EPP 的全局 ext_proc 配置可能使用 `cluster_name: dummy`，并把 request/response mode
设为 `SKIP`。真正的 EPP gRPC Service 通常由 Route 上的
`ExtProcPerRoute.grpc_service` 覆盖：

```text
InferencePool -> endpointPickerRef -> EPP Service
Route -> typed_per_filter_config -> ExtProcPerRoute.grpc_service
```

因此：

- 集群可以有多个 EPP，每个 InferencePool 对应一个 EPP。
- 一次请求的 `ext_proc_epp` 只表示该 Route 生效的 EPP 处理阶段。
- `ext_proc_epp` 自身不会自动携带 EPP Pod 名称。
- GateLens 结合已观测 `route_name`、per-route gRPC cluster、InferencePool 和
  EndpointSlice 快照解析对应 EPP；不能唯一匹配时必须显示歧义。
- 全局 `dummy` 只表示占位，不是本次实际访问的 EPP。

## 4. 生效与验证

### 4.1 检查 Envoy 配置

配置下发后检查目标网关 config dump，确认 HTTP filter 顺序中同时存在：

```text
gatelens.filters.http.ext_proc.bbr
envoy.filters.http.ext_proc
```

并确认：

- BBR 的 gRPC cluster 指向 `body-based-router`。
- EPP 的 Route `typed_per_filter_config` 指向对应 InferencePool 的 EPP。
- access log format 读取的 key 与上述两个 filter `name` 完全一致。

可使用：

```bash
istioctl proxy-config listeners <gateway-pod> -n <gateway-namespace> -o json
istioctl proxy-config routes <gateway-pod> -n <gateway-namespace> -o json
```

### 4.2 检查实际日志

发起一次 GateLens 探测后读取网关日志：

```bash
kubectl logs -n <gateway-namespace> <gateway-pod> --since=2m
```

成功日志应包含独立对象：

```json
{
  "gatelens_probe_id": "f843679465a4fad49229d655605b164b",
  "route_name": "inference.deepseek-r1-distill-qwen-7b.0",
  "upstream_cluster": "outbound|54321||model.inference.svc.cluster.local",
  "upstream_host": "10.233.105.125:8080",
  "ext_proc_bbr": {
    "request_header_latency_us": 1789,
    "request_body_call_count": 1,
    "failed_open": false
  },
  "ext_proc_epp": {
    "request_header_latency_us": 2934,
    "request_body_call_count": 1,
    "response_header_latency_us": 818,
    "failed_open": false
  }
}
```

判读规则：

| 现象 | 结论 |
| --- | --- |
| 两个字段均存在且阶段计数大于 0 | 本次请求有两个独立 ext_proc 运行时证据 |
| `failed_open=true` | 处理器失败后继续转发；最终 HTTP 200 不代表处理器正常 |
| gRPC status 为 `4` | DeadlineExceeded/处理超时 |
| `received_immediate_response=true` | ext_proc 直接生成了下游响应，不应再假设访问了推理服务 |
| `upstream_host=IP:port` | Envoy 选择或尝试连接该地址，可继续映射 EndpointSlice/Pod |
| `upstream_host=-` | 无实际 Endpoint 证据，只能展示 cluster 和配置快照 |
| BBR/EPP 数据混在一个字段 | 两个 HTTP filter 仍同名，或日志读取了错误 key |

## 5. 回滚

出现 Envoy 配置拒绝或流量异常时：

1. 回滚 BBR EnvoyFilter 到上一个已验证版本。
2. 保留基础 access log 字段；删除不存在的 Filter State formatter 不影响业务转发。
3. 检查网关 config dump 和控制面 rejected 配置事件。
4. 不要通过修改 EPP 控制器生成配置来临时修复 BBR 命名问题。

访问日志只读观测不应改变 `failure_mode_allow`、message timeout 或处理模式；这些都是
业务流量策略，必须单独评审。

## 6. 官方参考

- [Istio：Envoy Access Logs](https://istio.io/latest/docs/tasks/observability/logs/access-log/)
- [Istio MeshConfig API](https://istio.io/latest/docs/reference/config/istio.mesh.v1alpha1/)
- [Envoy External Processing Filter：Access Log Fields](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter)
- [Envoy Substitution Formatter：FILTER_STATE](https://www.envoyproxy.io/docs/envoy/latest/configuration/advanced/substitution_formatter)
