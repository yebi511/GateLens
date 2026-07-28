<p align="center">
  <img src="assets/gatelens-mark.svg" width="104" alt="GateLens logo">
</p>

<h1 align="center">GateLens</h1>

<p align="center"><strong>AI Gateway Explainability Platform</strong><br>让 AI 网关的配置、流量路径和故障原因变得可见。</p>

> GateLens 当前处于早期开发阶段。现有版本支持单 Kubernetes 集群、多命名空间的 Gateway API 拓扑与静态请求解释，尚不应作为生产变更或自动修复系统使用。

## 概述

AI 推理流量通常跨越 Gateway、HTTPRoute、ReferenceGrant、Service、EndpointSlice 和模型服务。单独阅读 YAML 很难判断请求会匹配哪个入口、跨命名空间后端是否被授权、以及服务是否存在可用 Endpoint。

GateLens 以只读方式监听 Kubernetes 资源，构建带来源证据的有效流量图。Go API 与 Vue Web 是两个独立进程：API 负责采集、规范化和路由解释；Web 只消费 JSON 契约并展示结果。

## 当前能力

- 监听单集群中的 Namespace、Service 与 EndpointSlice。
- 动态监听 Gateway API `Gateway`、`HTTPRoute` 与 `ReferenceGrant`。
- 监听 `IngressClass=higress` 及 Higress `McpBridge` 与 registry 配置。
- 展示 Gateway、Listener、HTTPRoute/Ingress、Service/McpBridge 与 Endpoint/Registry 的跨命名空间拓扑。
- 校验 ParentRef、BackendRef、ReferenceGrant 和 Ready Endpoint。
- 根据 Host、Path 与 Method 对请求进行静态解释。
- 提供资源搜索、配置健康清单和快照上下文。
- 自动定位网关的 Ready Pod，通过临时 port-forward 读取 Envoy `/config_dump`。
- 聚合 Wasm、`ext_proc` 和其他 Envoy Filter 的运行时挂载点，并关联 ECDS、Wasm 模块和上游 Cluster。
- 使用最小只读 RBAC，不修改集群资源，也不代理业务请求。

当前尚未实现 Higress/Istio 专有 CRD 的完整语义、多集群联邦拓扑、Trace/日志关联和实际流量确认。请求模拟属于**按快照推断**，不等同于真实请求已经经过该路径。

## 架构

```mermaid
flowchart LR
  K8S["Kubernetes API"] --> API["GateLens API · Go"]
  API --> SNAP["不可变拓扑快照"]
  SNAP --> GRAPH["有效流量图"]
  SNAP --> EXPLAIN["路由解释器"]
  GRAPH --> JSON["/api/v1 JSON"]
  EXPLAIN --> JSON
  BROWSER["浏览器"] --> WEB["GateLens Web · Vue"]
  WEB --> JSON
```

后端不再包含、构建或托管任何 HTML、JavaScript 和 CSS。开发环境由 Vite 将 `/api` 代理到 Go 服务；生产环境由独立的 Web 容器托管静态产物，并把 `/api` 转发到 API Service。

## 快速开始

需要 Go 1.24、Node.js `^20.19.0` 或 `>=22.12.0`，以及 npm 9 或更高版本。推荐使用仓库 `.nvmrc` 指定的 Node.js 24：

```bash
cd frontend
nvm install
nvm use
```

先启动演示 API：

```bash
make run-api-demo
```

在另一个终端启动前端：

```bash
make run-web
```

浏览器访问 <http://localhost:5173>。演示 API 位于 <http://localhost:8080>，Vite 开发服务器会自动代理请求。

不使用 Make 时：

```bash
GATELENS_MODE=demo GATELENS_ADDR=:8080 go run ./cmd/gatelens
cd frontend
npm ci
npm run dev
```

## 构建与测试

```bash
make fmt
make test
make build-all
```

API 二进制输出到 `bin/gatelens-api`，前端生产资源输出到 `frontend/dist/`。也可分别执行 `make test-api`、`make test-web`、`make build-api` 和 `make build-web`。

## 构建镜像

API 与 Web 使用独立镜像：

```bash
make image \
  API_IMAGE=registry.example.com/platform/gatelens-api:v0.1.0 \
  WEB_IMAGE=registry.example.com/platform/gatelens-web:v0.1.0
make image-push \
  API_IMAGE=registry.example.com/platform/gatelens-api:v0.1.0 \
  WEB_IMAGE=registry.example.com/platform/gatelens-web:v0.1.0
```

