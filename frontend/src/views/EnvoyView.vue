<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ArrowRight, Braces, Check, ChevronDown, ChevronUp, Copy, Filter, ListTree, Search, Server, Waypoints } from '@lucide/vue'
import { api } from '../api/client'
import StatusBadge from '../components/StatusBadge.vue'
import type { EnvoyCluster, EnvoyConfig, EnvoyListener, Topology } from '../types'

const props = defineProps<{ topology: Topology; initialGatewayId?: string }>()
const emit = defineEmits<{ error: [message: string] }>()

const gatewayID = ref('')
const config = ref<EnvoyConfig | null>(null)
const loading = ref(false)
const search = ref('')
const selectedListenerID = ref('')
const viewMode = ref<'parsed' | 'raw'>('parsed')
const rawSearch = ref('')
const rawMatchIndex = ref(-1)
const rawEditor = ref<HTMLTextAreaElement | null>(null)
const copied = ref(false)
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const gateways = computed(() => props.topology.nodes.filter((node) =>
  node.kind === 'Gateway' && node.conditions.includes('EnvoyConfig=available'),
))
const listeners = computed(() => {
  const query = search.value.trim().toLowerCase()
  return (config.value?.listeners ?? []).filter((listener) => !query || JSON.stringify(listener).toLowerCase().includes(query))
})
const selectedListener = computed(() => listeners.value.find((listener) => listener.id === selectedListenerID.value) ?? listeners.value[0] ?? null)
const clusterMap = computed(() => new Map((config.value?.clusters ?? []).map((cluster) => [cluster.name, cluster])))
const rawConfigText = computed(() => {
  if (config.value?.rawConfig == null) return ''
  try {
    return JSON.stringify(config.value.rawConfig, null, 2)
  } catch {
    return String(config.value.rawConfig)
  }
})
const rawSize = computed(() => {
  const bytes = new Blob([rawConfigText.value]).size
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
})
const rawMatches = computed(() => {
  const needle = rawSearch.value.trim().toLowerCase()
  const indexes: number[] = []
  if (!needle) return { indexes, truncated: false }
  const source = rawConfigText.value.toLowerCase()
  let cursor = source.indexOf(needle)
  while (cursor >= 0 && indexes.length < 10000) {
    indexes.push(cursor)
    cursor = source.indexOf(needle, cursor + Math.max(needle.length, 1))
  }
  return { indexes, truncated: cursor >= 0 }
})
const rawMatchLabel = computed(() => {
  if (!rawSearch.value.trim()) return rawConfigText.value ? rawConfigText.value.split('\n').length + ' 行 · ' + rawSize.value : '没有原始配置'
  if (!rawMatches.value.indexes.length) return '无匹配'
  const total = rawMatches.value.indexes.length + (rawMatches.value.truncated ? '+' : '')
  return (rawMatchIndex.value >= 0 ? rawMatchIndex.value + 1 + ' / ' : '') + total + ' 处'
})

watch([gateways, () => props.initialGatewayId], ([items, requested]) => {
  if (requested && items.some((item) => item.id === requested)) {
    gatewayID.value = requested
  } else if (!items.some((item) => item.id === gatewayID.value)) {
    gatewayID.value = items[0]?.id ?? ''
  }
}, { immediate: true })
watch(gatewayID, loadConfig, { immediate: true })
watch(listeners, (items) => {
  if (!items.some((listener) => listener.id === selectedListenerID.value)) selectedListenerID.value = items[0]?.id ?? ''
})
watch(rawSearch, () => {
  rawMatchIndex.value = -1
})

async function loadConfig() {
  if (!gatewayID.value) {
    config.value = null
    return
  }
  loading.value = true
  try {
    config.value = await api.envoy(gatewayID.value)
    selectedListenerID.value = config.value.listeners[0]?.id ?? ''
    rawSearch.value = ''
    viewMode.value = 'parsed'
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
async function moveRawMatch(direction: 1 | -1) {
  const matches = rawMatches.value.indexes
  if (!matches.length) return
  rawMatchIndex.value = (rawMatchIndex.value + direction + matches.length) % matches.length
  await nextTick()
  const editor = rawEditor.value
  const start = matches[rawMatchIndex.value]
  if (!editor || start == null) return
  editor.focus()
  editor.setSelectionRange(start, start + rawSearch.value.trim().length)
}
async function copyRawConfig() {
  if (!rawConfigText.value) return
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(rawConfigText.value)
    } else {
      const editor = rawEditor.value
      if (!editor) throw new Error('clipboard unavailable')
      editor.focus()
      editor.select()
      if (!document.execCommand('copy')) throw new Error('copy failed')
    }
    copied.value = true
    if (copiedTimer) clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => { copied.value = false }, 1800)
  } catch {
    emit('error', '无法复制原始配置，请检查浏览器剪贴板权限。')
  }
}

