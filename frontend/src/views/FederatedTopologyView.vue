<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type Component } from 'vue'
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
  PanelRightClose,
  PanelRightOpen,
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
const detailCollapsed = ref(false)
const columnsElement = ref<HTMLElement | null>(null)
const linkCanvas = ref({ width: 0, height: 0 })
const topologyLinks = ref<Array<{
  id: string
  edge: TopologyEdge
  path: string
  labelX: number
  labelY: number
  label: string
}>>([])
let linkResizeObserver: ResizeObserver | undefined
let linkLayoutFrame = 0

const icons: Record<string, Component> = {
  Gateway: Waypoints,
  Listener: CircleDot,
  HTTPRoute: GitBranch,
  Ingress: GitBranch,
  VirtualService: GitBranch,
  BBR: GitBranch,
  BodyBasedRouting: GitBranch,
  InferencePool: Boxes,
  Service: Server,
  InferenceService: Server,
  ModelService: Server,
  McpBridge: Braces,
  EPP: CircleDot,
  EndpointPicker: CircleDot,
  ExternalTarget: CloudCog,
  Endpoint: Box,
  Pod: Box,
  TransitHop: CloudCog,
  Registry: Braces,
}
const stages: Record<string, number> = {
  Gateway: 1,
  Listener: 1,
  HTTPRoute: 2,
  Ingress: 2,
  VirtualService: 2,
  BBR: 2,
  BodyBasedRouting: 2,
  McpBridge: 3,
  InferencePool: 3,
  Registry: 4,
  EPP: 4,
  EndpointPicker: 4,
  ExternalTarget: 5,
  TransitHop: 5,
  Service: 5,
  InferenceService: 5,
  ModelService: 5,
  Endpoint: 6,
  Pod: 6,
}
const nodeCategories = [
  { id: 'entry', title: '网关入口', kinds: ['Gateway'] },
  { id: 'routing', title: '路由与策略', kinds: ['HTTPRoute', 'Ingress', 'VirtualService', 'BBR', 'BodyBasedRouting'] },
  { id: 'backend', title: '后端抽象', kinds: ['McpBridge', 'InferencePool'] },
  { id: 'discovery', title: '发现与调度', kinds: ['Registry', 'EPP', 'EndpointPicker'] },
  { id: 'target', title: '服务目标', kinds: [] },
  { id: 'runtime', title: '运行实例', kinds: ['Endpoint', 'Pod'] },
]
const knownKinds = new Set(nodeCategories.flatMap((category) => category.kinds))
const hiddenKinds = new Set(['Listener'])
const targetSourceKinds = new Set(
  nodeCategories
    .filter((category) => ['routing', 'backend', 'discovery'].includes(category.id))
    .flatMap((category) => category.kinds),
)
const transferAnchorKinds = new Set([
  'HTTPRoute',
  'Ingress',
  'VirtualService',
  'BBR',
  'BodyBasedRouting',
  'McpBridge',
  'InferencePool',
  'Registry',
  'EPP',
  'EndpointPicker',
  'ExternalTarget',
  'TransitHop',
  'Service',
  'InferenceService',
  'ModelService',
])
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
const listenerOwnerIDs = computed(() => {
  const result = new Map<string, string>()
  for (const edge of props.topology.edges) {
    if (edge.relation !== 'owns' || node(edge.from)?.kind !== 'Gateway' || node(edge.to)?.kind !== 'Listener') continue
    result.set(edge.to, edge.from)
  }
  return result
})
const displayNodeID = (id: string) => listenerOwnerIDs.value.get(id) ?? id
const displayEdges = computed(() => {
  const result = new Map<string, TopologyEdge>()
  for (const edge of props.topology.edges) {
    const from = displayNodeID(edge.from)
    const to = displayNodeID(edge.to)
    if (from === to) continue
    const viaListeners = [edge.from, edge.to]
      .filter((id) => displayNodeID(id) !== id)
      .map((id) => node(id)?.name)
      .filter((name): name is string => Boolean(name))
    const projected = from === edge.from && to === edge.to ? edge : {
      ...edge,
      from,
      to,
      evidence: [edge.evidence, viaListeners.length ? `经 Listener ${viaListeners.join(', ')}` : ''].filter(Boolean).join('; '),
    }
    const key = `${projected.from}:${projected.to}:${projected.relation}:${projected.destination}`
    if (!result.has(key)) result.set(key, projected)
  }
  return [...result.values()]
})
const clusterName = (id: string) => clusters.value.find((item) => item.id === id)?.name ?? id
const currentNodes = computed(() => props.topology.nodes.filter((item) => item.clusterID === activeCluster.value.id))
const displayableNodes = computed(() => currentNodes.value.filter((item) => !hiddenKinds.has(item.kind)))
const visibleNodes = computed(() => {
  const query = search.value.trim().toLowerCase()
  return displayableNodes.value
    .filter((item) =>
      (!props.namespace || item.namespace === props.namespace) &&
      (!problemsOnly.value || item.status !== 'healthy') &&
      (!query || `${item.name} ${item.kind} ${item.namespace}`.toLowerCase().includes(query)),
    )
    .sort((left, right) => (stages[left.kind] ?? 99) - (stages[right.kind] ?? 99) || left.name.localeCompare(right.name))
})
const targetNodeIDs = computed(() => {
  const result = new Set<string>()
  for (const edge of props.topology.edges) {
    const from = node(edge.from)
    const to = node(edge.to)
    if (!from || !to || to.clusterID !== activeCluster.value.id || !targetSourceKinds.has(from.kind)) continue
    if (knownKinds.has(to.kind)) continue
    result.add(to.id)
  }
  return result
})
const groupedNodes = computed(() => {
  const groups = nodeCategories.map((category) => ({
    ...category,
    nodes: visibleNodes.value.filter((item) => category.id === 'target'
      ? targetNodeIDs.value.has(item.id)
      : category.kinds.includes(item.kind)),
  }))
  const otherNodes = visibleNodes.value.filter((item) => !knownKinds.has(item.kind) && !targetNodeIDs.value.has(item.id))
  return otherNodes.length ? [...groups, { id: 'other', title: '其他', kinds: [], nodes: otherNodes }] : groups
})
const selected = computed(() => visibleNodes.value.find((item) => item.id === selectedID.value) ?? null)
const crossEdges = computed(() => displayEdges.value.filter((edge) => {
  const from = node(edge.from)
  const to = node(edge.to)
  return Boolean(from && to && from.clusterID !== to.clusterID)
}))
const selectedEdges = computed(() => displayEdges.value.filter((edge) =>
  (edge.from === selectedID.value || edge.to === selectedID.value) && !isCrossEdge(edge),
))
const selectedTransfers = computed(() => selected.value ? boundaryEdgesFor(selected.value) : [])
const activeTransfers = computed(() => crossEdges.value.filter((edge) =>
  node(edge.from)?.clusterID === activeCluster.value.id || node(edge.to)?.clusterID === activeCluster.value.id,
))
const stateLabel = computed(() => activeCluster.value.connectionState === 'connected' ? '已连接' : activeCluster.value.connectionState === 'stale' ? '快照过期' : '未连接')
const relation = (edge: TopologyEdge) => `${node(edge.from)?.name ?? edge.from} -> ${node(edge.to)?.name ?? edge.to}`
const hasEnvoyRuntime = computed(() => selected.value?.kind === 'Gateway' && selected.value.conditions.includes('EnvoyConfig=available'))

