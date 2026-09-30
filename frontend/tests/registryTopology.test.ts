import test from 'node:test'
import assert from 'node:assert/strict'
import { registryOwners, unresolvedIngressBridges, visibleTopologyEdges } from '../src/utils/registryTopology.js'
import type { TopologyEdge, TopologyNode } from '../src/types.js'

function node(id: string, kind: string, name = id): TopologyNode {
  return { id, kind, name, namespace: 'higress-system', clusterID: 'edge', status: 'healthy', statusText: '已发现', summary: '', conditions: [], source: '' }
}

const ingress = node('ingress', 'Ingress')
const bridge = node('bridge', 'McpBridge', 'shared-bridge')
const first = node('registry/1', 'Registry', 'same-name')
const second = node('registry/2', 'Registry', 'same-name')
const target = node('service', 'Service')
const nodes = [ingress, bridge, first, second, target]
const baseEdges: TopologyEdge[] = [
  { from: ingress.id, to: bridge.id, relation: 'routes' },
  { from: bridge.id, to: first.id, relation: 'discovers' },
  { from: bridge.id, to: second.id, relation: 'discovers' },
  { from: first.id, to: target.id, relation: 'resolves' },
]

test('multiple registries keep their bridge provenance without a synthetic selection', () => {
  const owners = registryOwners(nodes, baseEdges)
  assert.equal(owners.get(first.id)?.id, bridge.id)
  assert.equal(owners.get(second.id)?.id, bridge.id)
  assert.deepEqual(unresolvedIngressBridges(ingress.id, baseEdges, nodes, owners).map((item) => item.id), [bridge.id])
  const visible = visibleTopologyEdges(baseEdges, new Set([ingress.id, first.id, second.id, target.id]))
  assert.deepEqual(visible.map((edge) => edge.relation), ['resolves'])
})

test('an explicit selection remains drawable with multiple registries', () => {
  const edges = [...baseEdges, { from: ingress.id, to: first.id, relation: 'selects' }]
  const owners = registryOwners(nodes, edges)
  assert.deepEqual(unresolvedIngressBridges(ingress.id, edges, nodes, owners), [])
  assert.deepEqual(visibleTopologyEdges(edges, new Set([ingress.id, first.id, target.id]))
    .map((edge) => edge.relation), ['resolves', 'selects'])
})

test('a sole registry is drawable only when selection evidence exists', () => {
  const soleNodes = [ingress, bridge, first]
  const edges = [
    { from: ingress.id, to: bridge.id, relation: 'routes' },
    { from: bridge.id, to: first.id, relation: 'discovers' },
  ]
  const owners = registryOwners(soleNodes, edges)
  assert.deepEqual(unresolvedIngressBridges(ingress.id, edges, soleNodes, owners).map((item) => item.id), [bridge.id])
  const selectedEdges = [...edges, { from: ingress.id, to: first.id, relation: 'selects' }]
  assert.deepEqual(unresolvedIngressBridges(ingress.id, selectedEdges, soleNodes, owners), [])
  assert.deepEqual(visibleTopologyEdges(selectedEdges, new Set([ingress.id, first.id]))
    .map((edge) => edge.relation), ['selects'])
})

test('a bridge without registries remains an unresolved reference', () => {
  const edges = [{ from: ingress.id, to: bridge.id, relation: 'routes' }]
  assert.deepEqual(unresolvedIngressBridges(ingress.id, edges, [ingress, bridge], registryOwners([ingress, bridge], edges))
    .map((item) => item.id), [bridge.id])
  assert.deepEqual(visibleTopologyEdges(edges, new Set([ingress.id])), [])
})
