<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowRight, Filter, Search, Server, Waypoints } from '@lucide/vue'
import { api } from '../api/client'
import StatusBadge from '../components/StatusBadge.vue'
import type { EnvoyCluster, EnvoyConfig, EnvoyListener, Topology } from '../types'

const props = defineProps<{ topology: Topology; initialGatewayId?: string }>()
const emit = defineEmits<{ error: [message: string] }>()

const gatewayID = ref('')
const config = ref<EnvoyConfig | null>(null)
const loading = ref(false)
const search = ref('')
const selectedListenerName = ref('')

const gateways = computed(() => props.topology.nodes.filter((node) =>
  node.kind === 'Gateway' && node.conditions.includes('EnvoyConfig=available'),
))
const listeners = computed(() => {
  const query = search.value.trim().toLowerCase()
  return (config.value?.listeners ?? []).filter((listener) => !query || JSON.stringify(listener).toLowerCase().includes(query))
})
const selectedListener = computed(() => listeners.value.find((listener) => listener.name === selectedListenerName.value) ?? listeners.value[0] ?? null)
const clusterMap = computed(() => new Map((config.value?.clusters ?? []).map((cluster) => [cluster.name, cluster])))

watch([gateways, () => props.initialGatewayId], ([items, requested]) => {
  if (requested && items.some((item) => item.id === requested)) {
    gatewayID.value = requested
  } else if (!items.some((item) => item.id === gatewayID.value)) {
    gatewayID.value = items[0]?.id ?? ''
  }
}, { immediate: true })
watch(gatewayID, loadConfig, { immediate: true })
watch(listeners, (items) => {
  if (!items.some((listener) => listener.name === selectedListenerName.value)) selectedListenerName.value = items[0]?.name ?? ''
})

async function loadConfig() {
  if (!gatewayID.value) {
    config.value = null
    return
  }
  loading.value = true
  try {
    config.value = await api.envoy(gatewayID.value)
    selectedListenerName.value = config.value.listeners[0]?.name ?? ''
  } catch (error) {
    config.value = null
    emit('error', `Envoy 配置读取失败：${error instanceof Error ? error.message : '未知错误'}`)
  } finally {
    loading.value = false
  }
}
function routeClusters(cluster: string, weighted: { name: string }[] | undefined): EnvoyCluster[] {
  const names = weighted?.length ? weighted.map((item) => item.name) : cluster ? [cluster] : []
  return names.map((name) => clusterMap.value.get(name)).filter((item): item is EnvoyCluster => Boolean(item))
}
function listenerTotals(listener: EnvoyListener) {
  return {
    chains: listener.filterChains.length,
    filters: listener.filterChains.reduce((total, chain) => total + chain.httpFilters.length, 0),
    routes: listener.filterChains.reduce((total, chain) => total + chain.routes.length, 0),
  }
}
</script>