网络受限环境可通过 `GOPROXY` 覆盖 API 镜像的 Go 模块代理。

## 部署到 Kubernetes

前置条件：

- 集群已安装 Gateway API CRD。
- 当前版本读取 `gateway.networking.k8s.io/v1` 的 Gateway/HTTPRoute，以及 `v1beta1` 的 ReferenceGrant。
- 若要采集 Higress 配置，集群还需安装 `networking.higress.io/v1` McpBridge CRD。
- API 与 Web 镜像已推送到集群可访问的仓库。

```bash
make deploy \
  API_IMAGE=registry.example.com/platform/gatelens-api:v0.1.0 \
  WEB_IMAGE=registry.example.com/platform/gatelens-web:v0.1.0
kubectl -n gatelens-system port-forward svc/gatelens 8080:80
```

访问 <http://localhost:8080>。`gatelens` Service 指向 Web，Web 在集群内通过 `gatelens-api:8080` 访问 API。只有 `gatelens-api` ServiceAccount 拥有 Kubernetes 只读权限，Web Pod 没有集群读取权限。

## 配置

### API

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `GATELENS_ADDR` | `:8080` | API HTTP 监听地址 |
| `GATELENS_MODE` | `kubernetes` | `kubernetes` 使用 in-cluster 配置；`demo` 使用内置数据 |
| `GATELENS_CLUSTER_ID` | `in-cluster` | 拓扑和快照中的集群标识 |
| `GATELENS_ALLOWED_ORIGINS` | 空 | 允许直接跨域调用 API 的 Origin，多个值用逗号分隔 |

### Web

| 环境变量 | 使用位置 | 说明 |
| --- | --- | --- |
| `GATELENS_API_PROXY` | Vite 开发时 | `/api` 开发代理目标，默认 `http://127.0.0.1:8080` |
| `VITE_API_BASE_URL` | 前端构建时 | API 绝对地址；同源部署保持为空 |
| `GATELENS_API_UPSTREAM` | Web 容器运行时 | Nginx API 上游，默认 `http://gatelens-api:8080` |

## HTTP API

| Endpoint | 说明 |
| --- | --- |
| `GET /healthz` | API 进程健康检查 |
| `GET /api/v1/context` | 集群、命名空间、快照与能力 |
| `GET /api/v1/topology` | 规范化拓扑节点和边 |
| `GET /api/v1/envoy/config?gatewayID=` | 指定 Gateway 的 Envoy 配置 |
| `GET /api/v1/resources?q=` | 搜索当前快照资源 |
| `GET /api/v1/health/findings` | 配置健康问题 |
| `POST /api/v1/route-explanations` | 按快照解释请求匹配和后端状态 |

## 项目结构

```text
cmd/gatelens/        API 进程入口
internal/api/        纯 JSON HTTP API
internal/kube/       Kubernetes informer、快照和解析
internal/domain/     通用领域模型
internal/demo/       本地演示数据源
frontend/            Vue 3 + TypeScript 独立前端
assets/              品牌资源，由前端构建使用
deploy/              API/Web Kubernetes 部署清单
docs/                产品、架构和 ADR 文档
```

详细说明见[代码目录结构](docs/08-code-structure.md)。

## 开发与贡献

提交变更前请保证：

- `go test ./...` 通过。
- `cd frontend && npm run typecheck && npm run build` 通过。
- 新增网关语义具有匿名化测试样本。
- 推断结果保留来源和快照信息。
- 不采集 Authorization、Cookie、API Key、提示词或响应正文。

架构调整请新增 ADR，不要改写已经接受的历史决策。参见 [`docs/adr`](docs/adr/README.md)。

## 文档

- [产品范围与用户旅程](docs/01-product-scope.md)
- [系统架构与数据流](docs/02-architecture.md)
- [统一领域模型与可解释路由](docs/03-domain-model.md)
- [前端展示设计](docs/05-frontend-design.md)
- [跨命名空间与跨集群设计](docs/06-federated-routing.md)
- [Higress Ingress 与 McpBridge 采集设计](docs/09-higress-ingress-mcpbridge.md)
- [架构决策记录](docs/adr/README.md)

## 安全

GateLens 当前仅提供只读能力。请不要在公开 Issue 中提交集群凭据、访问日志或业务请求内容。

## 许可证

项目尚未选择开源许可证。首次公开发布前必须添加明确的 `LICENSE` 文件；在此之前，请不要假定代码已获得任何开源许可。
