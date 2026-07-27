# ADR 0006：Web 与 API 独立构建和部署

- 状态：Accepted
- 日期：2026-07-26

## 背景

早期版本使用 Go `embed` 将原生 HTML、JavaScript 和 CSS 放入 API 二进制，同时在 `web/static` 保留另一份相同副本。这个方式适合快速演示，但形成双份源码，并使前端变更、依赖管理、发布节奏和扩缩容都与 Kubernetes 采集进程耦合。

现有前端已经完全通过 `/api/v1` 获取上下文、拓扑、Envoy 配置、健康问题、资源和请求解释，后端没有必要参与页面渲染。

## 决策

- Go 服务改为纯 JSON API，不嵌入或托管前端产物。
- 前端采用 Vue 3、TypeScript、组合式 API 与 Vite，源代码放在 `frontend/`。
- 本地开发使用 Vite `/api` 代理；需要直接跨 Origin 调用时，API 使用显式 Origin 白名单。
- 生产环境使用独立 Web 镜像托管静态产物，并反向代理 `/api` 到 API Service。
- Kubernetes 中 Web 与 API 使用独立 Deployment 和 Service；只有 API ServiceAccount 拥有集群读取权限。
- `assets/gatelens-mark.svg` 继续作为品牌源文件，由前端构建直接使用。

## 后果

前端和后端可以独立开发、测试、构建、发布和扩缩容。API 二进制更小且职责单一，Web Pod 不持有 Kubernetes 权限。代价是本地开发需要两个进程，发布需要两个镜像，并需维护反向代理或跨域配置。
