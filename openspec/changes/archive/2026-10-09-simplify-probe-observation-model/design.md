## Context

动机与范围见 `proposal.md`。本变更跨 Agent、Kubernetes 采集、联邦聚合、API、demo 和前端，且涉及协议调整，因此需要设计文档。

当前 `source.ProbeExecutor` 与 `source.ProbeStore` 都返回 `ProbeExecution`；`ExecuteProbe` / `ObserveProbe` 返回局部 `hops`，Server 再包装 Segment，`EnrichProbe` 又重建顶层记录并复制缺口。`ProbeAttemptGroup` 是公开领域结构，前端通过 `hopIDs` 回查记录，并重复识别重定向。当前采集器和分析器还通过问题说明中的“连续性”“顺序”关键词判断证据是否可排序。

`internal/observed/probe_attempts_test.go`、`internal/kube/probe_logs_test.go`、`internal/federation/store_test.go` 与前端探测样例已经覆盖合成重定向、真实重复 occurrence、跨 Pod、来源不明、多个候选、延迟日志、取消和读取失败。现有规格包含旧 Agent / 旧结果兼容，本次 delta 显式替换这些要求；有效新协议中的元信息不足仍须保留记录并表达未知。

用户确认前端、Server、Agent 同步升级并允许协议调整。工作区已有类型枚举和拓扑关联等未提交修改，实施应基于现有内容增量修改，不回退或覆盖这些工作。

## Goals / Non-Goals

**Goals:**

- 让每份日志和每个问题有唯一持有位置，减少冗余 JSON 字段和复制逻辑。
- 让 Agent 只返回本地事实，Server 独立负责联邦关联和最终归属。
- 把完整分组算法保留在后端，只公开展示需要的上下文及确认关系。
- 使用稳定原因码驱动判断，避免说明文本决定行为。

**Non-Goals:**

- 不将网关重入等实际未确认情形强制合成一条尝试链，不扩大最终上游确认范围。
- 不改变探测输入、入口发现、候选关联规则、请求次数、采集时间窗或 HTTP 客户端不跟随重定向的行为。
- 不重构所有 `ObservedHop` 日志字段，不删除来源、地址和原始时间等分析证据，不更改 AI 与 ext_proc 的业务语义。
- 不增加跨版本兼容适配、持久化历史探测、Span 因果链或额外依赖。

## Decisions

### 1. 局部结果和聚合结果使用不同类型

引入局部 `ProbeAgentResult`，由 `ProbeExecutor`、Kubernetes Store 和 `AgentCommandResult.Probe` 使用；`ProbeStore` 和 API 继续使用精简的 `ProbeExecution`。

| 结构 | 字段归属 |
| --- | --- |
| `ProbeAgentResult` | `SchemaVersion` 固定 2；`ProbeID`、`TraceID`、`ClusterID`、本地 `GatewayID`；`StartedAt`、`CompletedAt`；可选 `HTTP`；`Hops`、`Collection`、`Issues` |
| `ProbeHTTPResult` | `Method`、`Target`、`ResponseCode`、`ResponseBytes`、`DurationMillis`、`Error`；只由真实请求命令产生 |
| `ProbeExecution` | `SchemaVersion` 固定 2；保留现有探测身份、源网关、请求摘要、执行状态、时间、HTTP 响应、执行错误、联邦快照、`Segments`、`FinalResponseHopID`、`FinalUpstreamHopID`；新增顶层 `Issues` |

Server 从源命令的 `HTTP` 映射现有顶层响应字段，其他网关的观测结果不参与覆盖。请求失败但有日志时，同时保留请求错误和该段证据。命令调度失败使用现有命令错误路径；不在问题列表再复制同一执行错误。非致命执行问题，例如响应读取上限提示，放在顶层 `Issues`。

局部结果必须按命令上下文验证版本、探测/trace 标识、集群及本地网关归属；真实请求命令必须有 `HTTP`，只读观测命令不得有 `HTTP`。源命令协议错误使探测失败；其他网关协议错误成为该段采集失败问题，不覆盖已取得的源 HTTP 结果。对源结果 `StartedAt` 的 Server 排队起点与 Agent 执行起点沿用既有口径；聚合完成时间在全部关联处理后写入，HTTP duration 仍只计实测请求及响应读取。

替代方案：继续复用 `ProbeExecution` 但约定 Agent 不填某些字段。该方案仍让局部代码依赖联邦字段，无法从类型表达职责，因此不采用。

### 2. 每个 Segment 是日志与采集结果的唯一持有者

精简后的核心形状：

```text
ProbeExecution
  +-- HTTP result fields
  +-- Issues[] (execution)
  +-- Segments[]
  |     +-- Hops[] (each has ContextID)
  |     +-- Collection (State, CompletedAt)
  |     +-- RelationState
  |     +-- Links[] (From, To)
  |     +-- LocalTerminalHopIDs[]
  |     +-- Issues[] (collection, relation, inference)
  +-- FinalResponseHopID
  +-- FinalUpstreamHopID
```