onBeforeUnmount(() => {
  if (copiedTimer) clearTimeout(copiedTimer)
})
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

    <div class="filter-bar envoy-filter-bar">
      <label>网关
        <select v-model="gatewayID">
          <option v-if="!gateways.length" value="">没有可读取的 Envoy Gateway</option>
          <option v-for="gateway in gateways" :key="gateway.id" :value="gateway.id">{{ gateway.namespace }}/{{ gateway.name }}</option>
        </select>
      </label>
      <div class="envoy-view-tabs" role="tablist" aria-label="Envoy 配置视图">
        <button type="button" role="tab" :aria-selected="viewMode === 'parsed'" :class="{ active: viewMode === 'parsed' }" @click="viewMode = 'parsed'">
          <ListTree :size="14" aria-hidden="true" />解析视图
        </button>
        <button type="button" role="tab" :aria-selected="viewMode === 'raw'" :class="{ active: viewMode === 'raw' }" :disabled="!rawConfigText" @click="viewMode = 'raw'">
          <Braces :size="14" aria-hidden="true" />原始 JSON
        </button>
      </div>
      <template v-if="viewMode === 'parsed'">
        <label class="search-field envoy-search-field">
          <Search :size="15" aria-hidden="true" />
          <input v-model="search" placeholder="Listener、Route 或 Cluster" />
        </label>
        <span class="filter-count">{{ listeners.length }} 个 Listener</span>
      </template>
      <template v-else>
        <label class="search-field envoy-search-field">
          <Search :size="15" aria-hidden="true" />
          <input v-model="rawSearch" placeholder="搜索原始配置" @keydown.enter.prevent="moveRawMatch(1)" />
        </label>
        <span class="raw-match-count">{{ rawMatchLabel }}</span>
        <div class="raw-toolbar">
          <button class="icon-button" type="button" title="上一个匹配项" :disabled="!rawMatches.indexes.length" @click="moveRawMatch(-1)">
            <ChevronUp :size="16" aria-hidden="true" />
          </button>
          <button class="icon-button" type="button" title="下一个匹配项" :disabled="!rawMatches.indexes.length" @click="moveRawMatch(1)">
            <ChevronDown :size="16" aria-hidden="true" />
          </button>
          <button class="icon-button" type="button" :title="copied ? '已复制' : '复制原始配置'" :disabled="!rawConfigText" @click="copyRawConfig">
            <Check v-if="copied" :size="16" aria-hidden="true" />
            <Copy v-else :size="16" aria-hidden="true" />
          </button>
        </div>
      </template>
    </div>

    <div v-if="viewMode === 'parsed'" class="envoy-workspace">
      <section class="envoy-list-panel">
        <div v-if="loading" class="loading-block"><span class="spinner" />正在连接 Envoy Pod</div>
        <button
          v-for="listener in listeners"
          v-else
          :key="listener.id"
          class="envoy-list-item"
          :class="{ selected: selectedListener?.id === listener.id }"
          type="button"
          @click="selectedListenerID = listener.id"
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

    <section v-else class="envoy-raw-panel">
      <div class="envoy-raw-head">
        <div>
          <p class="eyebrow">Envoy Admin</p>
          <h2>原始 config_dump</h2>
        </div>
        <span>{{ config?.source }} · {{ config?.sampledPod || '未知 Pod' }} · {{ rawSize }}</span>
      </div>
      <textarea
        v-if="rawConfigText"
        ref="rawEditor"
        class="envoy-raw-code"
        :value="rawConfigText"
        readonly
        spellcheck="false"
        wrap="off"
        aria-label="Envoy 原始 config dump"
      />
      <div v-else class="empty-detail large">
        <Braces :size="28" aria-hidden="true" />
        <h2>没有原始配置</h2>
      </div>
    </section>
  </section>
</template>
