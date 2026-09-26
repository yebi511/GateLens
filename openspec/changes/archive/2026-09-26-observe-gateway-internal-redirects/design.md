## 背景与约束

变更动机见 [提案](proposal.md)。本设计覆盖 Agent 日志采集、Server 观察聚合和 Vue 实时探测页面，属于跨模块行为与数据结构变更，因此需要设计文档。

### 当前实现

- `internal/kube/probe.go` 的 `ExecuteProbe` 在 HTTP 请求结束后最多进行 5 次日志读取，`ObserveProbe` 最多读取 8 次；两者均在出现任意匹配日志时提前退出。远端观察由 Server 在源请求结束后派发，不另外发送 HTTP 请求。
- `internal/observed/higress_log.go` 已按 probe ID 优先、trace ID 兜底关联日志，尚未将 `-` 视作缺失标识。`ObservedAt` 来自 `start_time`，缺失时生成当前时间，缺少能够区分业务时间与展示时间的元数据。
- `internal/federation/store.go` 保留各网关的 `ProbeSegment`，但将全体 `Hops` 按 `ObservedAt` 排序；`EvidenceComplete` 主要表达显示的网关段是否具备访问日志。
- `ProbeView.vue` 在网关段内平铺访问记录，直接取全局 `hops` 的最后一条作为 `finalHop`；原始 `ai_log` 直接出现在详情中。
- 现有 Go 测试覆盖单记录、trace 兜底、有效 ID 冲突、跨网关观察和 ext_proc。前端当前只有类型检查与构建脚本，没有组件测试运行器。

### 已知证据与边界

用户提供的两条访问日志共享 `request_id`、原始 `start_time` 和下游连接，分别记录 sail / Qwen3.5 的 HTTP 400 与 ajiyongpeng / qwen3.6 的 HTTP 200；前一条详情为 `internal_redirect:ai_usage_via_upstream`。两个独立的 `ai_usage_record` 是业务用量日志，不是两次新增访问尝试。

该样例来自 Postman，probe ID 与 trace ID 均为 `-`，只能作为脱敏后的行为样例，不能直接导入为实时探测结果。验收样例需使用合成的有效 probe ID；不保留原问题、回答或用户身份。

当前官方 Envoy 源码中，router 成功执行内部重定向时调用 `recreateStream`，filter manager 将旧 stream 的详情设置为 `internal_redirect`；正常转发上游响应时 router 设置 `via_upstream`。`setResponseCodeDetails` 覆盖旧值，不自动拼接。扩展后缀的具体产生位置尚未核对用户实际构建源码，本设计只兼容已观测格式，不将其声称为所有 Envoy 版本的固定输出。

