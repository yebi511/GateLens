# 实时探测内部重定向验收记录

验收日期：2026-09-17。变更：`observe-gateway-internal-redirects`。以下数据均为合成样例，域名为示例域名，地址使用文档保留地址，没有复制真实请求正文或回答。

## 可重复样例

后端样例位于 `internal/observed/testdata/probe_redirect.jsonl`：两组 usage/access 日志共享 probe ID、原始开始时间及下游连接，Provider/模型/Route 切换，中间 HTTP 400 带扩展 marker，后续 HTTP 200。`TestSyntheticAccessLogsThroughAggregation` 将同一例替换为标准标记与另一个扩展后缀，贯穿日志解析和 Server 关系聚合，验证只有两条访问记录、一条 redirect 关系及唯一源响应归属。

前端可重复数据位于 `frontend/tests/probeCases.ts`；`probePresentation.test.ts` 验证纯函数转换。页面样例使用实际 `ProbeView.vue` 和生产样式，开发环境打开：

```powershell
cd frontend
npm run dev -- --host 127.0.0.1 --port 5173
```

访问 `http://127.0.0.1:5173/tests/probe-acceptance.html`，选择场景后点击“发送探测”。该专用样例入口替换 API 为合成结果，每次点击只产生一次合成调用，不访问网关或后端，也不进入默认生产页面入口。

## 场景与结果

| 场景 | 验收结果 |
| --- | --- |
| 普通单记录 | 一个已观测网关、一条紧凑记录、没有 redirect 提示；证据详情可展开 |
| 标准 `internal_redirect` | HTTP 200 摘要保留；同一网关内显示两次尝试、400 中间响应、一条 redirect 连接 |
| 自定义 filter 扩展 | `ai_usage_via_upstream` 和另一个后缀均识别，原始详情保留，普通 AI filter 详情不误判 |
| 连续两次 redirect | 一个网关、三条记录、两次已观测 redirect、两条组内连接 |
| 后续延迟写入 | Go 集成服务先输出 marker，再延迟 250 ms 输出终止日志；Execute 与 Observe 都保留两条记录，源 HTTP 请求计数始终为一次 |
| 只有中间记录 | 顶部保留 Agent HTTP 200，显示后续记录缺失和窗口结束，不补造终止尝试，最终上游未知 |
| 多终止候选/重入 | 全部记录保留，使用记录名称与关系未确认说明，不输出确定连接或全局最后记录归属 |
| usage/access 混排 | 两组日志只计两条访问记录，不依据 usage outcome 判断 HTTP 成败 |
| 旧 Agent | 原 hops、segments 与 ext_proc 证据保留，过程状态未知，没有确定的尝试连接或最终上游兜底 |
| 多网关 | 两个已观测网关分别组织记录；候选网关另计；不跨网关连接内部尝试，全请求最终上游未知 |
| 请求读取错误 | 顶部显示探测请求失败，下方普通 200 日志不能覆盖错误 |
| 占位关联值 | probe/trace 均为 `-` 不匹配；有效 trace 可兜底；有效冲突 probe 拒绝；request ID 不兜底 |
| 重读/重复 occurrence | 相同快照重读不增数；来源内相同内容的两个 occurrence 保留两个稳定 ID |
| 轮转/截断/取消/读错 | 保留已有记录，标注 collection 和 gap；来源顺序不足时不虚构确定关系 |

## 页面检查

使用本机浏览器检查了 9 个页面场景及实际 Vue 组件。桌面宽度 1280 px；窄屏宽度 375 px，尝试摘要与详情纵向排列，页面宽度 360 px（扣除滚动条），没有横向溢出。Route、模型、cluster 与响应详情均可换行。

使用 Enter 展开访问证据详情，焦点轮廓可见；ext_proc “失败放行”与调用/耗时详情仍保留。合成 `aiLog` 中的问题/回答 sentinel 未出现在页面可见文字中，Provider 与模型只通过固定摘要显示。浏览器未记录错误或警告。

日志 duration 分别显示 440/8736 ms，上游服务耗时分别为 233/8286 ms；顶部仅显示 Agent 合成实测总耗时 8750 ms，没有相加或差值推算。地址 `-` 显示“实际地址未记录”。多网关、缺口、歧义和旧结果均显示“最终上游归属未确认”。

## 自动验证

在仓库根目录：

```powershell
go test ./...
```

在 `frontend` 目录：

```powershell
npm run test:probe
npm run typecheck
npm run build
```

完整 Go 回归（含 probe、federation、API、Agent、ext_proc）、6 组前端转换测试、Vue/TypeScript 类型检查与 Vite 生产构建通过。后端合成日志贯穿测试在追加后再次单独验证；窄屏样式修正后再次执行前端验证。OpenSpec 使用 `openspec validate observe-gateway-internal-redirects --strict --no-interactive` 校验。

此验收覆盖采集窗口内可见证据，不宣称所有内部尝试都已输出日志；共享文件无法证明运行时来源，多网关缺少 Span 因果关系时，最终归属继续保持未知。