Segment 保留 `Index`、网关/集群身份、配置快照、日志来源、Transport / Destination 及候选推断依据/置信度。把 Segment 的 `ObservedAt` 改为 `SnapshotObservedAt`，明确它是配置快照时间；Hop 的时间字段此次保留，缺失请求开始时间生成的展示时间继续不能参与因果判断。

移除公开字段：

| 旧字段 | 新归属或计算方式 |
| --- | --- |
| 顶层 `Hops` | 按需展开 `Segments[].Hops` |
| 顶层 `Collection`、`LogSource` | 从源网关 Segment 读取 |
| 顶层 `EvidenceComplete` | 移除完整性布尔值；显示已观测/候选网关数和独立采集/关系状态 |
| 顶层 `RedirectSummary` | 重定向数来自 Hop 标记，已关联数来自 Segment Links；过程提示按状态和问题推导 |
| Segment `State`、`Evidence` | 是否有日志由 Hops 判断；采集失败由 Collection 与问题表达；候选依据独立保留 |
| 各层 `Gaps`、Collection `Reasons` | 单一所属位置的 `Issues` |
| Segment `AttemptGroups` | 后端内部分析结果，向外投影为上下文、连接、关系状态和局部终止引用 |

空记录数组始终输出 `[]`，不使用缺字段暗示兼容模式。运行中 Segment 可以尚未生成；已结束的 Segment 必须有明确 Collection 状态，命令尚未得到证据时可为 unknown，明确读取/协议失败为 read-error，取消为 cancelled。

替代方案：仅删除顶层 Hops，保留其他重复状态。这只能减少序列化大小，仍需要维护多处缺口和状态，不足以解决主要理解成本。

### 3. 完整分组保留为内部算法，公开最小关系

将 `ProbeAttemptGroup` 的内部等价类型收回 `internal/observed`。分组键仍为运行时身份（缺失时按日志来源与 Pod 降级）、原始请求开始时间、下游远端地址、本地地址。保留现有身份检查、来源序号可比性、重定向标记边界、多个终止候选与重入歧义规则。

Server 分析后给每条 Hop 写入 `ContextID`，该标识只在 Segment 内有效；ID 分配按结果中首次出现上下文确定，同一结果重复分析时不改变标识或前缀。Hop ID 仍保证整次探测唯一，连接与终止引用必须指向本 Segment 现有记录。

向 Segment 投影：

- `Links`：所有内部上下文中已确认的 redirect 后继连接。跨上下文、跨运行时、跨网关连接均禁止；未知或歧义上下文不产生连接。missing-next 上下文可以保留前面已经确认的局部连接，但不补齐缺失后继。
- `LocalTerminalHopIDs`：各上下文在唯一终止候选、关系明确、采集 settled 且无相关采集问题条件下确认的终止记录。非 redirect 的其他记录可以作为候选展示，无需重复传候选 ID 列表。
- `RelationState`：复用现有 `ProbeAttemptRelationState` 的语义；汇总优先级为 ambiguous > unconfirmed > missing-next > linked。无记录为 unconfirmed，不能从空记录推出关系明确。
- 上下文相关的问题带 `ContextID`。每个未知、歧义或缺后继上下文必须有问题，避免只得到段级状态却无法定位。段级状态用于概览，不能删除其他上下文已确认的 Links。

前端只按 ContextID 分栏、按已有 Links 展示连接；有连接的链按连接编号，唯一已确认局部终止的单记录可标为单次尝试，其他记录仍用记录名称。不同链或未连接记录保持隔离，客户端不得按日志时间、响应码或模型变化补造顺序。默认展开条件继续由 redirect 标记或 HTTP 错误决定。

替代方案：删除分组及其身份判断，直接把 Segment.Hops 视为一条尝试链。这会把多 Pod、网关重入或来源不明记录误连，违反现有证据边界，因此不采用。保留公开 Group 但改名只改善命名，无法减少前端 ID 间接索引，因此也不采用。

### 4. 问题统一格式，状态判断使用原因码

新增 `ProbeIssue`：`Scope`（execution / collection / relation / inference）、`Code`、`Message`、可选 `ContextID` 与 `HopID`。作用域标识问题的产生阶段，存储位置只有顶层执行问题和对应 Segment 问题两种，不为归总另存副本。

原因码最小集合及作用：

