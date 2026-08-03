<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { AlertCircle, LoaderCircle } from '@lucide/vue'
import { api } from './api/client'
import AppSidebar from './components/AppSidebar.vue'
import ContextBar from './components/ContextBar.vue'
import EnvoyView from './views/EnvoyView.vue'
import HealthView from './views/HealthView.vue'
import ResourcesView from './views/ResourcesView.vue'
import SimulatorView from './views/SimulatorView.vue'
import FederatedTopologyView from './views/FederatedTopologyView.vue'
import TopologyView from './views/TopologyView.vue'
import type { Finding, GateLensContext, Resource, Topology, ViewID } from './types'

const validViews = new Set<ViewID>(['topology', 'envoy', 'simulator', 'health', 'resources'])
const currentView = ref<ViewID>(viewFromHash())
const context = ref<GateLensContext | null>(null)
const topology = ref<Topology | null>(null)
const findings = ref<Finding[]>([])
const resources = ref<Resource[]>([])
const namespace = ref('')
const loading = ref(true)
const sidebarOpen = ref(false)
const focusNodeId = ref('')
const envoyGatewayID = ref('')
const errorMessage = ref('')
let toastTimer: ReturnType<typeof setTimeout> | undefined

function viewFromHash(): ViewID {
  const candidate = window.location.hash.slice(1) as ViewID
  return validViews.has(candidate) ? candidate : 'topology'
}
function navigate(view: ViewID) {
  currentView.value = view
  sidebarOpen.value = false
  if (window.location.hash !== `#${view}`) window.location.hash = view
}
function locate(targetID: string) {
  focusNodeId.value = targetID
  navigate('topology')
}
function openEnvoy(gatewayID: string) {
  envoyGatewayID.value = gatewayID
  navigate('envoy')
}
function showError(message: string) {
  errorMessage.value = message
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { errorMessage.value = '' }, 6000)
}
async function loadAll() {
  loading.value = true
  try {
    const [nextContext, nextTopology, nextFindings, nextResources] = await Promise.all([
      api.context(), api.topology(), api.findings(), api.resources(),
    ])
    context.value = nextContext
    topology.value = nextTopology
    findings.value = nextFindings
    resources.value = nextResources
  } catch (error) {
    showError(`读取后端失败：${error instanceof Error ? error.message : '未知错误'}`)
  } finally {
    loading.value = false
  }
}
function onHashChange() {
  currentView.value = viewFromHash()
}

onMounted(() => {
  window.addEventListener('hashchange', onHashChange)
  if (!window.location.hash) window.history.replaceState(null, '', '#topology')
  void loadAll()
})
onBeforeUnmount(() => {
  window.removeEventListener('hashchange', onHashChange)
  if (toastTimer) clearTimeout(toastTimer)
})
</script>

<template>
  <div class="app-shell">
    <AppSidebar :current="currentView" :context="context" :finding-count="findings.length" :open="sidebarOpen" @navigate="navigate" @close="sidebarOpen = false" />
    <button v-if="sidebarOpen" class="sidebar-scrim" type="button" aria-label="关闭导航" @click="sidebarOpen = false" />
    <main>
      <ContextBar v-model:namespace="namespace" :context="context" :loading="loading" @menu="sidebarOpen = true" @refresh="loadAll" />
      <div v-if="loading && !context" class="initial-loading"><LoaderCircle :size="30" class="spin" /><strong>正在建立集群上下文</strong><span>读取 API 快照和拓扑数据</span></div>
      <template v-else-if="context && topology">
        <FederatedTopologyView v-if="currentView === 'topology'" :context="context" :topology="topology" :namespace="namespace" :focus-node-id="focusNodeId" @navigate="navigate" @open-envoy="openEnvoy" />
        <EnvoyView v-else-if="currentView === 'envoy'" :topology="topology" :initial-gateway-id="envoyGatewayID" @error="showError" />
        <SimulatorView v-else-if="currentView === 'simulator'" :context="context" :topology="topology" @locate="locate" @error="showError" />
        <HealthView v-else-if="currentView === 'health'" :context="context" :findings="findings" @navigate="navigate" @locate="locate" />
        <ResourcesView v-else :resources="resources" @locate="locate" />
      </template>
      <div v-else class="initial-loading error-state"><AlertCircle :size="30" /><strong>无法读取 GateLens API</strong><span>确认后端已启动，然后使用顶部刷新按钮重试。</span></div>
    </main>
  </div>
  <div v-if="errorMessage" class="toast" role="status"><AlertCircle :size="16" />{{ errorMessage }}</div>
</template>
