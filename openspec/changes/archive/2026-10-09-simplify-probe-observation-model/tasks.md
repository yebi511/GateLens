## 1. 领域协议与职责边界

- [x] 1.1 在 `internal/domain/types.go` 定义版本 2 的 `ProbeAgentResult`、`ProbeHTTPResult`、`ProbeIssue` 及必要原因码，精简 `ProbeExecution` / `ProbeSegment`，为 Hop 增加 ContextID、Segment 增加 Links / RelationState / LocalTerminalHopIDs 并重命名 SnapshotObservedAt；通过领域序列化测试验证版本、字段归属、空数组和已移除字段不再输出，保留工作区现有枚举修改。
- [x] 1.2 更新 `internal/source/source.go` 的局部执行接口与 `AgentCommandResult.Probe`，让 Agent 与聚合 API 使用不同结果类型；通过相关调用点编译及 Agent 命令测试验证真实请求结果包含 HTTP、只读观测结果不包含 HTTP，配置命令行为保持不变。

- [x] 1.3 按 design.md 第 7 节为探测流程及直接共享字段补齐具名枚举，复用已有采集/关系枚举；字段、内部规则结果、参数和比较统一使用对应类型/常量，固定 JSON 值保持可读字符串；通过编译、枚举往返/非法值测试及代码检查验证没有仅定义常量却保留裸 string 状态字段的遗漏，原始日志开放值不受限。

## 2. 局部执行与日志采集

- [x] 2.1 更新 `internal/kube/probe.go` 的 Execute / Observe 局部返回值，分离 HTTP 错误与非致命执行问题；通过 probe 测试验证请求失败仍保留日志、响应读取上限有执行问题、Observe 不发请求、客户端不跟随 HTTP 重定向。
- [x] 2.2 更新 `internal/kube/probe_logs.go` 的采集结果载体、缓存问题和状态结束路径，移除 Collection.Reasons 与说明文本关键词判断；通过日志采集测试验证延迟记录、重读、相同 occurrence、轮转、截断、取消和读错均保留证据，并验证更改问题说明不改变稳定性判断。

## 3. 后端关系分析与联邦聚合

- [x] 3.1 将完整尝试分组类型收回 `internal/observed`，保留现有分组键和有序性检查，投影 ContextID、确认 Links、段级 RelationState、LocalTerminalHopIDs 和上下文问题；通过关系测试验证同时间多次 redirect、不同 Pod、不同开始时间/下游地址、缺身份、缺后继、多终止候选，以及同段可靠/未知上下文共存，确认不跨上下文连接且重复分析保持引用稳定。
- [x] 3.2 整理解析与分析边界，让日志解析负责 InternalRedirect 和 AIRouting，分析不重复解析 AI 日志；通过合成日志测试验证标准/扩展 marker、相似字符串不误判、usage/access 混排、AI 摘要及 ext_proc 信息不丢失。
- [x] 3.3 更新 `internal/federation/store.go` 的源结果接收、其他网关观测和 Segment 组装，只在所属位置保存记录、Collection 与 Issues；通过联邦测试验证两个已观测网关和缺日志候选的计数输入正确，保留配置推断依据且没有顶层记录副本。
- [x] 3.4 在 Server 命令结果消费处验证 schemaVersion、探测/trace 标识、集群/网关归属及 HTTP 载荷形状；通过 Agent/联邦测试验证源协议错误导致探测失败，其他网关协议错误只形成该段明确问题，不覆盖源 HTTP 结果，不适配旧载荷。
- [x] 3.5 保留最终响应与最终上游的 Server 确认规则，并使用结构化问题判断缺口；通过归属测试验证单上下文稳定精确 probe 可归属，HTTP 冲突、trace 兜底、请求错误、多上下文、采集缺口和多网关保持保守归属，所有引用指向本次现有记录。

## 4. API 与 demo

- [x] 4.1 更新 `internal/api/handler.go` 相关协议测试与 `internal/demo/store.go` 的合成结果；通过 API/demo 测试验证创建与查询均返回版本 2 的同一精简结构，demo 可以正常展示日志和 ext_proc，现有 API 路径、输入校验与禁用探测响应保持现有行为。
- [x] 4.2 将旧协议正向兼容用例替换为明确拒绝用例，并另保留新协议缺身份/顺序的未知证据用例；通过 domain、Agent、API 与联邦相关测试确认协议不支持和证据不足是两种独立结果。

## 5. 前端消费与展示

- [x] 5.1 同步 `frontend/src/types.ts` 和 `frontend/src/api/client.ts`，校验探测响应版本与固定枚举取值并移除旧字段类型；为协议字段和派生状态/记录角色使用集中定义的具名联合类型及标签映射；通过类型检查及 API 消费测试验证缺失/未知版本、非法状态和非法问题码明确报错，创建与查询都不走旧结果回退，原始日志开放字段不误拒绝。
- [x] 5.2 简化 `frontend/src/utils/probePresentation.ts`，直接展开 Segment 记录、按 ContextID 隔离、按确认 Links 展示顺序，从唯一数据计算计数和证据摘要；通过 `npm run test:probe` 验证中间失败、局部终止/最终响应角色、可靠与未知上下文共存、问题去重和未知归属，不按时间或响应码补造关系。
- [x] 5.3 更新 `frontend/src/views/ProbeView.vue`、`frontend/tests/probeCases.ts` 和验收入口，移除完整 Group 消费及旧协议成功样例，保留新协议未知证据样例并补充协议错误和多上下文样例；通过真实组件合成验收验证请求结果、AI/ext_proc 详情、候选网关、缺实际地址、错误默认展开、375 px 窄屏与键盘详情操作。

## 6. 文档与整体回归

- [x] 6.1 更新 `docs/03-domain-model.md`、`docs/10-observed-traffic-path.md`、`docs/12-probe-redirect-acceptance.md` 的模型、示例与验收说明，注明三端同步升级和整体回滚；检查当前使用说明不再推荐旧兼容或旧字段，并明确局部终止、最终响应、最终上游三种归属区别。
- [x] 6.2 在仓库根执行 `go test ./...`，在 frontend 执行 `npm run test:probe`、`npm run test:topology`、`npm run typecheck`、`npm run build`，完成跨层回归；确认本次模型调整没有回退工作区已有拓扑关联与类型枚举行为，并记录验证结果。
- [x] 6.3 执行 `openspec validate simplify-probe-observation-model --strict --no-interactive`，检查实施结果与三份 delta specs 一致、任务记录和人工验收证据完整；仅在全部实现及验证完成后把任务标记完成，归档另按用户后续指令执行。

