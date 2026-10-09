export type TopologyNodeKind =
  | 'Gateway'
  | 'Listener'
  | 'HTTPRoute'
  | 'Ingress'
  | 'Service'
  | 'Endpoint'
  | 'Pod'
  | 'InferencePool'
  | 'EndpointPicker'
  | 'McpBridge'
  | 'Registry'
  | 'ExternalTarget'
  | 'TransitHop'

export type Status = 'healthy' | 'warning' | 'error'
export type ViewID = 'topology' | 'envoy' | 'probe' | 'health' | 'resources'

export interface Cluster { id: string; name: string; version: string }
export interface Snapshot { id: string; observedAt: string; state: string }
export interface GateLensContext {
  cluster: Cluster
  namespaces: string[]
  snapshot: Snapshot
  adapterCapabilities: string[]
}
export interface TopologyNode {
  id: string
  name: string
  kind: TopologyNodeKind
  namespace: string
  clusterID: string
  status: Status
  statusText: string
  summary: string
  conditions: string[]
  source: string
  workloadScope?: string
}
export interface TopologyCluster {
  id: string
  name: string
  environment?: string
  version: string
  connectionState: string
  namespaces: string[]
  snapshot: Snapshot
}
export interface TopologyEdge {
  from: string
  to: string
  relation: string
  transport?: string
  destination?: string
  state?: string
  evidence?: string
}
export interface Topology {
  federatedSnapshotID?: string
  snapshotID: string
  consistency?: SnapshotConsistency
  observedAt: string
  clusters?: TopologyCluster[]
  nodes: TopologyNode[]
  edges: TopologyEdge[]
  probeEntries?: ProbeEntry[]
  truncated: boolean
}
export interface Finding {
  id: string
  severity: Status
  title: string
  resource: string
  basis: string
  targetID: string
}
export interface Resource {
  id: string
  kind: string
  name: string
  namespace: string
  status: Status
  statusText: string
  updatedAt: string
  findings: number
}
export interface EnvoyEndpoint { address: string; port: number; status: Status; health: string; weight: number }
export interface EnvoyCluster {
  name: string
  type: string
  discovery: string
  connectTimeout: string
  endpoints: EnvoyEndpoint[]
}
export interface EnvoyWeightedCluster { name: string; weight: number }
export interface EnvoyRoute {
  name: string
  match: string
  cluster: string
  weightedClusters?: EnvoyWeightedCluster[]
  extProcs?: EnvoyRouteExtProcTarget[]
}
export interface EnvoyRouteExtProcTarget {
  filterName: string
  typeURL?: string
  grpcClusters?: string[]
}
export interface EnvoyHTTPFilter {
  name: string
  type: string
  stage: string
  configSummary: string
  terminal: boolean
}
export interface EnvoyExtensionAttachment {
  listenerID: string
  listenerName: string
  filterChain: string
  filterName: string
  filterType: string
  position: number
}
export interface EnvoyExtensionDependency {
  kind: string
  name: string
  relation: string
  evidence: string
  resolved: boolean
}
export interface EnvoyExtension {
  id: string
  name: string
  kind: 'Wasm' | 'ext_proc' | 'Envoy Filter'
  typeURL?: string
  status: Status
  configSource: string
  configSummary: string
  attachments: EnvoyExtensionAttachment[]
  dependencies: EnvoyExtensionDependency[]
}
export interface EnvoyFilterChain {
  name: string
  match: string
  transport: string
  httpFilters: EnvoyHTTPFilter[]
  routes: EnvoyRoute[]
}
export interface EnvoyListener {
  id: string
  name: string
  address: string
  port: number
  protocol: string
  status: Status
  filterChains: EnvoyFilterChain[]
}
export interface EnvoyConfig {
  snapshotID: string
  observedAt: string
  state: string
  source: string
  proxy: string
  gatewayID?: string
  controller?: string
  workload?: string
  sampledPod?: string
  readyReplicas?: number
  listeners: EnvoyListener[]
  clusters: EnvoyCluster[]
  extensions: EnvoyExtension[]
  rawConfig?: unknown
}
// ProbeExecutionState 的固定协议值；原始日志字段保持开放。
export const ProbeExecutionStateValues = [
  'running', // 请求执行中
  'completed', // 请求已执行完成，HTTP 状态另行判断
  'failed', // 请求或调度失败
] as const
export type ProbeExecutionState = typeof ProbeExecutionStateValues[number]

