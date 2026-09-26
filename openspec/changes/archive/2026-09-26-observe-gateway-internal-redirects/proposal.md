## 为什么需要变更

同一次探测可能在 Higress 内部先访问模型 A，失败后重定向到模型 B，并产生多条共享 `gatelens_probe_id`、甚至共享 `start_time` 的访问日志。当前实时探测没有表达网关内尝试关系，采集端遇到第一条匹配日志即结束，前端又将排序后的最后一条日志视为最终上游，可能隐藏中间失败或错误归属最终响应。

## 变更内容

- 以有效的 `gatelens_probe_id` 关联一次探测，保留跨网关分段，在所属网关运行时内组织尝试记录；相同 ID 本身不证明发生重定向。
- 识别标准 Envoy 的 `internal_redirect` 和已观测扩展格式 `internal_redirect:<详情>`，保留原始响应详情，不依赖 `ai_usage_via_upstream` 或模型切换推断。
- 在有界采集窗口内继续收集后续日志，累计保留已读记录，避免重定向后的终止记录延迟写入时被遗漏。
- 扩展探测结果，提供网关内尝试分组、已观测重定向数量、终止尝试候选、排序依据与证据缺口；区分最终 HTTP 结果和内部过程证据。
- 前端采用“请求结果 → 网关段 → 网关内尝试链”的展示层级，明确中间响应、内部重定向和最终尝试归属不确定的情况。
- 从可解析的 `ai_log` 中提取 Provider 和模型等固定路由字段用于摘要，保留 ext_proc 与既有 Route/upstream 详情展示。
- 取消“最后一条日志即最终上游”的判断；缺少有效地址时显示“实际地址未记录”，日志耗时不相加替代 Agent 实测总耗时。

## 能力范围

### 新增能力

- `gateway-redirect-observation`：探测日志关联、网关内重定向识别、后续记录采集、尝试关系与终止候选的证据表达。
- `probe-redirect-presentation`：实时探测的请求摘要、网关内尝试链、回退提示、最终上游归属与未知状态展示。

### 修改的既有能力

无。当前仓库尚无 `openspec/specs/` 主规格；上述能力作为新增 delta 规格建立行为契约，兼容现有单记录探测与跨网关证据分段。

## 影响范围

- 后端：`internal/observed/higress_log.go`、`internal/kube/probe.go`、`internal/domain/types.go`、`internal/federation/store.go` 及附近测试。
- 前端：`frontend/src/types.ts`、`frontend/src/views/ProbeView.vue`、`frontend/src/styles.css`，以及必要的展示数据转换模块。
- 文档：主动探测说明与前端设计说明需要同步新增语义；本次提案阶段不改动这些项目文档。
- 接口：保留探测入口及既有 `hops`、`segments`、`responseCode`、`durationMillis` 字段，以可选字段增加观察结果；旧 Agent 未提供采集状态时，Server 显式返回未知。
- 无新增外部服务、依赖、网关配置自动修改或额外主动请求。不实现通用重试识别、usage 日志关联、Span 父子关系或完整跨网关分支图。
