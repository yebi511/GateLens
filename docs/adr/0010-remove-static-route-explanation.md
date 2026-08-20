# ADR 0010：移除静态请求路径预检

- 状态：Accepted
- 日期：2026-08-13

## 背景

配置快照只能表达 Gateway、Route、Service 和 Endpoint 的声明关系。WasmPlugin 可以在请求运行时依据 Header、Body、动态元数据、外部处理器响应或缓存状态改写 Path、Host 和其他路由输入，也可以清理 Envoy route cache 后触发重新选路。GateLens 无法通用执行第三方 Wasm 或复现其外部状态。

## 决策

移除“配置预检”页面、`POST /api/v1/route-explanations` API 和内部静态请求匹配解释器。配置拓扑继续用于资源关系与健康核验，但不再输出最终 Route/upstream 的预测结论。用户发起请求时只提供实时探测，并以网关访问日志等运行时证据展示实际路径。

## 后果

产品不会再把标准路由规则的局部推演误呈现为真实项目的最终路径。实时探测会产生真实流量，因此仍由 `GATELENS_ENABLE_ACTIVE_PROBES` 显式开启，并受入口发现、请求大小、超时和认证信息不持久化等限制。
