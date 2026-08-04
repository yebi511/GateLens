<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import {
  ArrowDownRight,
  ArrowUpLeft,
  Box,
  Boxes,
  Braces,
  CircleDot,
  CloudCog,
  GitBranch,
  Network,
  Search,
  Server,
  Waypoints,
} from '@lucide/vue'
import StatusBadge from '../components/StatusBadge.vue'
import type { GateLensContext, Topology, TopologyCluster, TopologyEdge, TopologyNode, ViewID } from '../types'

const props = defineProps<{
  context: GateLensContext
  topology: Topology
  clusterId: string
  namespace: string
  focusNodeId?: string
}>()
const emit = defineEmits<{ navigate: [view: ViewID]; openEnvoy: [gatewayID: string] }>()
const search = ref('')
const problemsOnly = ref(false)
const selectedID = ref('')

const icons: Record<string, Component> = {
  Gateway: Waypoints,
  Listener: CircleDot,
  HTTPRoute: GitBranch,
  Ingress: GitBranch,
  InferencePool: Boxes,
  Service: Server,
  McpBridge: Braces,
  Endpoint: Box,
  TransitHop: CloudCog,
  Registry: Braces,
}
const stages: Record<string, number> = {
  Gateway: 1,
  Listener: 2,
  HTTPRoute: 3,
  Ingress: 3,
  McpBridge: 4,
  Registry: 4,
  TransitHop: 4,
  Service: 5,
  InferencePool: 6,
  Endpoint: 7,
}
const transferAnchorKinds = new Set(['HTTPRoute', 'Ingress', 'McpBridge', 'Registry', 'TransitHop', 'Service'])
const fallbackCluster = (): TopologyCluster => ({
  id: props.context.cluster.id,
  name: props.context.cluster.name,
  version: props.context.cluster.version,
  connectionState: 'connected',
  namespaces: props.context.namespaces.filter((item) => item !== 'all'),
  snapshot: props.context.snapshot,
})
const clusters = computed(() => props.topology.clusters?.length ? props.topology.clusters : [fallbackCluster()])
const activeCluster = computed(() => clusters.value.find((item) => item.id === props.clusterId) ?? clusters.value[0] ?? fallbackCluster())
const node = (id: string) => props.topology.nodes.find((item) => item.id === id)
const clusterName = (id: string) => clusters.value.find((item) => item.id === id)?.name ?? id
const currentNodes = computed(() => props.topology.nodes.filter((item) => item.clusterID === activeCluster.value.id))
const visibleNodes = computed(() => {
  const query = search.value.trim().toLowerCase()
  return currentNodes.value
    .filter((item) =>
      (!props.namespace || item.namespace === props.namespace) &&
      (!problemsOnly.value || item.status !== 'healthy') &&
      (!query || `${item.name} ${item.kind} ${item.namespace}`.toLowerCase().includes(query)),
    )
    .sort((left, right) => (stages[left.kind] ?? 99) - (stages[right.kind] ?? 99) || left.name.localeCompare(right.name))
})
const selected = computed(() => visibleNodes.value.find((item) => item.id === selectedID.value) ?? null)
const crossEdges = computed(() => props.topology.edges.filter((edge) => {
  const from = node(edge.from)
  const to = node(edge.to)
  return Boolean(from && to && from.clusterID !== to.clusterID)
}))
const selectedEdges = computed(() => props.topology.edges.filter((edge) =>
  (edge.from === selectedID.value || edge.to === selectedID.value) && !isCrossEdge(edge),
))
const selectedTransfers = computed(() => selected.value ? boundaryEdgesFor(selected.value) : [])
const activeTransfers = computed(() => crossEdges.value.filter((edge) =>
  node(edge.from)?.clusterID === activeCluster.value.id || node(edge.to)?.clusterID === activeCluster.value.id,
))
const stateLabel = computed(() => activeCluster.value.connectionState === 'connected' ? '已连接' : activeCluster.value.connectionState === 'stale' ? '快照过期' : '未连接')
const relation = (edge: TopologyEdge) => `${node(edge.from)?.name ?? edge.from} -> ${node(edge.to)?.name ?? edge.to}`
const hasEnvoyRuntime = computed(() => selected.value?.kind === 'Gateway' && selected.value.conditions.includes('EnvoyConfig=available'))