function conditionValue(item: TopologyNode, prefix: string) {
  const condition = item.conditions.find((value) => value.startsWith(prefix))
  return condition?.slice(prefix.length) ?? ''
}

function nodeSubtitle(item: TopologyNode) {
  if (item.kind !== 'Endpoint') return [`${item.namespace || 'cluster-scoped'} / ${item.kind}`]
  const service = conditionValue(item, 'Service=')
  const address = conditionValue(item, 'Address=')
  const port = conditionValue(item, 'Port=')
  const endpoint = address ? `${address}${port ? `:${port}` : ''}` : ''
  const serviceName = service.split('/').pop() || service
  return [serviceName, endpoint].filter(Boolean).length
    ? [serviceName, endpoint].filter(Boolean)
    : [`${item.namespace || 'cluster-scoped'} / Endpoint`]
}

function nodeSubtitleTitle(item: TopologyNode) {
  if (item.kind !== 'Endpoint') return nodeSubtitle(item).join('')
  const service = conditionValue(item, 'Service=')
  const address = conditionValue(item, 'Address=')
  const port = conditionValue(item, 'Port=')
  return [service, address ? `${address}${port ? `:${port}` : ''}` : ''].filter(Boolean).join(' · ')
}

const relationLabels: Record<string, string> = {
  owns: '包含',
  attaches: '挂载',
  routes: '路由',
  transit: '转发',
  discovers: '发现',
  selects: '选择',
  resolves: '解析',
  excludes: '排除',
}

