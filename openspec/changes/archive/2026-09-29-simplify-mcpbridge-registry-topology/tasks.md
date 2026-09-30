## 1. Registry 展示投影

- [x] 1.1 调整 `FederatedTopologyView.vue` 的分列和可见节点，使 Registry 只出现在“后端抽象”、McpBridge 不再作为普通卡片、“发现与调度”保留 EPP/EndpointPicker；用包含关系在 Registry 卡片及详情显示所属 Bridge 的 `namespace/name`。通过单 Bridge 多 Registry 的页面场景核对卡片位置与归属。
- [x] 1.2 将 SVG 连线限定为两端可见的展示边，保留已有 `selects` 与 `resolves`，不把 `routes + discovers` 合成选择边；用明确选择、唯一选择和多 Registry 未选择的拓扑数据核对连线与无悬空边。

## 2. 未确定状态与 McpBridge 定位

- [x] 2.1 为只有 `Ingress -> McpBridge` 引用、没有对应 `selects` 的 Ingress 显示 Bridge 名称及“Registry 未确定”，在卡片和详情中可见；用多 Registry 未选择场景核对页面不出现指向候选 Registry 的关系。
- [x] 2.2 为无 Registry 的 McpBridge 增加“后端抽象”列异常提示，支持点击查看其“无注册中心”详情；用空 Bridge 健康发现核对提示、状态和详情可达。
- [x] 2.3 保持健康页与资源页按 McpBridge ID 定位：即使普通卡片隐藏，也能打开 Bridge 详情并列出其 Registry 或空配置状态；核对跨集群切换及筛选状态下的定位。
- [x] 2.4 使搜索 McpBridge 名称能显示关联 Registry 或空配置提示，问题筛选保留空 Bridge 提示，并核对可见对象计数、Registry 跨集群提示和选中高亮。

## 3. 验证

- [x] 3.1 为展示投影的多 Registry/单 Registry/空 Bridge 场景补充针对性验证，运行前端类型检查与构建，并人工检查健康页及资源页定位、筛选和详情行为；确认采集、API 和 Probe 页代码未因本次调整改变。