// ProbeIssueScope 的固定协议值；原始日志字段保持开放。
export const ProbeIssueScopeValues = [
  'execution', // 执行层问题
  'collection', // 日志采集问题
  'relation', // 记录关系问题
  'inference', // 候选推断问题
] as const
export type ProbeIssueScope = typeof ProbeIssueScopeValues[number]

// ProbeIssueCode 的固定协议值；原始日志字段保持开放。
export const ProbeIssueCodeValues = [
  'response-limit-exceeded', // 响应读取超过上限
  'log-source-rotated', // 日志源轮转或截断
  'log-window-truncated', // 读取窗口截断
  'log-order-unconfirmed', // 日志源顺序未确认
  'collection-window-ended', // 采集窗口结束
  'collection-cancelled', // 采集取消
  'log-read-error', // 读取日志失败
  'no-matching-log', // 没有匹配日志
  'agent-unavailable', // Agent 命令不可用
  'probe-protocol-unsupported', // 探测协议不支持或结果无效
  'request-identity-incomplete', // 请求身份不完整
  'attempt-order-unconfirmed', // 尝试顺序未确认
  'redirect-next-missing', // 重定向后继缺失
  'terminal-ambiguous', // 终止候选或重入有歧义
  'gateway-evidence-missing', // 候选网关缺少运行时证据
] as const
export type ProbeIssueCode = typeof ProbeIssueCodeValues[number]

// ProbeCorrelation 的固定协议值；原始日志字段保持开放。
export const ProbeCorrelationValues = [
  'probe-id', // Probe ID 精确匹配
  'trace-id', // Trace ID 兜底匹配
] as const
export type ProbeCorrelation = typeof ProbeCorrelationValues[number]

// ProbeInferenceConfidence 的固定协议值；原始日志字段保持开放。
export const ProbeInferenceConfidenceValues = [
  'high', // 地址精确匹配
  'medium', // 配置服务匹配
  'low', // 声明关系推断
  'ambiguous', // 存在多个匹配
] as const
export type ProbeInferenceConfidence = typeof ProbeInferenceConfidenceValues[number]

// ProbeOrderBasis 的固定协议值；原始日志字段保持开放。
export const ProbeOrderBasisValues = [
  'source-sequence', // 同一来源的记录顺序
  'unavailable', // 无可比顺序
] as const
export type ProbeOrderBasis = typeof ProbeOrderBasisValues[number]

// AgentCommandKind 的固定协议值；原始日志字段保持开放。
export const AgentCommandKindValues = [
  'envoy-config', // 读取 Envoy 配置
  'probe-http', // 发出真实探测请求
  'probe-observe', // 只读关联日志
] as const
export type AgentCommandKind = typeof AgentCommandKindValues[number]

// ExtProcOutcome 的固定协议值；原始日志字段保持开放。
export const ExtProcOutcomeValues = [
  'success', // 处理成功
  'timeout', // 处理超时
  'error', // 处理错误
  'fail-open', // 失败后放行
  'immediate-response', // 处理器直接返回响应
  'unknown', // 处理结果未知
] as const
export type ExtProcOutcome = typeof ExtProcOutcomeValues[number]

// SnapshotConsistency 的固定协议值；原始日志字段保持开放。
export const SnapshotConsistencyValues = [
  'single-cluster', // 单集群快照
  'waiting-for-agents', // 等待 Agent
  'consistent-window', // 快照在一致性时间窗内
  'remote-unavailable', // 远端不可用
  'time-skew', // 快照时间偏差
] as const
export type SnapshotConsistency = typeof SnapshotConsistencyValues[number]

// ObservationConfidence 的固定协议值；原始日志字段保持开放。
export const ObservationConfidenceValues = [
  'observed', // 直接日志观测
] as const
export type ObservationConfidence = typeof ObservationConfidenceValues[number]

// ProbeHTTPMethod 的固定协议值；原始日志字段保持开放。
export const ProbeHTTPMethodValues = [
  'GET', // 探测允许的 HTTP 方法
  'HEAD', // 探测允许的 HTTP 方法
  'POST', // 探测允许的 HTTP 方法
  'PUT', // 探测允许的 HTTP 方法
  'PATCH', // 探测允许的 HTTP 方法
  'DELETE', // 探测允许的 HTTP 方法
  'OPTIONS', // 探测允许的 HTTP 方法
] as const
export type ProbeHTTPMethod = typeof ProbeHTTPMethodValues[number]

// ProbeScheme 的固定协议值；原始日志字段保持开放。
export const ProbeSchemeValues = [
  'http', // HTTP 入口
  'https', // HTTPS 入口
] as const
export type ProbeScheme = typeof ProbeSchemeValues[number]

