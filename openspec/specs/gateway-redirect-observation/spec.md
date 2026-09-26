# 网关内部重定向观察规格

## Purpose

为一次实时探测关联网关访问日志，识别已观测的内部重定向，并在所属网关运行时内保留各次尝试及后续证据。系统明确区分日志关联、重定向标记、尝试顺序与最终响应归属，避免将中间失败隐藏为一次成功或将关联记录误当作确定的执行链。

## Requirements

### Requirement: 使用有效探测标识关联日志

系统 MUST 将有效 `gatelens_probe_id` 与本次探测 ID 精确相等的访问日志关联到同一次探测。空值、纯空白和 `-` MUST 视为未提供标识；仅在未提供有效 probe ID 时允许使用有效且匹配的 trace ID 兜底，并明确报告兜底依据。非空有效 probe ID 不匹配时 MUST 拒绝该日志，即使 trace ID 相同；`request_id` MUST NOT 作为探测关联键。

#### Scenario: 重定向前后共享探测标识
- **WHEN** 两条访问日志共享本次有效 probe ID，但 Route 和 upstream cluster 不同
- **THEN** 两条日志均关联本次探测，不因 Route 改变而拆成两次探测

#### Scenario: 占位标识与有效 trace 兜底
- **WHEN** 日志的 probe ID 为 `-`，trace ID 有效并匹配本次探测
- **THEN** 日志以 trace ID 关联，并注明关联依据为 trace 兜底

#### Scenario: 所有关联标识均为占位值
- **WHEN** 日志的 probe ID 和 trace ID 均为 `-`，只有 `request_id` 与其他日志一致
- **THEN** 系统不将该日志关联到本次探测

#### Scenario: 有效 probe ID 冲突
- **WHEN** 日志 probe ID 为另一次探测的有效值，但 trace ID 匹配本次探测
- **THEN** 系统拒绝该日志

### Requirement: 依据响应详情识别内部重定向

系统 MUST 将响应详情恰为 `internal_redirect`，或以 `internal_redirect:` 开头且包含非空扩展详情的访问日志，标为已观测内部重定向记录，并保留完整原值。系统 MUST NOT 依赖固定的 `ai_usage_via_upstream` 后缀，也 MUST NOT 仅凭状态码、模型变化、多条同 ID 日志或正文中出现关键词判断重定向。

#### Scenario: 标准 Envoy 标记
- **WHEN** 日志的响应详情为 `internal_redirect`
- **THEN** 系统识别为内部重定向记录

#### Scenario: 扩展标记
- **WHEN** 日志的响应详情为 `internal_redirect:ai_usage_via_upstream` 或 `internal_redirect:other_filter_detail`
- **THEN** 系统均识别为内部重定向记录，并保留原始扩展详情

#### Scenario: 普通失败与相似字符串
- **WHEN** 日志为 HTTP 400 且详情为 `via_upstream`、`ai_usage_via_upstream`、`not_internal_redirect` 或 `internal_redirect_failed`
- **THEN** 系统不将该记录标为内部重定向

### Requirement: 在网关运行时范围内组织尝试记录

系统 MUST 保留网关段，在同一次探测的所属网关运行时内组织记录，并保留可用的原始请求开始时间、下游连接身份、日志来源顺序及关联依据。系统 MUST NOT 跨网关或跨 Pod 将记录直接拼接成内部重定向链；Route、Provider、模型与 upstream cluster 的变化 MUST NOT 单独切断尝试分组。

有明确重定向标记且同一运行时的记录顺序和请求身份无冲突时，系统 MUST 输出已关联的后续尝试关系。身份缺失、来源混合或存在多个后续候选时 MUST 输出关系未确认及原因；多条日志没有重定向标记时 MUST 保留为同请求关联记录，不报告确定的重定向链。

#### Scenario: 同一 Pod 内 Route 切换
- **WHEN** 同一网关 Pod 的两条记录共享有效 probe ID、原始开始时间与下游连接，第一条有重定向标记，第二条在同一日志源随后出现
- **THEN** 两条记录属于同一网关内尝试分组，并报告第一条继续到第二条的关系

#### Scenario: 两个网关共享标识
- **WHEN** 两个网关均输出本次 probe ID 的访问日志
- **THEN** 系统保留两个网关段，不仅凭 ID 相同增加内部重定向次数或建立两条记录间的 redirect 关系

#### Scenario: 同一网关不同 Pod 的记录
- **WHEN** 同一网关多个 Pod 均输出本次 probe ID 的记录
- **THEN** 系统分别组织运行时记录，不以全局时间排序建立跨 Pod 的 redirect 链

#### Scenario: 多条记录缺少重定向标记
- **WHEN** 同一运行时存在两条同 probe ID 的普通访问日志
- **THEN** 系统保留两条记录，报告关系未确认，不以记录数量减一计算重定向次数

### Requirement: 相同开始时间不丢失顺序证据

