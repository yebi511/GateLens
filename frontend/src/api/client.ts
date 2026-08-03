import type {
  EnvoyConfig,
  Finding,
  GateLensContext,
  Resource,
  RouteExplanation,
  RouteExplanationRequest,
  Topology,
} from '../types'

const baseURL = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${baseURL}${path}`, options)
  if (!response.ok) {
    let message = `请求失败 (${response.status})`
    try {
      const body = (await response.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Keep the status-based fallback for non-JSON upstream failures.
    }
    throw new Error(message)
  }
  return response.json() as Promise<T>
}

async function getContext(): Promise<GateLensContext> {
  const value = await request<GateLensContext>('/api/v1/context')
  value.namespaces ??= []
  value.adapterCapabilities ??= []
  return value
}
async function getTopology(): Promise<Topology> {
  const value = await request<Topology>('/api/v1/topology')
  value.nodes ??= []
  value.edges ??= []
  for (const node of value.nodes) node.conditions ??= []
  value.clusters ??= []
  for (const cluster of value.clusters) cluster.namespaces ??= []
  return value
}
async function getEnvoy(gatewayID: string): Promise<EnvoyConfig> {
  const value = await request<EnvoyConfig>(`/api/v1/envoy/config?gatewayID=${encodeURIComponent(gatewayID)}`)
  value.listeners ??= []
  value.clusters ??= []
  value.extensions ??= []
  for (const listener of value.listeners) {
    listener.filterChains ??= []
    for (const chain of listener.filterChains) {
      chain.httpFilters ??= []
      chain.routes ??= []
      for (const route of chain.routes) route.weightedClusters ??= []
    }
  }
  for (const cluster of value.clusters) cluster.endpoints ??= []
  for (const extension of value.extensions) {
    extension.attachments ??= []
    extension.dependencies ??= []
  }
  return value
}
async function explain(payload: RouteExplanationRequest): Promise<RouteExplanation> {
  const value = await request<RouteExplanation>('/api/v1/route-explanations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  value.steps ??= []
  return value
}

export const api = {
  context: getContext,
  topology: getTopology,
  findings: async () => (await request<Finding[] | null>('/api/v1/health/findings')) ?? [],
  resources: async () => (await request<Resource[] | null>('/api/v1/resources')) ?? [],
  envoy: getEnvoy,
  explain,
}
