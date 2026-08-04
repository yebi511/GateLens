<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Check, CircleHelp, LoaderCircle, Route, XCircle } from '@lucide/vue'
import { api } from '../api/client'
import type { GateLensContext, RouteExplanation, Topology } from '../types'

const props = defineProps<{ context: GateLensContext; topology: Topology; clusterId: string }>()
const emit = defineEmits<{ locate: [targetID: string]; error: [message: string] }>()

const cluster = computed(() => props.topology.clusters?.find((item) => item.id === props.clusterId))
const gateways = computed(() => props.topology.nodes.filter((node) => node.clusterID === props.clusterId && node.kind === 'Gateway'))
const listeners = computed(() => props.topology.nodes.filter((node) => node.clusterID === props.clusterId && node.kind === 'Listener'))
const namespaces = computed(() => (cluster.value?.namespaces ?? props.context.namespaces).filter((item) => item !== 'all'))
const form = reactive({
  gateway: gateways.value[0]?.id ?? '',
  listener: '',
  method: 'POST',
  host: '',
  path: '/v1/chat/completions',
  namespace: '',
  model: '',
})
const result = ref<RouteExplanation | null>(null)
const loading = ref(false)

watch(gateways, (items) => {
  if (!items.some((gateway) => gateway.id === form.gateway)) form.gateway = items[0]?.id ?? ''
  form.listener = ''
  form.namespace = ''
  result.value = null
}, { immediate: true })

async function submit() {
  loading.value = true
  result.value = null
  try {
    result.value = await api.explain({
      snapshotID: cluster.value?.snapshot.id ?? props.context.snapshot.id,
      gateway: form.gateway,
      listener: form.listener,
      method: form.method,
      host: form.host.trim(),
      path: form.path.trim(),
      namespace: form.namespace,
      model: form.model.trim(),
    })
  } catch (error) {
    emit('error', `模拟失败：${error instanceof Error ? error.message : '未知错误'}`)
  } finally {
    loading.value = false
  }
}
function reset() {
  form.listener = ''
  form.method = 'POST'
  form.host = ''
  form.path = '/v1/chat/completions'
  form.namespace = ''
  form.model = ''
  result.value = null
}
function stepIcon(state: string) {
  if (state === 'passed') return Check
  if (state === 'rejected') return XCircle
  return CircleHelp
}
</script>

<template>
  <section class="view">
    <div class="page-header">
      <div>
        <p class="eyebrow">按快照推断</p>
        <h1>请求模拟</h1>
        <p>后端根据 HTTPRoute 匹配条件与当前 Endpoint 状态解释请求。</p>
      </div>
      <span class="inference-badge">不是实际流量</span>
    </div>

    <div class="simulator-layout">
      <form class="request-form" @submit.prevent="submit" @reset.prevent="reset">
        <div class="panel-title"><h2>请求输入</h2><span>快照 {{ cluster?.snapshot.observedAt ?? context.snapshot.observedAt }}</span></div>
        <label>Gateway
          <select v-model="form.gateway" required>
            <option v-for="gateway in gateways" :key="gateway.id" :value="gateway.id">{{ gateway.namespace }}/{{ gateway.name }}</option>
          </select>
        </label>
        <label>Listener
          <select v-model="form.listener"><option value="">自动匹配</option><option v-for="listener in listeners" :key="listener.id" :value="listener.id">{{ listener.namespace }}/{{ listener.name }}</option></select>
        </label>
        <div class="form-row">
          <label>Method<select v-model="form.method"><option>POST</option><option>GET</option><option>PUT</option><option>DELETE</option></select></label>
          <label>Host<input v-model="form.host" required placeholder="api.example.com" /></label>
        </div>
        <label>Path<input v-model="form.path" required /></label>
        <label>路由命名空间
          <select v-model="form.namespace"><option value="">全部</option><option v-for="namespace in namespaces" :key="namespace" :value="namespace">{{ namespace }}</option></select>
        </label>
        <label>模型标识 <span class="optional">可选</span><input v-model="form.model" placeholder="例如 qwen2.5-72b" /></label>
        <div class="form-actions">
          <button type="reset" class="quiet-button">重置</button>
          <button type="submit" class="primary-button" :disabled="loading || !form.gateway">
            <LoaderCircle v-if="loading" :size="15" class="spin" aria-hidden="true" />
            {{ loading ? '正在解析' : '运行模拟' }}
          </button>
        </div>
      </form>

      <section class="explanation-panel">
        <div v-if="loading" class="empty-detail large"><LoaderCircle :size="28" class="spin" /><h2>正在解析请求</h2><p>结果将固定绑定当前快照。</p></div>
        <template v-else-if="result">
          <div class="result-header">
            <div><h2>{{ result.summary }}</h2><p>{{ result.observedAt }} · 置信度 {{ result.confidence }} · 按快照推断</p></div>
            <span class="outcome" :class="result.outcome.toLowerCase()">{{ result.outcome }}</span>
          </div>
          <div class="step-list">
            <article v-for="(step, index) in result.steps" :key="`${step.targetID}-${index}`" class="explain-step">
              <span class="step-number"><component :is="stepIcon(step.state)" :size="14" aria-hidden="true" /></span>
              <div class="step-body"><strong>{{ step.title }}</strong><p>{{ step.detail }}</p><button class="source-link" type="button" @click="emit('locate', step.targetID)">在拓扑中查看</button></div>
              <span class="step-status" :class="step.state">{{ step.state }}</span>
            </article>
          </div>
        </template>
        <div v-else class="empty-detail large"><Route :size="28" aria-hidden="true" /><h2>等待请求</h2><p>填写 Host 与 Path 后运行模拟。</p></div>
      </section>
    </div>
  </section>
</template>