系统 MUST 保留同一日志源内的记录顺序，即使多个重定向记录的 `start_time` 相同。采集时间、不同来源的读取先后或缺失时间时生成的展示时间 MUST NOT 作为确定的尝试因果依据。无法确认顺序时 MUST 不输出确定的尝试先后关系。

#### Scenario: 相同 start_time 的两个尝试
- **WHEN** 同一日志源先输出重定向记录再输出普通记录，两者 `start_time` 完全相同
- **THEN** 系统保留两条记录及日志源顺序，不按相同时间去重

#### Scenario: 混合来源且顺序不可比
- **WHEN** 多条候选记录来自不同日志源，且没有足以确认后续关系的证据
- **THEN** 系统保留所有记录，并报告顺序未确认

### Requirement: 有界采集重定向后续记录

系统 MUST 在已有关联日志后继续给予日志写入有界的采集机会，不得因读到第一条重定向记录就结束。系统 MUST 累计保留已采集证据，重读相同来源的同一日志 occurrence 不得重复计数；同一来源中实际存在的多条相同记录 MUST 保留各自 occurrence。

系统 MUST 在采集窗口结束、取消或读取失败时返回已采集记录及明确的采集状态；存在重定向记录但缺少可关联后续记录时 MUST 返回证据缺口。采集 MUST 仅执行只读日志操作，不得额外发送探测请求、跟随客户端 HTTP 重定向或无限等待。

#### Scenario: 后续记录延迟写入
- **WHEN** 首次读取仅有重定向记录，后续终止候选在采集窗口内延迟写入
- **THEN** 结果包含两条记录，且仅发送过一次主动请求

#### Scenario: 重读与真实重复记录
- **WHEN** 多次采集重读同一记录，且日志源内另有两个内容完全相同的实际 occurrence
- **THEN** 重读不增加计数，两个实际 occurrence 均保留，不因指纹相同合并为一个

#### Scenario: 窗口结束仍缺少后续证据
- **WHEN** 采集窗口结束时只存在一条内部重定向记录
- **THEN** 系统返回该记录，报告采集窗口已结束及后续尝试证据缺失，不推断重定向后成功

#### Scenario: 采集过程中取消或读取失败
- **WHEN** 已采集一条记录后上下文取消或日志读取失败
- **THEN** 系统保留已有记录，返回对应采集状态及缺口

### Requirement: 分离重定向证据与最终响应归属

系统 MUST 独立报告已观测重定向记录数量、尝试关系状态与终止尝试候选；数量只能描述已采集证据，不得宣称未输出的内部过程已完整。HTTP 请求结果 MUST 以源 Agent 实际响应或请求错误为准，MUST NOT 由某条网关日志覆盖。

系统 MUST NOT 将全局排序后的最后一条记录直接标为最终上游。局部唯一终止候选、全请求最终响应归属以及跨网关末端归属 MUST 区分；多个终止候选、状态冲突、采集缺口或跨网关因果缺失时 MUST 报告归属未确认。

#### Scenario: 中间 400 与最终 200
- **WHEN** 源 Agent 实际收到 HTTP 200，同一运行时的关联记录先为带 redirect 标记的 400，后为普通 200
- **THEN** 请求结果保留 HTTP 200，400 保留为中间响应，200 作为该运行时的终止候选

#### Scenario: 多个终止候选
- **WHEN** 同一次探测关联了多个无法区分归属的非 redirect 记录
- **THEN** 系统保留候选，报告最终尝试归属未确认，不选择时间最大的记录

#### Scenario: 旧 Agent 未提供采集状态
- **WHEN** 结果仅有旧版 `hops` 和 `segments`，缺少新增采集状态
- **THEN** 系统保留既有记录，报告内部过程观察状态未知，不认定尝试完整

### Requirement: 提供固定 AI 路由摘要字段

系统 MUST 仅从可解析为对象的 `ai_log` 提取固定路由摘要字段 `provider`、`request_model`、`upstream_model` 和 `response_model`，并保留它们原始业务含义。摘要 MUST NOT 将 `question`、`answer_no_stream`、任意嵌套业务字段或 `downstream_disconnected` 作为路由成功失败的依据。无法解析或字段缺失时 MUST 不影响 Route、状态码和 redirect 观察结果；独立 `ai_usage_record` 日志 MUST NOT 计为访问尝试。

#### Scenario: 有 Provider 与模型字段
- **WHEN** 访问日志的 `ai_log` 包含上述字段及问题、回答文本
- **THEN** 系统提供固定路由摘要，不将问题、回答加入新增摘要字段

#### Scenario: ai_log 格式不支持
- **WHEN** `ai_log` 为空、为非 JSON 文本、非法 JSON 或 JSON 数组
- **THEN** 系统仍正常提供该访问记录的 Route、响应码和 redirect 观察结果，AI 摘要保持缺失

#### Scenario: usage 与 access log 成对出现
- **WHEN** 容器日志包含两条独立 `ai_usage_record` 和两条对应访问日志
- **THEN** 系统只将两条访问日志作为尝试记录，既不创建四次尝试也不依据 usage 的 outcome 推断 HTTP 状态
