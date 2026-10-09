## Why

实时探测把 Agent 局部执行结果、Server 联邦聚合结果、日志记录和尝试关系放进同一个模型；`hops`、采集状态、缺口及多种摘要重复表达，前端还需要通过 Group 中的 ID 再查找记录。需要在保留重定向证据与归属边界的前提下，减少协议字段和各层理解成本。

## What Changes

- **BREAKING**：前端、Server 与 Agent 同步升级探测协议，不提供旧版探测结果或旧 Agent 的兼容读取、双写及回退适配；API 路径保持不变。
- `ProbeExecution` 以 `segments[].hops` 作为日志记录的唯一存储位置，移除顶层重复的 `hops`、`collection`、`logSource`，移除可推导的 `evidenceComplete` 和 `redirectSummary`。
- Agent 使用专门的局部探测结果，独立于 Server 返回的 `ProbeExecution`；实际 HTTP 结果只由源 Agent 提供。
- 完整尝试分组保留为 Server 内部分析结构；公开 Segment 只提供记录上的请求上下文标识、已确认连接、关系状态与局部终止记录引用。
- 用有明确作用域和原因码的问题列表替代多层复制的 `gaps` / `reasons`；采集状态和尝试关系状态继续独立表达，显示摘要按需计算。
- 前端直接消费分段记录及后端确认的关系，保留中间失败、多个运行实例隔离、采集异常、未知归属、AI 摘要及 ext_proc 详情。
- 对探测流程中取值固定的状态、关联方式、置信度、命令种类、问题作用域和原因码统一使用具名枚举；Go 以具名类型和常量表达，TypeScript 以对应具名字面量联合类型表达，协议保留可读字符串，并校验非法取值。

## Capabilities

### New Capabilities

- `probe-result-contract`：定义局部执行与聚合结果的职责、唯一数据归属、结构化问题，以及同步升级的精简探测协议。

### Modified Capabilities

- `gateway-redirect-observation`：尝试分组改为内部分析，公开确认关系与上下文标识；取消旧 Agent 兼容要求，保留有效标识、来源顺序和最终归属约束。
- `probe-redirect-presentation`：从精简结果直接展示记录及关系，按需生成摘要；取消旧结果回退，保留窄屏、键盘访问和未知状态表达。

## Impact

- 后端：`internal/domain/types.go`、`internal/source/source.go`、`internal/kube/probe.go`、`internal/kube/probe_logs.go`、`internal/observed/probe_attempts.go`、`internal/federation/store.go`、`internal/agent/agent.go`、`internal/api/handler.go`、`internal/demo/store.go` 及相关测试。
- 前端：`frontend/src/types.ts`、`frontend/src/utils/probePresentation.ts`、`frontend/src/views/ProbeView.vue`、API 类型、探测样例及验收入口。
- 文档：`docs/03-domain-model.md`、`docs/10-observed-traffic-path.md`、`docs/12-probe-redirect-acceptance.md` 与探测相关 OpenSpec 规格。
- 协议：`POST /api/v1/probes`、`GET /api/v1/probes/{id}` 返回体，以及 Agent 命令结果中的 `probe`；探测请求表单与命令种类保持现有行为。
- 不新增依赖；不调整入口发现、候选网关关联规则、真实请求次数、采集时间窗、客户端重定向策略或跨网关最终上游确认能力。当前已有未提交实现修改作为规划基线，本变更的提案阶段仅新增规划文件。

