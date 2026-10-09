# 实时探测模型简化验收记录

验收日期：2026-10-09。变更：`simplify-probe-observation-model`。在原内部重定向样例基础上验证版本 2 协议。所有访问记录为合成数据，无真实请求正文或回答。

## 可重复入口

后端样例为 `internal/observed/testdata/probe_redirect.jsonl`。`TestSyntheticAccessLogsThroughAggregation` 替换标准、AI 扩展和其他扩展 marker，贯穿解析与聚合，验证两条访问记录、一条连接、HTTP 结果及唯一源响应引用。

前端数据为 `frontend/tests/probeCases.ts`；纯函数及协议测试为 `probePresentation.test.ts`。实际 `ProbeView.vue` 与生产样式的验收入口：

```powershell
cd frontend
npm run dev -- --host 127.0.0.1 --port 5173
```

打开 `http://127.0.0.1:5173/tests/probe-acceptance.html`，选择场景后点击“发送探测”。该入口替换 API 为合成数据，不访问网关，不出现在默认生产入口。版本错误样例通过实际协议校验器拒绝，并显示错误信息。

## 场景结果

| 场景 | 访问记录 / 确认连接 | 结果 |
| --- | --- | --- |
| 普通单记录 | 1 / 0 | 一个已观测网关，没有 redirect 提示，详情可展开 |
| 标准 internal_redirect | 2 / 1 | HTTP 200 实测摘要保留，HTTP 400 为中间响应 |
| 自定义 filter 扩展 | 2 / 1 | 原始 marker 保留，Provider/模型从固定 AI 摘要显示 |
| 连续两次 redirect | 3 / 2 | 一个网关段中三次尝试，两条确认后继连接 |
| 只有中间记录 | 1 / 0 | 窗口结束与后继缺失分别显示，最终上游未知 |
| 多终止候选 | 3 / 0 | 全部使用访问记录名称，不补造确定顺序 |
| 新协议身份缺失 | 2 / 0 | 版本合法但身份不足，保留日志和 ext_proc，过程未知 |
| 可靠与未知上下文 | 3 / 1 | 分成两个 ContextID，可靠连接与局部终止角色保留，未知记录无编号、无连接 |
| 协议版本错误 | 拒绝进入普通结果页 | 明确提示需要版本 2 及三端同步升级 |
| 多个网关 | 3 / 1 | 两个已观测网关、一个无日志候选，最终上游未知 |
| 请求读取错误 | 1 / 0 | 请求失败独立显示，成功日志不覆盖错误 |

后端回归还覆盖：延迟 250 ms 写入终止日志但只发一次 HTTP；只读命令无 HTTP 载荷；客户端不跟随 3xx；响应读取失败仍保留日志；响应读取上限单独产生 execution issue；usage/access 混排；占位关联值；真实重复 occurrence；轮转、窗口截断、取消与读取失败；不同 Pod、原始开始时间或下游地址不跨上下文连接；重复分析 ID/引用稳定；说明文本不影响问题判断。

局部终止、最终响应和最终上游引用分别验证。HTTP 冲突、trace 兜底、请求错误、多上下文、采集问题及多网关不会被“全局最后一条记录”替代。源协议或身份错误使执行失败；远端协议错误保留为对应段采集问题，源 HTTP 200 不受影响。领域测试验证有限枚举往返及非法值，开放日志 Method / Protocol / ResponseFlags / 详情仍可接收。

## 实际组件检查

本次使用本地无头 Edge 加载实际 Vue 页面，遍历上述 11 个场景，检查可见记录、连接、候选、未知归属与协议错误。浏览器控制服务初始化失败后改用本地组件回归，没有发送真实网关请求。

桌面 1280 px，窄屏 375 px；截图已目视检查，375 px 下 body 与 document 宽度均为 375 px，无横向溢出，长 Route / Cluster / 模型纵向换行。通过 Tab 定位访问证据 summary，Enter 展开，焦点 outline 为 solid。AI 问题/回答 sentinel 未出现在可见文字；ext_proc “失败放行”、调用次数与耗时保留；地址 `-` 显示“实际地址未记录”。日志 duration 为 440/8736 ms，上游服务耗时为 233/8286 ms，顶部仍为实测 8750 ms，不相加或求差。

使用真实 API client 并拦截为合成响应，分别验证创建与查询的版本 2 成功结果、旧版本拒绝和非法 issue.code 拒绝，共 6 个检查。浏览器没有页面错误或控制台错误。本次本地报告和桌面/窄屏截图在 `.tmp/probe-acceptance/`（忽略目录）中。

## 自动验证

仓库根执行 `go test ./...`；frontend 执行 `npm run test:probe`（7 组）、`npm run test:topology`（4 组）、`npm run typecheck`、`npm run build`。OpenSpec 校验命令为：

```powershell
openspec validate simplify-probe-observation-model --strict --no-interactive
```

版本 2 不兼容旧 Agent 或旧结果。部署需先停止新探测，等待在途请求完成，三端同步升级；回滚时前端、Server、Agent 整体回滚并重启。Server 内存结果不做跨版本转换。本次仅覆盖采集窗口内可见证据，settled 不宣称所有内部尝试都已输出日志；共享文件和多网关缺少因果依据时仍保持未知。