| 原因码 | 作用域与含义 |
| --- | --- |
| `response-limit-exceeded` | execution，保留现有响应读取上限提示 |
| `log-source-rotated`、`log-window-truncated`、`log-order-unconfirmed` | collection，无法证明来源连续或顺序，阻止依赖来源顺序的确认 |
| `collection-window-ended`、`collection-cancelled`、`log-read-error` | collection，说明采集结束原因 |
| `no-matching-log`、`agent-unavailable`、`probe-protocol-unsupported` | collection，无匹配记录、命令不可用、局部结果协议不支持 |
| `request-identity-incomplete`、`attempt-order-unconfirmed` | relation，身份或可比顺序不足，包含 ContextID |
| `redirect-next-missing`、`terminal-ambiguous` | relation，后继缺失或终止候选/重入歧义，包含 ContextID |
| `gateway-evidence-missing` | inference，配置或 upstream 关联保留的候选网关缺运行时证据 |

同位置按 `(Scope, Code, ContextID, HopID)` 去重。不同网关的相同原因码是不同问题，汇总时保留网关身份。顶层主执行 Error 不再复制为 Issue；InferenceBasis 保留具体匹配依据，问题表达缺证据的事实。

`probeLogCache.canSettle` 与尝试分析通过明确原因码判断连续性和顺序，不扫描 Message。集合中的不同原因可能同时存在，例如窗口结束和后继缺失分别保留。Collection 仅保存 State / CompletedAt，原因位于 Segment Issues；内部采集结果使用小型载体一并返回 Hops、Collection、Issues，避免散落多返回值与字符串原因。

替代方案：统一为一个大字符串数组。它不能稳定区分采集与关系问题，仍会让说明文本参与业务判断，因此不采用。

### 5. 派生字段由展示层计算，确认结论由 Server 给出

后端统一在日志解析时填充 `InternalRedirect` 和固定 `AIRouting` 摘要；公开新协议要求消费方直接使用解析结果。移除 Server 的重复 AI 摘要解析与前端对原始响应详情的重定向重判定，原始详情继续用于展示。`ObservedHop` 其他字段此次不做成批重命名或重新嵌套。

前端 `probeSummary` 从 Segment 计算网关/记录/redirect/连接数量。过程提示优先表达歧义，其次身份或顺序未知，再表达采集失败、窗口结束、候选缺日志或 missing-next 等缺口；只有展示范围内的采集和关系均满足条件时才能使用已关联措辞，始终不宣称全部内部过程已输出。该概览不作为后端最终归属判断输入。

Server 继续负责 `FinalResponseHopID` 与 `FinalUpstreamHopID`：源请求完成、无执行错误、源网关只有一个明确上下文、采集稳定、无相关问题、所有记录 probe-id 精确关联且终止码与实际 HTTP 码一致，才确认最终响应；仅一个 Segment 且无相关缺口时才确认最终上游。保留当前保守策略，不因新的 LocalTerminalHopIDs 数量为一就绕过单上下文约束。多网关不补造 Span 因果。

替代方案：把最终归属交给前端从连接推断，会重复关键规则并造成不同消费者结论不一致，因此不采用。

### 6. 通过明确版本同步切换，不做迁移适配

API 和 Agent 局部结果都带 `schemaVersion: 2`；未版本化旧协议视为不支持。Server 在接收 probe 结果时校验，前端 API client 在返回类型对象前校验；错误结果不得进入普通空日志展示路径。命令 Kind、请求输入及 API 路径不变，不增加能力协商或双协议分支。

替代方案：保留旧字段双写和前端 fallback。用户已明确选择同步升级，该方案会维持本次要删除的复杂度，因此不采用。

### 7. 固定取值统一使用具名枚举

用户要求可用枚举的状态等字段全部用枚举表达。范围是本次探测流程及直接共享的字段，不扩大为整个仓库的类型重构。先保留计划删除的冗余字段删除决定，不为已删除字段重新建立枚举。

Go 使用具名 string 类型和同类型常量；字段、函数参数和返回值都使用该类型，生产代码的赋值、比较和 switch 使用具名常量，不只定义常量后仍把字段声明为 string。已有 `ProbeCollectionState`、`ProbeAttemptRelationState` 直接复用。TypeScript 使用同名或语义对应的具名字面量联合类型；需要运行时校验、标签映射或选项列表时，由集中常量对象/列表派生类型，避免在各组件重复抄取值。各枚举常量及类型注释说明业务含义，JSON 使用现有可读字符串，不使用 iota 数字作为协议值。