function updateLinkLayout() {
  const canvas = columnsElement.value
  if (!canvas) {
    topologyLinks.value = []
    return
  }
  const canvasRect = canvas.getBoundingClientRect()
  const elements = new Map<string, HTMLElement>()
  canvas.querySelectorAll<HTMLElement>('[data-node-id]').forEach((element) => {
    const id = element.dataset.nodeId
    if (id) elements.set(id, element)
  })
  const links = displayEdges.value.flatMap((edge, index) => {
    const from = node(edge.from)
    const to = node(edge.to)
    const fromElement = elements.get(edge.from)
    const toElement = elements.get(edge.to)
    if (!from || !to || !fromElement || !toElement || isCrossEdge(edge)) return []
    if (from.clusterID !== activeCluster.value.id || to.clusterID !== activeCluster.value.id) return []

    const fromRect = fromElement.getBoundingClientRect()
    const toRect = toElement.getBoundingClientRect()
    const fromCenterX = fromRect.left - canvasRect.left + fromRect.width / 2
    const toCenterX = toRect.left - canvasRect.left + toRect.width / 2
    const fromCenterY = fromRect.top - canvasRect.top + fromRect.height / 2
    const toCenterY = toRect.top - canvasRect.top + toRect.height / 2
    let fromX = fromRect.right - canvasRect.left
    let toX = toRect.left - canvasRect.left
    let fromY = fromCenterY
    let toY = toCenterY
    let path = ''
    let labelX = (fromX + toX) / 2
    let labelY = (fromCenterY + toCenterY) / 2 - 4

    if (toRect.left >= fromRect.right) {
      const bend = Math.max(24, (toX - fromX) * 0.42)
      path = `M ${fromX} ${fromCenterY} C ${fromX + bend} ${fromCenterY}, ${toX - bend} ${toCenterY}, ${toX} ${toCenterY}`
    } else if (toRect.right <= fromRect.left) {
      fromX = fromRect.left - canvasRect.left
      toX = toRect.right - canvasRect.left
      const bend = Math.max(24, (fromX - toX) * 0.42)
      path = `M ${fromX} ${fromCenterY} C ${fromX - bend} ${fromCenterY}, ${toX + bend} ${toCenterY}, ${toX} ${toCenterY}`
      labelX = (fromX + toX) / 2
    } else if (toRect.top >= fromRect.bottom) {
      fromX = fromCenterX
      toX = toCenterX
      fromY = fromRect.bottom - canvasRect.top
      toY = toRect.top - canvasRect.top
      const bend = Math.max(12, (toY - fromY) * 0.5)
      path = `M ${fromX} ${fromY} C ${fromX} ${fromY + bend}, ${toX} ${toY - bend}, ${toX} ${toY}`
      labelX = (fromX + toX) / 2
      labelY = (fromY + toY) / 2 - 4
    } else {
      fromX = fromCenterX
      toX = toCenterX
      fromY = fromRect.top - canvasRect.top
      toY = toRect.bottom - canvasRect.top
      const bend = Math.max(12, (fromY - toY) * 0.5)
      path = `M ${fromX} ${fromY} C ${fromX} ${fromY - bend}, ${toX} ${toY + bend}, ${toX} ${toY}`
      labelX = (fromX + toX) / 2
      labelY = (fromY + toY) / 2 - 4
    }

    return [{
      id: `${edge.from}:${edge.to}:${edge.relation}:${index}`,
      edge,
      path,
      labelX,
      labelY,
      label: relationLabels[edge.relation] ?? edge.relation,
    }]
  })
  linkCanvas.value = {
    width: Math.max(canvas.clientWidth, canvas.scrollWidth),
    height: Math.max(canvas.clientHeight, canvas.scrollHeight),
  }
  topologyLinks.value = links
}

function scheduleLinkLayout() {
  cancelAnimationFrame(linkLayoutFrame)
  linkLayoutFrame = requestAnimationFrame(updateLinkLayout)
}

async function refreshLinkLayout() {
  await nextTick()
  linkResizeObserver?.disconnect()
  const canvas = columnsElement.value
  if (!canvas) {
    topologyLinks.value = []
    return
  }
  linkResizeObserver = new ResizeObserver(scheduleLinkLayout)
  linkResizeObserver.observe(canvas)
  canvas.querySelectorAll<HTMLElement>('[data-node-id]').forEach((element) => linkResizeObserver?.observe(element))
  scheduleLinkLayout()
}

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

function selectNode(id: string) {
  selectedID.value = id
  detailCollapsed.value = false
}

