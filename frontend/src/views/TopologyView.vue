<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import {
  Box,
  Boxes,
  Braces,
  CircleDot,
  CloudCog,
  GitBranch,
  Search,
  Network,
  Server,
  Waypoints,
} from '@lucide/vue'
import StatusBadge from '../components/StatusBadge.vue'
import type { GateLensContext, Topology, TopologyNode, ViewID } from '../types'

const props = defineProps<{
  context: GateLensContext
  topology: Topology
  namespace: string
  focusNodeId?: string
}>()
const emit = defineEmits<{ navigate: [view: ViewID] }>()

const search = ref('')
const problemsOnly = ref(false)
const selectedID = ref('')

const groups = [
  { title: '网关', kinds: ['Gateway', 'GatewayWorkload'] },
  { title: '监听器', kinds: ['Listener'] },
  { title: '路由', kinds: ['HTTPRoute', 'Ingress'] },
  { title: '后端', kinds: ['InferencePool', 'Service', 'McpBridge'] },
  { title: '端点 / 边界', kinds: ['Endpoint', 'TransitHop', 'Registry'] },
]
const icons: Record<string, Component> = {
  Gateway: Waypoints,
  GatewayWorkload: CloudCog,
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

const visibleNodes = computed(() => {
  const query = search.value.trim().toLowerCase()
  return props.topology.nodes.filter((node) =>
    (!props.namespace || node.namespace === props.namespace) &&
    (!problemsOnly.value || node.status !== 'healthy') &&
    (!query || `${node.name} ${node.kind} ${node.namespace}`.toLowerCase().includes(query)),
  )
})
const selected = computed(() => props.topology.nodes.find((node) => node.id === selectedID.value) ?? null)
const incoming = computed(() => props.topology.edges.filter((edge) => edge.to === selectedID.value))
const outgoing = computed(() => props.topology.edges.filter((edge) => edge.from === selectedID.value))
const nodeName = (id: string) => props.topology.nodes.find((node) => node.id === id)?.name ?? id
const nodeIcon = (kind: string) => icons[kind] ?? Box
const nodesIn = (kinds: string[]) => visibleNodes.value.filter((node) => kinds.includes(node.kind))
const hasEnvoyRuntime = computed(() => {
  if (selected.value?.kind !== 'Gateway') return false
  return incoming.value.some((edge) =>
    edge.relation === 'serves' && props.topology.nodes.some((node) => node.id === edge.from && node.kind === 'GatewayWorkload'),
  )
})

watch(
  () => props.focusNodeId,
  (id) => { if (id) selectedID.value = id },
  { immediate: true },
)
watch(
  () => props.topology.nodes,
  (nodes) => {
    if (selectedID.value && !nodes.some((node) => node.id === selectedID.value)) selectedID.value = ''
  },
)

function selectNode(node: TopologyNode) {
  selectedID.value = node.id
}
</script>

<template>
  <section class="view">
    <div class="page-header">
      <div>
        <p class="eyebrow">有效流量图</p>
        <h1>单集群多命名空间拓扑</h1>
        <p>来自快照 {{ context.snapshot.observedAt }}，覆盖 {{ context.namespaces.length }} 个命名空间。</p>
      </div>
      <button class="primary-button" type="button" @click="emit('navigate', 'simulator')">模拟请求</button>
    </div>

    <div class="filter-bar">
      <label class="search-field">
        <Search :size="15" aria-hidden="true" />
        <input v-model="search" placeholder="资源名称或命名空间" />
      </label>
      <label class="check-label"><input v-model="problemsOnly" type="checkbox" />仅问题节点</label>
      <span class="filter-count">{{ visibleNodes.length }} / {{ topology.nodes.length }} 个对象</span>
    </div>

    <div class="topology-workspace">
      <section class="graph-panel">
        <div class="graph-toolbar">
          <span><i class="legend-line" />配置关系</span>
          <span><i class="legend-dot warning" />需要关注</span>
          <span v-if="topology.truncated" class="truncated-note">结果已截断</span>
        </div>
        <div class="topology-canvas">
          <div v-for="group in groups" :key="group.title" class="graph-layer">
            <p>{{ group.title }}</p>
            <button
              v-for="node in nodesIn(group.kinds)"
              :key="node.id"
              class="graph-node"
              :class="[{ selected: selectedID === node.id }, `node-${node.status}`]"
              type="button"
              :title="`${node.kind} ${node.namespace}/${node.name}`"
              @click="selectNode(node)"
            >
              <span class="node-kind"><component :is="nodeIcon(node.kind)" :size="15" aria-hidden="true" /></span>
              <strong>{{ node.name }}</strong>
              <small>{{ node.namespace || '外部' }} · {{ node.kind }}</small>
              <span class="node-state" :class="`text-${node.status}`">{{ node.statusText }}</span>
            </button>
            <div v-if="nodesIn(group.kinds).length === 0" class="empty-layer">无匹配对象</div>
          </div>
        </div>
      </section>

      <aside class="detail-panel">
        <template v-if="selected">
          <StatusBadge :status="selected.status" :label="selected.statusText" />
          <h2>{{ selected.name }}</h2>
          <p class="detail-kind">{{ selected.clusterID }}/{{ selected.namespace || 'external' }} · {{ selected.kind }}</p>
          <section class="detail-section">
            <h3>摘要</h3>
            <p>{{ selected.summary }}</p>
          </section>
          <section class="detail-section">
            <h3>状态条件</h3>
            <ul class="compact-list"><li v-for="condition in selected.conditions" :key="condition">{{ condition }}</li></ul>
            <p v-if="!selected.conditions.length">无</p>
          </section>
          <section class="detail-section">
            <h3>关系</h3>
            <p><b>上游</b> {{ incoming.map((edge) => nodeName(edge.from)).join('、') || '无' }}</p>
            <p><b>下游</b> {{ outgoing.map((edge) => nodeName(edge.to)).join('、') || '无' }}</p>
          </section>
          <section class="detail-section">
            <h3>来源</h3>
            <p>{{ selected.source }}</p>
          </section>
          <button v-if="hasEnvoyRuntime" class="primary-button full-button" type="button" @click="emit('navigate', 'envoy')">
            查看 Envoy 配置
          </button>
        </template>
        <template v-else>
          <div class="empty-detail">
            <Network :size="26" aria-hidden="true" />
            <h2>选择一个对象</h2>
            <p>查看对象位置、状态、来源和关联关系。</p>
          </div>
        </template>
      </aside>
    </div>
  </section>
</template>