function isCrossEdge(edge: TopologyEdge) {
  const from = node(edge.from)
  const to = node(edge.to)
  return Boolean(from && to && from.clusterID !== to.clusterID)
}
function canReachBoundary(startID: string, boundaryID: string, maxDepth = 2) {
  let frontier = [startID]
  const visited = new Set(frontier)
  for (let depth = 0; depth < maxDepth; depth += 1) {
    const next: string[] = []
    for (const current of frontier) {
      for (const edge of props.topology.edges) {
        if (edge.from !== current || isCrossEdge(edge)) continue
        const target = node(edge.to)
        if (!target || target.clusterID !== activeCluster.value.id) continue
        if (edge.to === boundaryID) return true
        if (!visited.has(edge.to)) {
          visited.add(edge.to)
          next.push(edge.to)
        }
      }
    }
    frontier = next
  }
  return false
}
function boundaryEdgesFor(item: TopologyNode) {
  const matches = crossEdges.value.filter((edge) => {
    if (edge.from === item.id || edge.to === item.id) return true
    if (!transferAnchorKinds.has(item.kind)) return false
    return node(edge.from)?.clusterID === activeCluster.value.id && canReachBoundary(item.id, edge.from)
  })
  return [...new Map(matches.map((edge) => [`${edge.from}:${edge.to}`, edge])).values()]
}
function remoteNode(edge: TopologyEdge) {
  return node(edge.from)?.clusterID === activeCluster.value.id ? node(edge.to) : node(edge.from)
}
function transferDirection(edge: TopologyEdge) {
  return node(edge.from)?.clusterID === activeCluster.value.id ? 'outbound' : 'inbound'
}
function transferLabel(edge: TopologyEdge) {
  const remote = remoteNode(edge)
  const prefix = transferDirection(edge) === 'outbound' ? '流向' : '来自'
  return `${prefix} ${clusterName(remote?.clusterID ?? '')}`
}
function targetLabel(edge: TopologyEdge) {
  const remote = remoteNode(edge)
  if (!remote) return edge.destination || '未知目标'
  return `${remote.kind} · ${remote.namespace || 'cluster-scoped'}/${remote.name}`
}

watch(() => props.focusNodeId, (id) => {
  if (id && currentNodes.value.some((item) => item.id === id)) selectedID.value = id
}, { immediate: true })
watch(() => activeCluster.value.id, () => {
  selectedID.value = ''
  search.value = ''
  problemsOnly.value = false
})
watch(visibleNodes, (items) => {
  if (selectedID.value && !items.some((item) => item.id === selectedID.value)) selectedID.value = ''
})
</script>