| 字段/用途 | 具名类型 | 有效取值 |
| --- | --- | --- |
| Execution.State | `ProbeExecutionState` | running、completed、failed |
| Collection.State | `ProbeCollectionState` | settled、window-ended、cancelled、read-error、unknown |
| Segment.RelationState 与内部上下文关系 | `ProbeAttemptRelationState` | linked、unconfirmed、missing-next、ambiguous |
| Issue.Scope | `ProbeIssueScope` | execution、collection、relation、inference |
| Issue.Code | `ProbeIssueCode` | 第 4 节原因码集合，每项有具名常量 |
| Hop.Correlation | `ProbeCorrelation` | probe-id、trace-id；可选字段缺省不表示精确匹配 |
| Segment.InferenceConfidence 及关联规则结果 | `ProbeInferenceConfidence` | high、medium、low、ambiguous |
| 内部上下文 OrderBasis | `ProbeOrderBasis` | source-sequence、unavailable |
| AgentCommand.Kind | `AgentCommandKind` | envoy-config、probe-http、probe-observe；复用现有常量名并补上具名类型 |
| ExtProcObservation.Outcome | `ExtProcOutcome` | success、timeout、error、fail-open、immediate-response、unknown |
| Execution.SnapshotConsistency 及直接共享的拓扑一致性来源 | `SnapshotConsistency` | single-cluster、waiting-for-agents、consistent-window、remote-unavailable、time-skew |
| 保留的 Hop.Confidence | `ObservationConfidence` | observed；不与推断置信度共用枚举 |
| 归一化后的探测输入/命令/HTTP 结果 Method | `ProbeHTTPMethod` | GET、HEAD、POST、PUT、PATCH、DELETE、OPTIONS；输入空值仍按原规则默认 GET |
| ProbeEntry.Scheme | `ProbeScheme` | http、https |

共享字段使用同一类型贯穿生产者、聚合者和消费者；例如快照一致性若从 Topology 拷贝，就同步类型化对应来源及 demo，而不是在探测处盲目强制转换。内部规则结果、缓存和分组中可枚举字段也使用相同枚举，不在内部继续散落裸字符串。前端派生的请求显示状态、过程显示状态和记录角色同样定义具名类型，记录角色使用稳定标识再映射中文标签，不把中文文案作为判断条件。

ID、时间、地址、模型、Provider、来源路径、说明文本、ContentType、原始日志的 Method / Protocol / ResponseFlags / ResponseCodeDetails / GRPCStatus / ext_proc ReasonCode，以及来自可扩展配置的 Transport，保持原始值类型；这些是开放输入，不为了枚举限制未知但有效的业务事实。HTTP 响应码保持整数，布尔事实保持 bool，schemaVersion 使用具名版本常量而非状态枚举。

具名 string 类型本身不阻止 JSON 接收任意字符串。Agent 结果消费和前端 API client 除版本外还验证固定取值字段：必填状态缺失、非法枚举及非法问题码明确报非法结果；允许 unknown 的字段只能按已定义语义生成 unknown，不把非法值静默升级为成功或已关联。可选字段未提供与显式 unknown 分开处理，缺少身份和关联依据仍阻止不符合条件的确认。HTTP 输入的方法和 Scheme 使用原有输入校验路径，原始日志不因开放字段出现新值而丢弃。

替代方案：仅把字符串提取为无类型常量，或只在 TypeScript 内联联合类型。前者不能表达字段所属状态域，后者容易在多个消费者间漂移，因此不采用。为每个日志原始值创建封闭枚举也会损失可扩展证据，因此只枚举系统掌控的固定集合。

## Risks / Trade-offs

- [同步部署期间版本不匹配] -> 明确版本检查；停发探测并一起升级或回滚三端，不做静默降级。
- [段级关系状态不能完整表达多个上下文] -> 保留每条记录 ContextID、可靠 Links 和带上下文引用的问题；验证可靠与未知上下文共存时不丢证据。
- [移除重复字段后遗漏消费者] -> 覆盖 source 接口、API、Agent、demo、前端合成样例与文档；断言序列化结果不出现旧字段。
- [结构化问题增加一个小类型] -> 用该类型替换四处字符串缺口以及关键词判断，限制原因码集合，不扩展为通用诊断框架。
- [当前未提交修改与实施重叠] -> 基于工作区现有枚举和关联规则做增量修改；验证拓扑回归，避免混入无关重构。
- [完整因果仍不可知] -> 保留原始分析证据和现有归属约束，关系未确认时保留日志、不连接；简化协议不提升证据置信度。

## Migration Plan

1. 同一实现变更内完成领域协议、Agent、Server、demo、前端、合成数据和文档更新；不交付部分切换的发布版本。
2. 把旧协议正向兼容测试替换为版本拒绝测试；保留新协议缺运行时/顺序元信息的未知证据测试，两者不能混为一谈。
3. 完成 Go 全量测试、前端 probe 与拓扑测试、类型检查和生产构建；用真实 ProbeView 的合成入口验证桌面、375 px 窄屏及键盘访问。
4. 发布前停止发起新探测，等待已有请求完成，再同步部署前端、Server 与所有 Agent。当前探测结果保存在 Server 内存，不提供旧内存结果或外部缓存结果转换。
5. 回滚时三端整体回滚；重启后的探测重新发起，不保留跨版本命令或结果。对外说明 API 路径相同但响应和 Agent 探测载荷为破坏性变更。

