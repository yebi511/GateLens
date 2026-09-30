# 联邦拓扑 Registry 展示规格

## Purpose

规定联邦拓扑如何展示 Higress McpBridge 中的 Registry、路由选择与配置归属，使用户能直接阅读上游关系，同时保留未确定选择和异常配置的诊断入口。

## Requirements

### Requirement: 将 McpBridge Registry 展示为后端抽象
联邦拓扑页面 MUST 在“后端抽象”列展示 McpBridge 下的 Registry，MUST 在每个 Registry 卡片上标明其所属 McpBridge 的命名空间和名称，且 MUST NOT 在“发现与调度”列重复展示该 Registry。页面 MUST NOT 将 McpBridge 绘制为普通拓扑节点卡片；EPP 与 EndpointPicker MUST 继续位于“发现与调度”列。

#### Scenario: 一个 McpBridge 含多个 Registry
- **WHEN** 当前集群的 McpBridge 配置了多个 Registry
- **THEN** 每个 Registry 在“后端抽象”列各有一张卡片，卡片均标明同一个 McpBridge 归属，页面不出现 McpBridge 普通节点卡片或“发现与调度”列的重复 Registry 卡片

### Requirement: 拓扑关系只表达已确定的 Registry 选择
联邦拓扑 MUST 在具有明确选择证据时展示 Ingress 到 Registry 的选择关系，并继续展示 Registry 到 Service 或外部目标的解析关系。Ingress 仅引用 McpBridge、但 Registry 选择未确定时，页面 MUST 标明所引用的 McpBridge 与“Registry 未确定”，MUST NOT 把该 Ingress 连接到全部候选 Registry，也 MUST NOT 将候选 Registry 表达为实际流量目标。

#### Scenario: 明确指定 Registry
- **WHEN** Ingress 的配置证据确定选择 McpBridge 中的一个 Registry
- **THEN** 拓扑显示该 Ingress 到被选 Registry 的关系，并可继续沿 Registry 查看解析目标

#### Scenario: McpBridge 只有一个 Registry
- **WHEN** Ingress 引用的 McpBridge 仅包含一个 Registry，且已有唯一选择证据
- **THEN** 拓扑显示该 Ingress 到唯一 Registry 的关系

#### Scenario: 多 Registry 但没有选择证据
- **WHEN** Ingress 引用包含多个 Registry 的 McpBridge，且没有可确认的 Registry 选择
- **THEN** 页面显示该引用及“Registry 未确定”，不绘制该 Ingress 到候选 Registry 的选择关系

### Requirement: 隐藏 McpBridge 卡片后仍可诊断和定位
联邦拓扑 MUST 保留 McpBridge 的配置归属、健康发现和定位能力。没有 Registry 的 McpBridge MUST 在拓扑中有可见、可定位的异常提示；从健康页或资源页定位 McpBridge 时，MUST 打开能识别该 McpBridge 的详情，并使其 Registry 或空配置状态可见。Registry 的详情 MUST 能查看所属 McpBridge；按 McpBridge 名称搜索时 MUST 能找到其 Registry 或空配置提示。

#### Scenario: McpBridge 没有 Registry
- **WHEN** McpBridge 未配置 Registry
- **THEN** 拓扑显示带有其名称及命名空间的异常提示，定位该 McpBridge 后能查看“无注册中心”的诊断信息

#### Scenario: 定位有 Registry 的 McpBridge
- **WHEN** 用户从资源页定位一个包含 Registry 的 McpBridge
- **THEN** 拓扑打开该 McpBridge 的详情，并显示与之关联的 Registry

#### Scenario: 搜索 McpBridge
- **WHEN** 用户在联邦拓扑搜索 McpBridge 名称
- **THEN** 搜索结果包含其 Registry；若该 McpBridge 没有 Registry，则包含其异常提示

### Requirement: 保留底层拓扑语义
此次展示调整 MUST NOT 改变采集和 API 提供的 McpBridge、Registry 节点及其关系，也 MUST NOT 改变健康发现、跨集群关联和实时探测对这些关系的解释。

#### Scenario: 其他页面消费拓扑数据
- **WHEN** 健康页或实时探测页读取同一拓扑快照
- **THEN** 它们仍可使用原有 McpBridge、Registry 节点和关系进行定位或解释