<template>
  <section class="view">
    <div class="page-header">
      <div>
        <p class="eyebrow">数据面配置</p>
        <h1>Envoy 配置</h1>
        <p v-if="config">{{ config.controller || 'Envoy' }} · 采样 {{ config.sampledPod || '未知 Pod' }} · {{ config.readyReplicas || 1 }} 个 Ready 副本</p>
        <p v-else>按 Listener 查看 Filter Chain、HTTP Filter、Route、Cluster 和 Endpoint。</p>
      </div>
      <span class="inference-badge">{{ loading ? '正在读取' : config?.proxy || '等待配置' }}</span>
    </div>

    <div class="filter-bar">
      <label>网关
        <select v-model="gatewayID">
          <option v-if="!gateways.length" value="">没有可读取的 Envoy Gateway</option>
          <option v-for="gateway in gateways" :key="gateway.id" :value="gateway.id">{{ gateway.namespace }}/{{ gateway.name }}</option>
        </select>
      </label>
      <label class="search-field">
        <Search :size="15" aria-hidden="true" />
        <input v-model="search" placeholder="Listener、Route 或 Cluster" />
      </label>
      <span class="filter-count">{{ listeners.length }} 个 Listener</span>
    </div>

    <div class="envoy-workspace">
      <section class="envoy-list-panel">
        <div v-if="loading" class="loading-block"><span class="spinner" />正在连接 Envoy Pod</div>
        <button
          v-for="listener in listeners"
          v-else
          :key="listener.name"
          class="envoy-list-item"
          :class="{ selected: selectedListener?.name === listener.name }"
          type="button"
          @click="selectedListenerName = listener.name"
        >
          <span class="envoy-list-icon"><Waypoints :size="15" aria-hidden="true" /></span>
          <span class="envoy-list-copy">
            <strong>{{ listener.name }}</strong>
            <small>{{ listener.address }}:{{ listener.port }} · {{ listener.protocol }}</small>
            <em>{{ listenerTotals(listener).chains }} chains · {{ listenerTotals(listener).routes }} routes · {{ listenerTotals(listener).filters }} filters</em>
          </span>
          <StatusBadge :status="listener.status" :label="listener.status === 'healthy' ? '正常' : '关注'" />
        </button>
        <div v-if="!loading && !listeners.length" class="loading-block">没有匹配的 Listener</div>
      </section>

      <section class="envoy-detail-panel">
        <template v-if="selectedListener">
          <div class="envoy-detail-head">
            <div>
              <p class="eyebrow">Listener</p>
              <h2>{{ selectedListener.name }}</h2>
              <p>{{ selectedListener.address }}:{{ selectedListener.port }} · {{ selectedListener.protocol }} · {{ selectedListener.filterChains.length }} 条 Filter Chain</p>
            </div>
            <StatusBadge :status="selectedListener.status" :label="selectedListener.status === 'healthy' ? '正常' : '关注'" />
          </div>

          <div class="envoy-chain-list">
            <article v-for="(chain, chainIndex) in selectedListener.filterChains" :key="chain.name" class="envoy-chain">
              <div class="envoy-chain-head">
                <span class="chain-index">{{ chainIndex + 1 }}</span>
                <div><strong>{{ chain.name || 'default' }}</strong><small>{{ chain.match || '无额外匹配条件' }} · {{ chain.transport || '默认传输' }}</small></div>
                <span>{{ chain.httpFilters.length }} filters · {{ chain.routes.length }} routes</span>
              </div>
              <div class="envoy-path">
                <span><Waypoints :size="16" /><b>Listener</b><small>{{ selectedListener.name }}</small></span>
                <ArrowRight :size="15" />
                <span><Filter :size="16" /><b>Filter Chain</b><small>{{ chain.name || 'default' }}</small></span>
                <ArrowRight :size="15" />
                <span><Server :size="16" /><b>Cluster</b><small>按路由选择</small></span>
              </div>
              <section class="envoy-subsection">
                <h3>HTTP Filters <small>按执行顺序</small></h3>
                <div class="envoy-filter-list">
                  <div v-for="(filter, index) in chain.httpFilters" :key="`${filter.name}-${index}`" class="envoy-filter">
                    <span class="filter-order">{{ index + 1 }}</span>
                    <div><strong>{{ filter.name }}</strong><small>{{ filter.type }} · {{ filter.stage }}{{ filter.terminal ? ' · terminal' : '' }}</small><p>{{ filter.configSummary }}</p></div>
                  </div>
                  <p v-if="!chain.httpFilters.length" class="muted-copy">未解析到 HTTP Filter。</p>
                </div>
              </section>
              <section class="envoy-subsection">
                <h3>Routes <small>Virtual Host 规则</small></h3>
                <div class="envoy-route-list">
                  <div v-for="route in chain.routes" :key="route.name" class="envoy-route">
                    <div><strong>{{ route.name }}</strong><small>{{ route.match }}</small></div>
                    <code>{{ route.weightedClusters?.map((item) => `${item.name} (${item.weight})`).join(' · ') || route.cluster || 'direct response' }}</code>
                    <div v-for="cluster in routeClusters(route.cluster, route.weightedClusters)" :key="cluster.name" class="cluster-inline">
                      <span class="cluster-icon"><Server :size="14" aria-hidden="true" /></span>
                      <div><strong>{{ cluster.name }}</strong><small>{{ cluster.type }} · {{ cluster.discovery }} · {{ cluster.endpoints.length }} endpoints</small></div>
                      <span>{{ cluster.endpoints.filter((item) => item.status === 'healthy').length }}/{{ cluster.endpoints.length }} healthy</span>
                      <div class="cluster-endpoints">
                        <span v-for="endpoint in cluster.endpoints" :key="`${endpoint.address}:${endpoint.port}`" class="cluster-endpoint" :class="`text-${endpoint.status}`">
                          {{ endpoint.address }}:{{ endpoint.port }} · {{ endpoint.health }}
                        </span>
                      </div>
                    </div>
                  </div>
                  <p v-if="!chain.routes.length" class="muted-copy">该 Filter Chain 没有 Route。</p>
                </div>
              </section>
            </article>
          </div>
        </template>
        <div v-else class="empty-detail large">
          <Waypoints :size="28" aria-hidden="true" />
          <h2>选择一个 Listener</h2>
          <p>查看它的 Filter Chain 和请求处理路径。</p>
        </div>
      </section>
    </div>
  </section>
</template>