<template>
  <section class="view federated-view">
    <div class="page-header">
      <div>
        <p class="eyebrow">集群拓扑</p>
        <h1>{{ activeCluster.name }}</h1>
        <p>{{ activeCluster.id }} · {{ activeCluster.version }} · {{ activeCluster.namespaces.length }} 个命名空间</p>
      </div>
      <button class="primary-button" type="button" @click="emit('navigate', 'simulator')">模拟请求</button>
    </div>

    <div class="federated-context single-cluster-context">
      <div class="federated-cluster-summary">
        <span>当前集群</span>
        <strong>{{ activeCluster.name }}</strong>
        <small>{{ activeCluster.snapshot.observedAt }} / {{ stateLabel }}</small>
      </div>
      <div class="federated-snapshot">
        <span>联邦快照</span>
        <strong>{{ topology.federatedSnapshotID || topology.snapshotID }}</strong>
        <small>{{ activeTransfers.length }} 条跨集群边界 / {{ topology.consistency || activeCluster.snapshot.state }}</small>
      </div>
    </div>

    <div class="filter-bar">
      <label class="search-field">
        <Search :size="15" aria-hidden="true" />
        <input v-model="search" placeholder="搜索当前集群的资源或命名空间" />
      </label>
      <label class="check-label"><input v-model="problemsOnly" type="checkbox" />仅问题节点</label>
      <span class="filter-count">{{ visibleNodes.length }} / {{ currentNodes.length }} 个对象</span>
    </div>

    <div class="federated-workspace single-cluster-workspace">
      <section class="federated-board">
        <section class="cluster-zone">
          <header>
            <div>
              <span>拓扑对象</span>
              <h2>{{ activeCluster.name }}</h2>
              <small>仅显示 {{ activeCluster.id }} 内的配置与工作负载</small>
            </div>
            <StatusBadge :status="activeCluster.connectionState === 'connected' ? 'healthy' : 'warning'" :label="stateLabel" />
          </header>
          <div v-if="visibleNodes.length" class="cluster-flow">
            <button
              v-for="item in visibleNodes"
              :key="item.id"
              class="federated-node"
              :class="[{ selected: selectedID === item.id, boundary: boundaryEdgesFor(item).length }, `node-${item.status}`]"
              type="button"
              @click="selectedID = item.id"
            >
              <component :is="icons[item.kind] || Box" :size="15" aria-hidden="true" />
              <span>
                <strong>{{ item.name }}</strong>
                <small>{{ item.namespace || 'cluster-scoped' }} / {{ item.kind }}</small>
              </span>
              <em>{{ item.statusText }}</em>
              <span v-if="boundaryEdgesFor(item)[0]" class="boundary-target">
                <CloudCog :size="12" aria-hidden="true" />{{ transferLabel(boundaryEdgesFor(item)[0]) }}
              </span>
            </button>
          </div>
          <div v-else class="cluster-empty">
            <Network :size="24" aria-hidden="true" />
            <strong>当前筛选下没有拓扑对象</strong>
          </div>
        </section>
      </section>

      <aside class="detail-panel federated-detail">
        <template v-if="selected">
          <StatusBadge :status="selected.status" :label="selected.statusText" />
          <h2>{{ selected.name }}</h2>
          <p class="detail-kind">{{ selected.kind }} / {{ activeCluster.name }}/{{ selected.namespace || 'cluster-scoped' }}</p>

          <section v-if="selectedTransfers.length" class="detail-section transfer-section">
            <h3>跨集群传输</h3>
            <article v-for="edge in selectedTransfers" :key="`${edge.from}-${edge.to}`" class="boundary-card">
              <header>
                <component :is="transferDirection(edge) === 'outbound' ? ArrowDownRight : ArrowUpLeft" :size="16" aria-hidden="true" />
                <div><span>{{ transferDirection(edge) === 'outbound' ? '流向集群' : '来源集群' }}</span><strong>{{ clusterName(remoteNode(edge)?.clusterID ?? '') }}</strong></div>
              </header>
              <dl>
                <div><dt>目标</dt><dd>{{ targetLabel(edge) }}</dd></div>
                <div v-if="edge.destination"><dt>地址 / 引用</dt><dd><code>{{ edge.destination }}</code></dd></div>
                <div><dt>传输</dt><dd>{{ edge.transport || 'unknown' }} · {{ edge.state || 'unknown' }}</dd></div>
                <div v-if="edge.evidence"><dt>证据</dt><dd>{{ edge.evidence }}</dd></div>
              </dl>
            </article>
          </section>

          <section class="detail-section">
            <h3>摘要</h3>
            <p>{{ selected.summary }}</p>
          </section>
          <section class="detail-section">
            <h3>配置位置</h3>
            <p>{{ activeCluster.name }}/{{ selected.namespace || 'cluster-scoped' }}</p>
          </section>
          <section v-if="selected.workloadScope" class="detail-section">
            <h3>运行工作负载</h3>
            <p>{{ activeCluster.name }}/{{ selected.workloadScope }}</p>
          </section>
          <section class="detail-section">
            <h3>本集群关系</h3>
            <ul class="compact-list">
              <li v-for="edge in selectedEdges" :key="`${edge.from}-${edge.to}`">
                {{ relation(edge) }}
                <small v-if="edge.transport">{{ edge.transport }} / {{ edge.destination || edge.state }}</small>
                <small v-if="edge.evidence">{{ edge.evidence }}</small>
              </li>
            </ul>
            <p v-if="!selectedEdges.length">-</p>
          </section>
          <section class="detail-section">
            <h3>来源</h3>
            <p>{{ selected.source }}</p>
          </section>
          <button v-if="hasEnvoyRuntime" class="primary-button full-button" type="button" @click="emit('openEnvoy', selected.id)">查看 Envoy 配置</button>
        </template>
        <template v-else>
          <div class="empty-detail">
            <Network :size="26" aria-hidden="true" />
            <h2>选择一个对象</h2>
            <p>查看本集群关系、配置证据和跨集群目标。</p>
          </div>
        </template>
      </aside>
    </div>
  </section>
</template>