function hideDetailOnBlankClick(event: MouseEvent) {
  const target = event.target
  if (!(target instanceof Element)) return
  if (target.closest('button, a, input, select, textarea, label, [role="button"], .federated-detail')) return
  selectedID.value = ''
  detailCollapsed.value = false
}

watch(() => props.focusNodeId, (id) => {
  const displayID = id ? displayNodeID(id) : ''
  if (displayID && currentNodes.value.some((item) => item.id === displayID)) selectNode(displayID)
}, { immediate: true })
watch(() => activeCluster.value.id, () => {
  selectedID.value = ''
  search.value = ''
  problemsOnly.value = false
})
watch(visibleNodes, (items) => {
  if (selectedID.value && !items.some((item) => item.id === selectedID.value)) selectedID.value = ''
})
watch([visibleNodes, displayEdges, groupedNodes], refreshLinkLayout, { deep: true, flush: 'post' })
onMounted(refreshLinkLayout)
onBeforeUnmount(() => {
  cancelAnimationFrame(linkLayoutFrame)
  linkResizeObserver?.disconnect()
})
</script>

<template>
  <section class="view federated-view" @click="hideDetailOnBlankClick">
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
      <span class="filter-count">{{ visibleNodes.length }} / {{ displayableNodes.length }} 个对象</span>
    </div>

    <div class="federated-workspace single-cluster-workspace" :class="{ 'detail-collapsed': !selected || detailCollapsed }">
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
          <div v-if="visibleNodes.length" class="cluster-columns-scroll">
            <div ref="columnsElement" class="cluster-columns" :style="{ '--category-count': groupedNodes.length }">
              <svg
                v-if="topologyLinks.length"
                class="topology-links"
                :width="linkCanvas.width"
                :height="linkCanvas.height"
                :viewBox="`0 0 ${linkCanvas.width} ${linkCanvas.height}`"
                aria-hidden="true"
              >
                <g
                  v-for="link in topologyLinks"
                  :key="link.id"
                  class="topology-link"
                  :class="[
                    `relation-${link.edge.relation}`,
                    {
                      active: selectedID && (link.edge.from === selectedID || link.edge.to === selectedID),
                      muted: selectedID && link.edge.from !== selectedID && link.edge.to !== selectedID,
                    },
                  ]"
                >
                  <path class="topology-link-path" :d="link.path" />
                  <text class="topology-link-label" :x="link.labelX" :y="link.labelY" text-anchor="middle">{{ link.label }}</text>
                </g>
              </svg>
              <section v-for="category in groupedNodes" :key="category.id" class="topology-column">
                <header class="topology-column-header">
                  <strong>{{ category.title }}</strong>
                  <span>{{ category.nodes.length }}</span>
                </header>
                <div class="topology-column-nodes">
                  <button
                    v-for="item in category.nodes"
                    :key="item.id"
                    class="federated-node"
                    :class="[{ selected: selectedID === item.id, boundary: boundaryEdgesFor(item).length }, `node-${item.status}`]"
                    :data-node-id="item.id"
                    type="button"
                    @click="selectNode(item.id)"
                  >
                    <component :is="icons[item.kind] || Box" :size="15" aria-hidden="true" />
                    <span>
                      <strong :title="item.name">{{ item.name }}</strong>
                      <small :title="nodeSubtitleTitle(item)">
                        <span v-for="line in nodeSubtitle(item)" :key="line">{{ line }}</span>
                      </small>
                    </span>
                    <em>{{ item.statusText }}</em>
                    <span v-if="boundaryEdgesFor(item)[0]" class="boundary-target">
                      <CloudCog :size="12" aria-hidden="true" />{{ transferLabel(boundaryEdgesFor(item)[0]) }}
                    </span>
                  </button>
                  <div v-if="!category.nodes.length" class="topology-column-empty">当前无对象</div>
                </div>
              </section>
            </div>
          </div>
          <div v-else class="cluster-empty">
            <Network :size="24" aria-hidden="true" />
            <strong>当前筛选下没有拓扑对象</strong>
          </div>
        </section>
      </section>

      <button
        v-if="selected && detailCollapsed"
        class="icon-button detail-reopen-button"
        type="button"
        title="展开详情"
        aria-label="展开详情"
        @click="detailCollapsed = false"
      >
        <PanelRightOpen :size="17" aria-hidden="true" />
      </button>

      <aside v-if="selected && !detailCollapsed" class="detail-panel federated-detail">
        <button
          class="icon-button detail-collapse-button"
          type="button"
          title="收起详情"
          aria-label="收起详情"
          @click="detailCollapsed = true"
        >
          <PanelRightClose :size="17" aria-hidden="true" />
        </button>
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