// ProbeCollectionState 的固定协议值；原始日志字段保持开放。
export const ProbeCollectionStateValues = [
  'settled', // 已观察到稳定终止记录。
  'window-ended', // 采集窗口到期，未确认稳定终止记录。
  'cancelled', // 日志采集已取消。
  'read-error', // 日志读取失败。
  'unknown', // 采集状态未确认。
] as const
export type ProbeCollectionState = typeof ProbeCollectionStateValues[number]

// ProbeAttemptRelationState 的固定协议值；原始日志字段保持开放。
export const ProbeAttemptRelationStateValues = [
  'linked', // 已确认尝试关系，包括单条有序访问记录。
  'unconfirmed', // 运行时身份或日志顺序不足，尝试关系未确认。
  'missing-next', // 已发生内部重定向，后续尝试记录缺失。
  'ambiguous', // 存在多个终止候选或重入记录，尝试归属有歧义。
] as const
export type ProbeAttemptRelationState = typeof ProbeAttemptRelationStateValues[number]

export interface ProbeEntry {
  id: string
  gatewayID: string
  clusterID: string
  namespace: string
  serviceName: string
  dnsName: string
  port: number
  scheme: ProbeScheme
  protocol: string
  displayName: string
  addresses?: string[]
}

export interface ProbeRequest {
  sourceCluster: string
  gatewayID: string
  entryID: string
  method: ProbeHTTPMethod
  path: string
  host?: string
  apiKey?: string
  contentType?: string
  body?: string
  timeoutSeconds?: number
}
export interface ObservedHop {
  id: string
  contextID: string
  runtimeSource?: string
  logSourceID?: string
  logSequence?: number
  requestStartTime?: string
  internalRedirect?: boolean
  aiRouting?: { provider?: string; requestModel?: string; upstreamModel?: string; responseModel?: string }
  observedAt: string
  clusterID: string
  pod: string
  authority?: string
  method?: string
  path?: string
  protocol?: string
  routeName?: string
  upstreamCluster?: string
  upstreamHost?: string
  upstreamLocalAddress?: string
  downstreamRemoteAddress?: string
  downstreamLocalAddress?: string
  responseCode?: number
  responseFlags?: string
  responseCodeDetails?: string
  durationMillis?: number
  upstreamServiceTimeMillis?: number
  upstreamTransportFailureReason?: string
  aiLog?: string
  extProcs?: ExtProcObservation[]
  evidenceSource: string
  correlation?: ProbeCorrelation
  confidence: ObservationConfidence
}
export interface ExtProcObservation {
  processor?: string
  ruleID?: string
  selectedPool?: string
  selectedEndpoint?: string
  reasonCode?: string
  requestHeaderCalls?: number
  requestBodyCalls?: number
  responseHeaderCalls?: number
  responseBodyCalls?: number
  requestHeaderLatencyUs?: number
  requestBodyLatencyUs?: number
  responseHeaderLatencyUs?: number
  responseBodyLatencyUs?: number
  grpcStatus?: string
  failureModeAllowed: boolean
  failedOpen: boolean
  messageTimeout: boolean
  httpError: boolean
  receivedImmediateResponse: boolean
  invoked: boolean
  outcome: ExtProcOutcome
}
export interface ProbeIssue {
  scope: ProbeIssueScope
  code: ProbeIssueCode
  message: string
  contextID?: string
  hopID?: string
}
export interface ProbeSegment {
  index: number
  clusterID: string
  gatewayID: string
  gatewayName?: string
  snapshotID?: string
  snapshotObservedAt?: string
  logSource?: string
  transport?: string
  destination?: string
  inferenceBasis?: string
  inferenceConfidence?: ProbeInferenceConfidence
  hops: ObservedHop[]
  collection: ProbeCollection
  relationState: ProbeAttemptRelationState
  links: { from: string; to: string }[]
  localTerminalHopIDs: string[]
  issues: ProbeIssue[]
}
export interface ProbeExecution {
  schemaVersion: 2
  id: string
  traceID: string
  sourceCluster: string
  gatewayID: string
  method: ProbeHTTPMethod
  target: string
  state: ProbeExecutionState
  startedAt: string
  completedAt?: string
  responseCode?: number
  responseBytes?: number
  durationMillis?: number
  segments: ProbeSegment[]
  federatedSnapshotID?: string
  snapshotConsistency?: SnapshotConsistency
  issues: ProbeIssue[]
  error?: string
  finalResponseHopID?: string
  finalUpstreamHopID?: string
}
export interface ProbeCollection {
  state: ProbeCollectionState
  completedAt?: string
}
