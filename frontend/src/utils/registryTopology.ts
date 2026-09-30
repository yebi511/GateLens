import type { TopologyEdge, TopologyNode } from '../types.js'

export function registryOwners(nodes: TopologyNode[], edges: TopologyEdge[]): Map<string, TopologyNode> {
  const byID = new Map(nodes.map((node) => [node.id, node]))
  const owners = new Map<string, TopologyNode>()
  for (const edge of edges) {
    const bridge = byID.get(edge.from)
    const registry = byID.get(edge.to)
    if (edge.relation === 'discovers' && bridge?.kind === 'McpBridge' && registry?.kind === 'Registry') {
      owners.set(registry.id, bridge)
    }
  }
  return owners
}

export function unresolvedIngressBridges(
  ingressID: string,
  edges: TopologyEdge[],
  nodes: TopologyNode[],
  owners: Map<string, TopologyNode>,
): TopologyNode[] {
  const byID = new Map(nodes.map((node) => [node.id, node]))
  const selectedBridgeIDs = new Set(edges
    .filter((edge) => edge.from === ingressID && edge.relation === 'selects')
    .map((edge) => owners.get(edge.to)?.id)
    .filter((id): id is string => Boolean(id)))
  const unresolved = new Map<string, TopologyNode>()
  for (const edge of edges) {
    if (edge.from !== ingressID || edge.relation !== 'routes') continue
    const bridge = byID.get(edge.to)
    if (bridge?.kind === 'McpBridge' && !selectedBridgeIDs.has(bridge.id)) unresolved.set(bridge.id, bridge)
  }
  return [...unresolved.values()]
}

export function visibleTopologyEdges(edges: TopologyEdge[], visibleIDs: Set<string>): TopologyEdge[] {
  return edges.filter((edge) => visibleIDs.has(edge.from) && visibleIDs.has(edge.to))
}
