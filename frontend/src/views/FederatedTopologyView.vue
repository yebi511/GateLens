<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import { Box, Boxes, Braces, CircleDot, CloudCog, GitBranch, Network, Search, Server, Waypoints } from '@lucide/vue'
import StatusBadge from '../components/StatusBadge.vue'
import type { GateLensContext, Topology, TopologyCluster, TopologyEdge, TopologyNode, ViewID } from '../types'
const props = defineProps<{ context: GateLensContext; topology: Topology; namespace: string; focusNodeId?: string }>()
const emit = defineEmits<{ navigate: [view: ViewID]; openEnvoy: [gatewayID: string] }>()
const search = ref('')
const problemsOnly = ref(false)
const selectedID = ref('')
const c = { eyebrow: '\u8054\u90a6\u6d41\u91cf\u89c6\u56fe', title: '\u8de8\u96c6\u7fa4\u6d41\u5411\u603b\u89c8', desc: '\u4ece\u5165\u53e3\u7f51\u5173\u5230 GPU \u63a8\u7406\u670d\u52a1\u7684\u914d\u7f6e\u5173\u7cfb\u3001\u4f20\u8f93\u8fb9\u754c\u4e0e\u5feb\u7167\u8bc1\u636e\u3002', simulate: '\u6a21\u62df\u8bf7\u6c42', search: '\u641c\u7d22\u8d44\u6e90\u3001\u547d\u540d\u7a7a\u95f4\u6216\u96c6\u7fa4', problems: '\u4ec5\u95ee\u9898\u8282\u70b9', objects: '\u4e2a\u5bf9\u8c61', ingress: '\u5165\u53e3\u7f51\u5173\u96c6\u7fa4', gpu: 'GPU \u63a8\u7406\u96c6\u7fa4', related: '\u5df2\u63a5\u5165\u5173\u8054\u96c6\u7fa4', snapshot: '\u8054\u90a6\u5feb\u7167', consistent: '\u4e00\u81f4\u7a97\u53e3\u5185', transport: '\u8de8\u96c6\u7fa4\u4f20\u8f93\u8fb9\u754c', config: '\u914d\u7f6e\u5bf9\u8c61\u4f4d\u7f6e', runtime: '\u8fd0\u884c\u5de5\u4f5c\u8d1f\u8f7d\u4f4d\u7f6e', relationships: '\u5173\u8054\u4e0e\u8bc1\u636e', source: '\u6765\u6e90', select: '\u9009\u62e9\u4e00\u4e2a\u5bf9\u8c61', selectHint: '\u67e5\u770b\u5176\u96c6\u7fa4\u4f4d\u7f6e\u3001\u8de8\u96c6\u7fa4\u8fb9\u754c\u548c\u914d\u7f6e\u8bc1\u636e\u3002' }
const icons: Record<string, Component> = { Gateway: Waypoints, Listener: CircleDot, HTTPRoute: GitBranch, Ingress: GitBranch, InferencePool: Boxes, Service: Server, McpBridge: Braces, Endpoint: Box, TransitHop: CloudCog, Registry: Braces }
const stages: Record<string, number> = { Gateway: 1, Listener: 2, HTTPRoute: 3, Ingress: 3, TransitHop: 4, Service: 5, InferencePool: 6, Endpoint: 7, McpBridge: 5, Registry: 7 }
const clusters = computed<TopologyCluster[]>(() => props.topology.clusters?.length ? props.topology.clusters : [{ id: props.context.cluster.id, name: props.context.cluster.name, role: 'ingress', version: props.context.cluster.version, connectionState: 'connected', namespaces: props.context.namespaces, snapshot: props.context.snapshot }])
const node = (id: string) => props.topology.nodes.find((item) => item.id === id)
const rawVisible = computed(() => { const q = search.value.trim().toLowerCase(); return props.topology.nodes.filter((item) => (!props.namespace || item.namespace === props.namespace) && (!problemsOnly.value || item.status !== 'healthy') && (!q || `${item.name} ${item.kind} ${item.namespace} ${item.clusterID}`.toLowerCase().includes(q))) })
const visibleIDs = computed(() => { const ids = new Set(rawVisible.value.map((item) => item.id)); for (const edge of props.topology.edges) if (ids.has(edge.from) || ids.has(edge.to)) { ids.add(edge.from); ids.add(edge.to) }; return ids })
const visibleNodes = computed(() => props.topology.nodes.filter((item) => visibleIDs.value.has(item.id)))
const selected = computed(() => node(selectedID.value) ?? null)
const selectedEdges = computed(() => props.topology.edges.filter((edge) => edge.from === selectedID.value || edge.to === selectedID.value))
const crossEdges = computed(() => props.topology.edges.filter((edge) => node(edge.from)?.clusterID !== node(edge.to)?.clusterID))
const clusterNodes = (id: string) => visibleNodes.value.filter((item) => item.clusterID === id).sort((a, b) => (stages[a.kind] ?? 99) - (stages[b.kind] ?? 99) || a.name.localeCompare(b.name))
const clusterLabel = (cluster: TopologyCluster) => cluster.id === props.context.cluster.id ? c.ingress : cluster.role === 'gpu-inference' ? c.gpu : c.related
const stateLabel = (cluster: TopologyCluster) => cluster.connectionState === 'connected' ? '\u5df2\u9a8c\u8bc1' : '\u8fdc\u7aef\u672a\u63a5\u5165'
const relation = (edge: TopologyEdge) => `${node(edge.from)?.name ?? edge.from} -> ${node(edge.to)?.name ?? edge.to}`
const resolvedBoundary = (item: TopologyNode) => item.kind === 'TransitHop' && crossEdges.value.some((edge) => edge.from === item.id && edge.state === 'resolved')
const displayStatus = (item: TopologyNode) => resolvedBoundary(item) ? '\u5df2\u9a8c\u8bc1' : item.statusText
watch(() => props.focusNodeId, (id) => { if (id) selectedID.value = id }, { immediate: true })
watch(() => props.topology.nodes, (items) => { if (selectedID.value && !items.some((item) => item.id === selectedID.value)) selectedID.value = '' })
</script>
<template>
  <section class="view federated-view">
    <div class="page-header"><div><p class="eyebrow">{{ c.eyebrow }}</p><h1>{{ c.title }}</h1><p>{{ c.desc }}</p></div><button class="primary-button" type="button" @click="emit('navigate', 'simulator')">{{ c.simulate }}</button></div>
    <div class="federated-context"><div class="federated-snapshot"><span>{{ c.snapshot }}</span><strong>{{ topology.federatedSnapshotID || topology.snapshotID }}</strong><small>{{ topology.consistency === 'consistent-window' ? c.consistent : topology.consistency }}</small></div><div v-for="cluster in clusters" :key="cluster.id" class="federated-cluster-summary" :class="{ ingress: cluster.id === context.cluster.id }"><span>{{ clusterLabel(cluster) }}</span><strong>{{ cluster.name }}</strong><small>{{ cluster.snapshot.observedAt }} / {{ stateLabel(cluster) }}</small></div></div>
    <div class="filter-bar"><label class="search-field"><Search :size="15" aria-hidden="true" /><input v-model="search" :placeholder="c.search" /></label><label class="check-label"><input v-model="problemsOnly" type="checkbox" />{{ c.problems }}</label><span class="filter-count">{{ visibleNodes.length }} / {{ topology.nodes.length }} {{ c.objects }}</span></div>
    <div class="federated-workspace">
      <section class="federated-board">
        <template v-for="(cluster, index) in clusters" :key="cluster.id">
          <section class="cluster-zone" :class="{ ingress: cluster.id === context.cluster.id }"><header><div><span>{{ clusterLabel(cluster) }}</span><h2>{{ cluster.name }}</h2><small>{{ cluster.version }} / {{ cluster.namespaces.length }} namespaces</small></div><StatusBadge :status="cluster.connectionState === 'connected' ? 'healthy' : 'warning'" :label="stateLabel(cluster)" /></header><div class="cluster-flow"><button v-for="item in clusterNodes(cluster.id)" :key="item.id" class="federated-node" :class="[{ selected: selectedID === item.id }, `node-${resolvedBoundary(item) ? 'healthy' : item.status}`, { boundary: item.kind === 'TransitHop' }]" type="button" @click="selectedID = item.id"><component :is="icons[item.kind] || Box" :size="15" aria-hidden="true" /><span><strong>{{ item.name }}</strong><small>{{ item.namespace || 'external' }} / {{ item.kind }}</small></span><em>{{ displayStatus(item) }}</em></button></div></section>
          <section v-if="index < clusters.length - 1" class="transport-bridge"><template v-for="edge in crossEdges.filter((item) => node(item.from)?.clusterID === cluster.id)" :key="`${edge.from}-${edge.to}`"><CloudCog :size="17" aria-hidden="true" /><div><span>{{ c.transport }}</span><strong>{{ edge.transport || 'unknown' }} / {{ edge.destination || relation(edge) }}</strong><small>{{ edge.state || 'unknown' }} / {{ edge.evidence || 'configuration evidence' }}</small></div></template></section>
        </template>
      </section>
      <aside class="detail-panel federated-detail"><template v-if="selected"><StatusBadge :status="selected.status" :label="selected.statusText" /><h2>{{ selected.name }}</h2><p class="detail-kind">{{ selected.kind }} / {{ selected.clusterID }}/{{ selected.namespace || 'external' }}</p><section class="detail-section"><h3>{{ c.config }}</h3><p>{{ selected.clusterID }}/{{ selected.namespace || 'cluster-scoped' }}</p></section><section class="detail-section"><h3>{{ c.runtime }}</h3><p>{{ selected.workloadScope || selected.clusterID + '/' + (selected.namespace || 'external') }}</p></section><section class="detail-section"><h3>{{ c.relationships }}</h3><ul class="compact-list"><li v-for="edge in selectedEdges" :key="`${edge.from}-${edge.to}`">{{ relation(edge) }}<small v-if="edge.transport">{{ edge.transport }} / {{ edge.destination || edge.state }}</small><small v-if="edge.evidence">{{ edge.evidence }}</small></li></ul><p v-if="!selectedEdges.length">-</p></section><section class="detail-section"><h3>{{ c.source }}</h3><p>{{ selected.source }}</p></section><button v-if="selected.kind === 'Gateway' && selected.conditions.includes('EnvoyConfig=available')" class="primary-button full-button" type="button" @click="emit('openEnvoy', selected.id)">Envoy</button></template><template v-else><div class="empty-detail"><Network :size="26" aria-hidden="true" /><h2>{{ c.select }}</h2><p>{{ c.selectHint }}</p></div></template></aside>
    </div>
  </section>
</template>
