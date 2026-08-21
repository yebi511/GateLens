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
  kind: string
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
  consistency?: string
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
export interface ProbeEntry {
  id: string
  gatewayID: string
  clusterID: string
  namespace: string
  serviceName: string
  dnsName: string
  port: number
  scheme: string
  protocol: string
  displayName: string
  addresses?: string[]
}

export interface ProbeRequest {
  sourceCluster: string
  gatewayID: string
  entryID: string
  method: string
  path: string
  host?: string
  apiKey?: string
  contentType?: string
  body?: string
  timeoutSeconds?: number
}
export interface ObservedHop {
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
  responseCode?: number
  responseFlags?: string
  responseCodeDetails?: string
  durationMillis?: number
  upstreamServiceTimeMillis?: number
  upstreamTransportFailureReason?: string
  aiLog?: string
  extProcs?: ExtProcObservation[]
  evidenceSource: string
  correlation?: string
  confidence: string
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
  outcome: 'success' | 'error' | 'timeout' | 'fail-open' | 'unknown' | string
}
export interface ProbeSegment {
  index: number
  clusterID: string
  gatewayID: string
  gatewayName?: string
  snapshotID?: string
  observedAt?: string
  state: string
  evidence: string
  logSource?: string
  transport?: string
  destination?: string
  inferenceBasis?: string
  inferenceConfidence?: 'high' | 'medium' | 'low' | 'ambiguous'
  hops: ObservedHop[]
  gaps: string[]
}
export interface ProbeExecution {
  id: string
  traceID: string
  sourceCluster: string
  gatewayID: string
  method: string
  target: string
  state: string
  startedAt: string
  completedAt?: string
  responseCode?: number
  responseBytes?: number
  durationMillis?: number
  logSource: string
  hops: ObservedHop[]
  segments: ProbeSegment[]
  federatedSnapshotID?: string
  snapshotConsistency?: string
  gaps: string[]
  error?: string
  evidenceComplete: boolean
}
