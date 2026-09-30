## Context

见 `proposal.md`。当前 `FederatedTopologyView.vue` 用 `nodeCategories` 将 McpBridge 放在“后端抽象”、Registry 放在“发现与调度”；SVG 仅对两端都有可见卡片的边绘线。采集层同时提供 `Ingress -> McpBridge` 的 `routes`、`McpBridge -> Registry` 的 `discovers`，以及有明确目标时 `Ingress -> Registry` 的 `selects`。健康页和资源页可按原始 McpBridge ID 跳转拓扑；当前详情选择必须落在可见节点中，隐藏普通卡片后需要单独处理该入口。

## Goals / Non-Goals

**Goals:**
- 让联邦拓扑沿“路由与策略 -> 后端抽象 -> 服务目标”的列顺序直接阅读已确认的 Registry 选择及解析关系。
- 不丢失 McpBridge 归属、空配置告警、资源定位和选择未确定的语义。

**Non-Goals:**
- 不改变单集群 `TopologyView.vue`、采集节点和边、API 协议或实时探测的推断规则。
- 不从 `routes` 加 `discovers` 推断多个 Registry 均被选中，也不把配置关系当作实际请求路径。

## Decisions

### 1. 在联邦拓扑建立仅供展示的投影

从 `nodeCategories` 移除 McpBridge，并把 Registry 加入“后端抽象”；“发现与调度”保留 EPP/EndpointPicker。原始 `props.topology` 保持完整。以 `discovers` 边构建 Registry 到所属 McpBridge 的查找表，卡片副标题显示 `McpBridge <namespace>/<name>`，详情显示完整归属和原始配置关系。不要从 Registry ID 字符串反推所属 Bridge，因为 ID 格式不是展示契约。

画线使用展示边集合：保留两端可见的原始边，排除端点为隐藏 McpBridge 的边；保留原始 `selects` 边作为 Ingress 到 Registry 的直接关系，不从 `routes` 与 `discovers` 的传递闭包合成 `selects`。这样同列与跨列路径都只反映已有证据。`McpBridge -> Registry` 的包含关系转到卡片文字和详情，不作为 SVG 线。

替代方案是修改后端输出为 `Ingress -> Registry`。这会改变联邦关联、健康页和 Probe 页消费的拓扑语义，因此不采用。

### 2. 未确定选择用路由说明承载

对于指向 McpBridge 的 Ingress `routes` 边，如果同一 Ingress 没有指向该 Bridge 下 Registry 的 `selects` 边，在 Ingress 详情中列出 McpBridge 引用并显示“Registry 未确定”。可在 Ingress 卡片给出简短提示，使未连线不被误读为没有后端。即使 Bridge 含多个 Registry，页面也不批量绘制候选连线。单 Registry 的选择仍以采集层已给出的 `selects` 边为准；若采集层未给出该边，也显示未确定，避免前端自行补推断。

替代方案是把未确定引用画成从 Ingress 指向所有 Registry 的虚线；这仍会让候选看起来像实际选择，因此不用。

### 3. 隐藏普通卡片但允许 McpBridge 详情与空配置提示

详情选择从“必须在可见卡片中”调整为“当前集群中可定位的对象”：正常点击仍由可见卡片选择；从健康页或资源页传入 McpBridge ID 时可打开 Bridge 详情，并列出它的 Registry。若 Bridge 无 Registry，“后端抽象”列显示独立的异常提示行，带 Bridge 名称、命名空间和“无注册中心”；点击提示也打开同一详情。此提示不是普通 McpBridge 节点卡片，也不参与拓扑连线。

搜索 McpBridge 名称时，展示其所有 Registry；无 Registry 时展示异常提示。问题筛选下，空配置提示仍可见。对象计数只统计可见资源卡片，提示行另行表达数量，避免把隐藏 Bridge 当作普通卡片计数。定位动作应清除会遮住目标的搜索或问题筛选，并在切换集群后保持目标 ID 到详情完成打开。

替代方案是把健康页目标 ID 改写成首个 Registry ID。无 Registry 时没有可定位对象，多个 Registry 时选择任一个也会误导，因此不用。

### 4. 保持跨集群边界提示挂在可见对象上

Registry 既有跨集群边与其目标提示继续附着于 Registry 卡片。隐藏 Bridge 不应使路由与 Registry 已有的边界提示、选中高亮或详情关系丢失。筛选、命名空间过滤和连线布局都应基于同一可见展示集合；不为因筛选隐藏的端点绘制悬空连线。

## Risks / Trade-offs

- [空 Bridge 失去常规卡片] -> 提供独立异常提示与可直接打开的 Bridge 详情，并验证健康页定位。
- [多 Registry 被误认为全量转发] -> 只画已有 `selects` 边；未确定引用使用显式文案。
- [过滤后详情或连线失效] -> 区分可见卡片集合与可定位详情集合，切换筛选时清理或保留可解释的选择状态，并检查 SVG 仅连接可见端点。
- [Registry 同名导致归属混淆] -> 卡片及详情使用 `namespace/name` 标识 Bridge，而非只展示 Registry 名称。

## Migration Plan

纯前端展示调整，无数据迁移。部署时可沿用现有拓扑响应；回滚页面变更即可恢复原列布局。
