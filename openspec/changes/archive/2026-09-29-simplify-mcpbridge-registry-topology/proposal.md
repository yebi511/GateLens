## Why

联邦拓扑当前把 McpBridge 和它的 Registry 分散在相邻两列，增加了一层视觉跳转，也让真正解析到服务目标的 Registry 离路由较远。将 Registry 作为后端抽象展示，可以更直接地表达配置上游与目标之间的关系。

## What Changes

- 联邦拓扑的“后端抽象”列展示 Registry，Registry 卡片标明所属 McpBridge 的命名空间和名称；“发现与调度”列不再展示 Registry。
- 联邦拓扑不再绘制 McpBridge 资源卡片，但保留其配置归属和诊断语义。对于没有 Registry 的 McpBridge，仍在拓扑中呈现可定位的告警入口。
- 仅在明确选择 Registry 或 McpBridge 仅有一个 Registry 时，绘制 Ingress 到 Registry 的选择关系。多 Registry 且未确定目标时，显示未确定状态，不制造指向所有 Registry 的流量关系。
- 保持采集、API 拓扑数据、健康发现与其他页面对 McpBridge/Registry 的既有使用方式。

## Capabilities

### New Capabilities

- `federated-topology-registry-presentation`: 规定联邦拓扑中 Registry 的分列、McpBridge 归属、路由关系和隐藏 Bridge 后的诊断定位行为。

### Modified Capabilities

无。

## Impact

- 主要影响 `frontend/src/views/FederatedTopologyView.vue` 的分列、可见节点、关系投影、筛选与详情。
- 可能影响 `frontend/src/App.vue` 的拓扑定位入口；需要为该页面的关系投影与诊断场景补充验证。
- 不变更 Kubernetes 采集、联邦关联规则、API 模型或 `ProbeView` 对 Registry 的解释。