参考：[router 源码](https://github.com/envoyproxy/envoy/blob/main/source/common/router/router.cc)、[filter manager 源码](https://github.com/envoyproxy/envoy/blob/main/source/common/http/filter_manager.cc)、[响应详情常量](https://github.com/envoyproxy/envoy/blob/main/envoy/stream_info/stream_info.h)、[stream info 实现](https://github.com/envoyproxy/envoy/blob/main/source/common/stream_info/stream_info_impl.h)。源码参考用于解释判定依据，不要求修改网关 filter。

## 目标与非目标

**目标：**

- 新旧 Agent 与 Server 可渐进升级；既有访问记录仍然可用，新增语义由可选字段承载。
- 保持同一次主动请求和只读广播观察，增加有限的日志写入等待，不扩大流量副作用。
- 将“关联到本次探测”“具有 redirect 标记”“已关联后续记录”“全请求最终归属”作为不同结论，分别提供依据。
- 用用户提供的日志形态建立可复现的验收用例，不依赖生产日志或私有 filter 的实现。

**非目标：**

- 不识别未产生 redirect 标记的通用 retry、hedging、镜像请求或任意 Wasm fallback。
- 不引入 Span 父子关系，不恢复静态请求路由预测，也不把共享 ID 的跨网关记录直接画成确定分支图。
- 不解析独立 usage 日志、统计 token、修复字符编码或解释 `downstream_disconnected` 的自定义含义。
- 不自动修改 Higress access log 配置，不改变 Agent 不跟随客户端 HTTP 3xx 的约束。

## 设计决策

### 1. 请求归组与内部尝试关系分离

请求范围使用有效 probe ID 精确匹配；缺失值统一为不存在，其中包含空字符串、空白和 `-`。有效 probe ID 冲突继续禁止 trace 兜底，trace 的 `-` 也不参与匹配。沿用 `Correlation` 表达 `probe-id` 或 `trace-id`，不引入 `request_id` 关联。

Server 在既有 `ProbeSegment` 内按运行时组织尝试。运行时身份优先使用集群、Gateway 和 namespace/Pod/Container 来源；容器身份通过采集元数据保留。可用的原始开始时间、下游远端及本地连接身份用于区分同一运行时内再次进入或发生冲突的记录，不使用 Route、authority、path、Provider 或模型作为固定分组键，因为重定向可能改变它们。

建立相邻后续关系必须满足：同一运行时、请求身份无冲突、同一可排序日志源、前一条带 redirect 标记且后继唯一。缺少身份时可展示同组候选，但标为关系未确认；trace 兜底的分组明确保持该关联依据，不升级为 probe ID 精确证据。文件模式若不能确定文件对应的 Pod/Container，则记录 runtime 未知，仅保留来源内候选顺序。

**考虑的替代方案：** 将同 ID 的全部日志排成一条链实现最简单，但会把跨网关转发、不同 Pod 或普通重复记录当成 redirect，因此不采用。

### 2. 只识别有明确边界的响应详情标记

将识别逻辑放在日志观察层，接受恰好 `internal_redirect`，以及 `internal_redirect:` 后包含非空详情的字符串；保存原值和识别结果。仅对标记外侧做空白规范化，不使用任意子串匹配。

已观测重定向数量等于采集后去除重读的 redirect 记录数量，是证据数量，不等于网关内部发生过的所有重定向次数。没有标记时，即使 Provider 改变或存在 400 后接 200，也只显示关联记录。

**考虑的替代方案：** 仅匹配自定义完整值会遗漏标准 router；匹配任意包含 `internal_redirect` 的字符串会误识别失败详情，两者均不采用。

### 3. 保留来源顺序与稳定记录身份

为 `ObservedHop` 增加可选元数据：

| 字段 | 含义 |
| --- | --- |
| `id` | 本次探测内稳定记录 ID，供尝试关系与终止候选引用 |
| `runtimeSource` | 采集侧确定的运行时来源身份；无法确认时缺失 |
| `logSequence` | 同一来源的记录序号，只在来源内可比 |
| `requestStartTime` | 日志实际输出的原始开始时间，缺失时不生成业务值 |
| `internalRedirect` | 依据响应详情识别的 redirect 标记 |
| `aiRouting` | 固定 Provider 与模型摘要字段 |

保留 `ObservedAt` 供兼容与展示，但其缺失时间的回填不参与身份判断或因果排序。全局 `hops` 可继续作为旧消费者的记录列表；新增分组只引用记录 ID，使用同一来源的顺序，不依赖该列表顺序。

采集缓存以来源内记录 occurrence 为单位：文件读取优先使用文件代次与字节位置；Pod 日志重复快照使用原始单行内容指纹及同快照内 occurrence 编号关联重读。原始内容指纹只在当前采集缓存内部使用，不作为对外记录 ID；对外 ID 使用当前探测内不含业务内容的稳定编号。同一内容在同一快照出现两次时保留两个 occurrence。快照截断、文件轮转或无法定位重叠时保留已有证据并报告来源连续性未知，不虚构顺序或完整性。

**考虑的替代方案：** 时间加 Route 的去重键会删除共享时间的真实记录，甚至删除重复访问同一路由的尝试；只拼接每轮解析结果又会重复计数，不采用。

### 4. 使用有界采集与短暂稳定等待

`ExecuteProbe` 和 `ObserveProbe` 复用同一采集循环与累计缓存，仍受现有读取字节上限、命令超时和上下文取消约束。

初始默认策略：源请求结束后的日志窗口上限 2 秒，远端只读窗口上限 4 秒，读取间隔 200 毫秒，稳定等待 500 毫秒。窗口包含每次读取时间，读取请求受剩余时间的子上下文约束；剩余命令时间不足时缩短窗口，不能突破命令 deadline。这些默认值是实现参数，不作为固定规格契约，也不新增用户配置项。

- 没有关联记录时继续等待至窗口结束。
- 已有无冲突的终止候选时，至少经过一次额外完整读取及稳定等待后才允许结束；新 occurrence 到达会重置稳定等待。
- 仍有缺少后继的 redirect 记录、身份冲突或来源顺序未知时，持续到窗口上限，返回已读记录和缺口。
- 每轮读取合并到缓存，不覆盖已有集合；失败或取消仍返回已有记录。
- 对窗口结束的最后一次完整读取处理全部记录后再生成结果。时间上限只是采集结束，不证明未输出日志不存在。

新增可选 `ProbeExecution.collection`，包含状态 `settled`、`window-ended`、`cancelled`、`read-error` 或 `unknown`，以及截止时间和原因；远端结果复制到对应 `ProbeSegment.collection`。`settled` 只表示采集窗口内观察到稳定终止候选，不宣称整个内部过程已穷尽。

**考虑的替代方案：** 第一条匹配即结束可能只有中间记录；无限等待没有可证明的结束信号；每次都等待最大窗口又增加普通探测延迟，因此采用稳定等待加上限。

### 5. Server 统一产生观察结果，兼容旧字段

Server 在收齐各 Agent 结果后调用独立的观察聚合模块，根据每段记录和采集元数据生成以下可选字段。Agent 负责来源事实，Server 负责业务关系，前端负责文字与布局，不在三处分别推断执行链。

| 位置 | 新增字段 | 含义 |
| --- | --- | --- |
| `ProbeSegment` | `attemptGroups` | 运行时分组、记录 ID、有依据的尝试编号与后续关系 |
| 尝试分组 | `relationState` | `linked`、`unconfirmed`、`missing-next` 或 `ambiguous` |
| 尝试分组 | `orderBasis` | `source-sequence` 或 `unavailable` |
| 尝试分组 | `terminalCandidateIDs`、`gaps` | 非 redirect 终止候选及未确认原因 |
| `ProbeExecution` | `redirectSummary` | 已观测 redirect 数量、已关联关系数量、内部过程状态 |
| `ProbeExecution` | `finalResponseHopID` | 唯一可归属源网关响应记录，无法确认时缺失 |
| `ProbeExecution` | `finalUpstreamHopID` | 唯一可归属全请求最终上游的记录，无法确认时缺失 |

内部过程状态使用 `observed`、`partial`、`ambiguous`、`unknown`，不提供称为“所有尝试完整”的状态。`observed` 表示已关联所显示的尝试关系、采集正常收尾且没有已知缺口。过程缺口分别保留在分组和对应网关段，并汇总到请求级 `gaps`。

保留既有 `EvidenceComplete`，继续表达网关段的日志覆盖；新前端明确区分该字段与内部过程状态。旧 Agent 缺少元数据时可以识别已有详情里的 redirect 标记，但缺少可靠来源顺序、身份与采集状态时不构造确定关系，collection 默认为 `unknown`。新增 Server 结果使用可选字段，旧客户端仍能读取原有字段。

**考虑的替代方案：** 只在 Vue 内分组无法修复采集遗漏，也难以区分旧 Agent 与正常收尾；重新定义既有 `EvidenceComplete` 又改变旧消费者语义，因此不采用。

### 6. 保守确认局部终止记录与全请求最终上游

一个运行时分组中的非 redirect 日志只是终止候选。存在唯一终止候选、连续来源顺序、所有已观察 redirect 都有唯一后继、无身份或采集缺口时，可确认该分组的局部终止记录；多个候选或任何冲突保持未知。

只有源 Agent 请求正常完成、源网关存在唯一无歧义分组、具有 probe ID 精确关联及可靠身份、局部终止记录响应码与 Agent 实际响应码一致、采集正常收尾时，才设置 `finalResponseHopID`。仅响应码相同或只有一条日志不足以绕过其他条件。

当只有源网关这一已观测段、没有相关候选网关或其他过程缺口，并满足上述归属条件时，允许将同一记录作为 `finalUpstreamHopID`，表示该请求唯一观察到的最终上游选择。存在多个网关时，本变更不根据全局时间、配置唯一匹配或局部终止候选推断全请求末端，保留各段候选并显示全请求最终归属未确认。

`upstream_host` 缺失不会删除 cluster、Provider 和模型证据；前端明确表示地址未记录。即使存在地址，也只表达日志记录的上游选择，不能证明连接成功或后端处理成功。

**考虑的替代方案：** 全局最后记录、最大 duration、唯一 HTTP 200 或配置图末端均不能通用证明最终响应来源，不采用。

### 7. AI 摘要采用固定字段，不影响基础观察

在解析访问日志时尝试将字符串 `ai_log` 解码为对象，只提取 `provider`、`request_model`、`upstream_model`、`response_model` 的字符串值，映射到 `aiRouting`；字段长度按展示需要实施有界限制。不能解码时返回空摘要，不使整条日志解析失败。

新增摘要不返回问题、回答或任意业务字段，不使用 usage 的 outcome 或断连字段推断访问结果。既有 `aiLog` 接口字段暂时保留兼容，本变更不扩大其采集范围；新页面默认不展示整段原值，只展示固定摘要和现有基础访问证据。

**考虑的替代方案：** 前端解析原始 AI 日志会把业务字段解释分散到展示层；解析整个 usage 体系又扩大范围，因此不采用。

### 8. 前端沿用网关卡片，增加尝试层级

表单、会话草稿和入口选择保持现有行为。结果区域由以下层级组成：

| 层级 | 默认可见内容 | 交互与状态 |
| --- | --- | --- |
| 请求摘要 | 实际 HTTP 结果、Agent 总耗时、已观测网关数、记录数、redirect 数量 | 请求成功为绿色；redirect 提示为橙色；过程证据单独显示 |
| 网关段 | 集群、Gateway、Pod 分组、快照与网关覆盖证据 | redirect 或错误分组默认展开；候选网关保持既有缺口展示 |
| 尝试摘要 | Provider、上游模型、Route、响应码和记录角色 | 有序关联时编号；未知时使用“记录”及关系提示 |
| 后续连接 | “内部重定向，继续下一次尝试” | 仅关系已关联时连接；否则显示已发生 redirect 但后续关系未确认 |
| 尝试详情 | cluster、实际地址、日志 duration、上游服务耗时、响应详情、ext_proc、来源 | 每条记录独立展开，按钮可通过键盘操作 |
| 上游归属区 | 已归属记录或“最终上游归属未确认” | 缺少地址显示“实际地址未记录”；局部候选留在网关段内 |
| 证据缺口区 | 采集状态、后续缺失、排序或归属歧义 | 网关内就近提示，请求级去重汇总 |

样例的显示结果为：一个网关段，两条尝试记录；第一条 sail / Qwen3.5 为 HTTP 400 中间响应，随后显示内部重定向连接，第二条 ajiyongpeng / qwen3.6 为 HTTP 200 终止候选。在满足归属条件时第二条升级为最终响应。两个 upstream host 均缺失，因此不展示确定 Endpoint 或 Pod。

日志 duration 分别保留 440 ms、8736 ms，服务耗时分别保留 233 ms、8286 ms。顶部总耗时始终使用 Agent 实测值，不相加，也不将差值包装成未记录的单次耗时。

普通单记录紧凑显示；窄屏将字段纵向堆叠，长标识换行。使用文字、图标和颜色共同表达成功、错误、redirect 与未知，不仅依赖颜色。展示转换提取为小型纯函数模块，Vue 组件避免新增复杂推断。

**考虑的替代方案：** 每条日志新增网关卡片会虚增跳数；全局平铺尝试又会混淆所属网关；单纯折叠错误记录降低中间失败可见性，因此采用网关内嵌尝试卡片。

## 风险与取舍

- 日志写入可能迟于采集上限 → 保留已采集证据并报告窗口结束或后续缺失，不宣称内部过程完整。
- 普通探测会增加稳定等待，广播会增加只读日志调用 → 使用短稳定窗口和固定上限，保留字节限制与命令 deadline；不通过额外主动请求补证据。
- 文件来源不一定包含可确认的 Pod 身份，日志轮转也可能丢失重叠 → 降级为候选关系或顺序未知，不强行确认链。
- 非访问类记录可能恰有相同 probe ID → 日志观察层需区分明确的 `ai_usage_record`，不将其计为尝试；不扩大到 usage 关联。
- 同 ID 的镜像、重入或多个终止记录会产生歧义 → 保留记录，显示关系与最终归属未确认。
- 不同 Agent 版本造成部分元数据缺失 → 可选字段兼容、未知状态兜底；绝不退回“最后一条即最终上游”。
- 无 Span 关系会使多网关全请求最终上游更保守 → 仍展示每个网关的终止候选；拓扑映射继续注明配置来源，不恢复静态预测。
- 自定义后缀未来变化 → 仅支持有边界的 `internal_redirect:<非空详情>`，保存原值，针对其他格式显式增加适配而非宽泛字符串匹配。

## 迁移与验证计划

1. 先实现可选协议字段、解析与 Server 聚合，并让缺少字段的 Agent 结果显式降级未知；不修改生产网关配置。
2. 更新 Agent 有界采集，再更新前端尝试层级；现有 Server/Agent 命令和 API 路径不变。
3. 使用合成 probe ID、脱敏 Provider/模型日志验证标准 marker、扩展 marker、相同时间、多网关与延迟后续场景。
4. Go 使用现有测试体系覆盖解析、采集、聚合的行为；前端展示转换使用现有运行环境可执行的纯函数测试，不引入大型组件测试框架。类型检查、构建及桌面/窄屏页面验收覆盖展示。
5. 同步 `docs/10-observed-traffic-path.md` 与 `docs/05-frontend-design.md` 的 marker、关联条件、耗时与证据边界说明。
6. 回滚时可整体回滚本变更；字段均可选，不需数据迁移。独立回滚旧前端会恢复其原先误取最后记录的风险，因此优先将 Server、Agent 与新展示作为一个版本验收和发布。
