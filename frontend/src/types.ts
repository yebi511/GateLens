export type Status = 'healthy' | 'warning' | 'error'
export type ViewID = 'topology' | 'envoy' | 'simulator' | 'health' | 'resources'

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
export interface TopologyEdge { from: string; to: string; relation: string; transport?: string }
export interface Topology {
  snapshotID: string
  observedAt: string
  nodes: TopologyNode[]
  edges: TopologyEdge[]
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
}
export interface EnvoyHTTPFilter {
  name: string
  type: string
  stage: string
  configSummary: string
  terminal: boolean
}
export interface EnvoyFilterChain {
  name: string
  match: string
  transport: string
  httpFilters: EnvoyHTTPFilter[]
  routes: EnvoyRoute[]
}
export interface EnvoyListener {
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
}
export interface RouteExplanationRequest {
  snapshotID: string
  gateway: string
  listener: string
  method: string
  host: string
  path: string
  namespace: string
  model: string
}
export interface ExplainStep { hop: number; title: string; detail: string; state: string; targetID: string }
export interface RouteExplanation {
  snapshotID: string
  observedAt: string
  outcome: string
  confidence: string
  summary: string
  steps: ExplainStep[]
}
