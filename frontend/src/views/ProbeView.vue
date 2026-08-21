<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { AlertTriangle, CheckCircle2, Clock3, LoaderCircle, RadioTower, Server, Trash2, Waypoints } from '@lucide/vue'
import { api } from '../api/client'
import type { ProbeExecution, ProbeSegment, Topology } from '../types'
import { formatCSTDateTime } from '../utils/dateTime'

const props = defineProps<{ topology: Topology; clusterId: string }>()
const emit = defineEmits<{ error: [message: string] }>()
const entries = computed(() => (props.topology.probeEntries ?? []).filter((entry) => entry.clusterID === props.clusterId))
const selectedEntry = computed(() => entries.value.find((entry) => entry.id === form.entryID))
const defaultForm = () => ({ entryID: '', method: 'POST', path: '/v1/chat/completions', host: '', apiKey: '', contentType: 'application/json', body: '', timeoutSeconds: 15 })
const form = reactive(defaultForm())
const rememberAPIKey = ref(false)
const apiKeyInput = ref<HTMLInputElement | null>(null)
const result = ref<ProbeExecution | null>(null)
const loading = ref(false)
let restoringDraft = false
const segments = computed<ProbeSegment[]>(() => {
  if (!result.value) return []
  if (result.value.segments.length) return result.value.segments
  return [{
    index: 1,
    clusterID: result.value.sourceCluster,
    gatewayID: result.value.gatewayID,
    state: result.value.hops.length ? 'observed' : 'missing',
    evidence: result.value.hops.length ? 'observed' : 'inferred',
    logSource: result.value.logSource,
    hops: result.value.hops,
    gaps: result.value.gaps,
  }]
})

function evidenceLabel(segment: ProbeSegment) {
  if (segment.evidence === 'observed') return 'Observed'
	if (segment.inferenceConfidence === 'high') return '高置信候选'
	if (segment.inferenceConfidence === 'medium') return '中置信候选'
	if (segment.inferenceConfidence === 'low') return '配置候选'
	if (segment.inferenceConfidence === 'ambiguous') return '歧义候选'
  if (segment.state === 'missing' || segment.state === 'unavailable') return 'Missing'
  return 'Inferred'
}

function segmentTitle(segment: ProbeSegment) {
	if (segment.evidence === 'observed') return `第 ${segment.index} 跳`
	if (segment.inferenceBasis) return '候选下一跳'
	return '网关证据缺口'
}

function draftStorageKey(clusterID: string) {
  return `gatelens.probe-draft.v1:${encodeURIComponent(clusterID)}`
}

function persistDraft(clusterID = props.clusterId) {
  if (restoringDraft || !clusterID) return
  try {
    window.sessionStorage.setItem(draftStorageKey(clusterID), JSON.stringify({
      entryID: form.entryID,
      method: form.method,
      path: form.path,
      host: form.host,
      apiKey: rememberAPIKey.value ? form.apiKey : '',
      rememberAPIKey: rememberAPIKey.value,
      contentType: form.contentType,
      body: form.body,
      timeoutSeconds: form.timeoutSeconds,
    }))
  } catch {
    // Storage may be disabled or full; the in-memory form remains usable.
  }
}

function restoreDraft(clusterID: string) {
  restoringDraft = true
  Object.assign(form, defaultForm())
  rememberAPIKey.value = false
  try {
    const raw = window.sessionStorage.getItem(draftStorageKey(clusterID))
    if (!raw) return
    const draft = JSON.parse(raw) as Record<string, unknown>
    const stringValue = (name: string, fallback: string, max: number) => typeof draft[name] === 'string' ? (draft[name] as string).slice(0, max) : fallback
    form.entryID = stringValue('entryID', '', 2048)
    form.method = ['POST', 'GET', 'PUT', 'PATCH', 'DELETE', 'HEAD'].includes(String(draft.method)) ? String(draft.method) : 'POST'
    form.path = stringValue('path', '/v1/chat/completions', 8192)
    form.host = stringValue('host', '', 2048)
    form.contentType = stringValue('contentType', 'application/json', 1024)
    form.body = stringValue('body', '', 65536)
    const timeout = Number(draft.timeoutSeconds)
    form.timeoutSeconds = Number.isFinite(timeout) && timeout >= 1 && timeout <= 30 ? timeout : 15
    rememberAPIKey.value = draft.rememberAPIKey === true
    form.apiKey = rememberAPIKey.value ? stringValue('apiKey', '', 8192) : ''
  } catch {
    try { window.sessionStorage.removeItem(draftStorageKey(clusterID)) } catch { /* Ignore unavailable storage. */ }
  } finally {
    restoringDraft = false
  }
}

function clearDraft() {
  restoringDraft = true
  Object.assign(form, defaultForm())
  form.entryID = entries.value[0]?.id ?? ''
  rememberAPIKey.value = false
  result.value = null
  try { window.sessionStorage.removeItem(draftStorageKey(props.clusterId)) } catch { /* Ignore unavailable storage. */ }
  restoringDraft = false
}

watch(() => props.clusterId, (clusterID, previousClusterID) => {
  if (previousClusterID) persistDraft(previousClusterID)
  restoreDraft(clusterID)
  if (!entries.value.some((entry) => entry.id === form.entryID)) form.entryID = entries.value[0]?.id ?? ''
  result.value = null
}, { immediate: true })
watch([form, rememberAPIKey], () => persistDraft(), { deep: true })
watch(entries, (items) => {
  if (!items.some((entry) => entry.id === form.entryID)) form.entryID = items[0]?.id ?? ''
  result.value = null
}, { immediate: true })

async function submit() {
  loading.value = true
  result.value = null
  // Password managers and browser autofill can update the input without an
  // input event. Read its live value at submit time so the first probe carries
  // the credential even when Vue's model has not observed that update yet.
  const apiKey = apiKeyInput.value?.value ?? form.apiKey
  form.apiKey = apiKey
  if (rememberAPIKey.value) persistDraft()
  try {
    result.value = await api.createProbe({
      sourceCluster: props.clusterId,
      gatewayID: selectedEntry.value?.gatewayID ?? '',
      entryID: form.entryID,
      method: form.method,
      path: form.path.trim(),
      host: form.host.trim() || undefined,
      apiKey: apiKey || undefined,
      contentType: form.body ? form.contentType.trim() || undefined : undefined,
      body: form.body || undefined,
      timeoutSeconds: form.timeoutSeconds,
    })
  } catch (error) {
    emit('error', `探测失败：${error instanceof Error ? error.message : '未知错误'}`)
  } finally {
    if (!rememberAPIKey.value) form.apiKey = ''
    loading.value = false
  }
}
</script>

<template>
  <section class="view">
    <div class="page-header">
      <div><p class="eyebrow">跨网关访问日志证据</p><h1>实时探测</h1><p>从已发现的网关 Service 发送一次真实请求，并关联各集群 Gateway 的实际路由结果。</p></div>
      <span class="probe-risk"><AlertTriangle :size="14" aria-hidden="true" />产生真实流量</span>
    </div>
    <div class="probe-workspace">
    <div class="probe-layout">
      <form class="request-form" @submit.prevent="submit">
        <div class="panel-title"><h2>探测请求</h2><span>{{ props.clusterId }}</span></div>
        <label>网关入口 Service<select v-model="form.entryID" required><option v-for="entry in entries" :key="entry.id" :value="entry.id">{{ entry.displayName }}</option></select></label>
        <p v-if="selectedEntry" class="entry-target">{{ selectedEntry.scheme }}://{{ selectedEntry.dnsName }}:{{ selectedEntry.port }}</p>
        <p v-else class="entry-empty">当前集群没有发现协议明确、且选中 Ready Gateway Pod 的 Service 入口。</p>
        <div class="form-row"><label>Method<select v-model="form.method"><option>POST</option><option>GET</option><option>PUT</option><option>PATCH</option><option>DELETE</option><option>HEAD</option></select></label><label>超时（秒）<input v-model.number="form.timeoutSeconds" type="number" min="1" max="30" /></label></div>
        <label>Path<input v-model="form.path" required placeholder="/v1/chat/completions" /></label>
        <label>Host <span class="optional">可选</span><input v-model="form.host" placeholder="api.example.com" /></label>
        <div class="probe-secret-field">
          <label for="probe-api-key">API Key <span class="optional">可选</span></label>
          <input id="probe-api-key" ref="apiKeyInput" v-model="form.apiKey" type="password" autocomplete="off" maxlength="8192" placeholder="sk-..." />
          <label class="probe-session-toggle"><input v-model="rememberAPIKey" type="checkbox" />本次标签页保留</label>
        </div>
        <label>Content-Type<input v-model="form.contentType" /></label>
        <label>请求 Body <span class="optional">最多 64 KiB，本标签页保留</span><textarea v-model="form.body" rows="7" spellcheck="false" /></label>
        <div class="form-actions"><button class="secondary-button" type="button" :disabled="loading" @click="clearDraft"><Trash2 :size="15" aria-hidden="true" />清空草稿</button><button class="primary-button" type="submit" :disabled="loading || !selectedEntry"><LoaderCircle v-if="loading" :size="15" class="spin" aria-hidden="true" />{{ loading ? '正在发送并等待日志' : '发送探测' }}</button></div>
      </form>

      <section class="probe-results">
        <div v-if="loading" class="empty-detail large"><LoaderCircle :size="28" class="spin" /><h2>等待网关完成请求</h2><p>源 Agent 正在发送一次请求，各集群 Agent 同步收集匹配的访问日志。</p></div>
        <template v-else-if="result">
          <header class="probe-summary">
            <div><p class="eyebrow">{{ result.evidenceComplete ? 'Observed' : 'Partial' }}</p><h2>{{ result.state === 'completed' ? `HTTP ${result.responseCode || '-'}` : '探测失败' }}</h2><p>{{ formatCSTDateTime(result.completedAt || result.startedAt) }} · {{ result.durationMillis }} ms · {{ segments.length }} 个网关段</p></div>
            <CheckCircle2 v-if="result.evidenceComplete" :size="24" aria-hidden="true" /><AlertTriangle v-else :size="24" aria-hidden="true" />
          </header>
          <div class="probe-identifiers"><code>probe {{ result.id }}</code><code>trace {{ result.traceID }}</code><span v-if="result.snapshotConsistency" class="snapshot-consistency" :class="`is-${result.snapshotConsistency}`">{{ result.snapshotConsistency }}</span></div>
          <div class="observed-path">
            <article class="observed-hop source-hop"><span><RadioTower :size="17" /></span><div><strong>{{ result.sourceCluster }}</strong><p>{{ result.method }} {{ result.target }}</p></div><em>Probe</em></article>
            <section v-for="segment in segments" :key="segment.gatewayID" class="probe-segment" :class="`segment-${segment.state}`">
              <header class="segment-header">
                <span><Waypoints :size="17" /></span>
                <div><strong>{{ segmentTitle(segment) }} · {{ segment.gatewayName || segment.gatewayID }}</strong><p>{{ segment.clusterID }}<template v-if="segment.snapshotID"> · {{ segment.snapshotID }}</template><template v-if="segment.observedAt"> · {{ formatCSTDateTime(segment.observedAt) }}</template></p></div>
				<em :class="`evidence-${segment.evidence === 'observed' ? 'observed' : segment.inferenceBasis ? 'inferred' : 'missing'}`">{{ evidenceLabel(segment) }}</em>
              </header>
              <div v-if="segment.transport || segment.destination" class="transit-boundary"><span>{{ segment.transport || '跨集群' }}</span><code>{{ segment.destination || '远端入口' }}</code></div>
			  <div v-if="segment.inferenceBasis" class="segment-inference"><strong>推断依据</strong><span>{{ segment.inferenceBasis }}</span></div>
              <article v-for="(hop, index) in segment.hops" :key="`${hop.pod}-${hop.observedAt}-${index}`" class="segment-event">
                <div><strong>{{ hop.routeName || '未命名 Route' }}</strong><p>{{ hop.pod || segment.gatewayName }} · {{ hop.authority }}{{ hop.path }}</p><dl><div><dt>Cluster</dt><dd>{{ hop.upstreamCluster || '-' }}</dd></div><div><dt>Upstream</dt><dd>{{ hop.upstreamHost || '-' }}</dd></div><div><dt>结果</dt><dd>HTTP {{ hop.responseCode || '-' }} · {{ hop.durationMillis }} ms</dd></div><div v-if="hop.aiLog"><dt>AI log</dt><dd>{{ hop.aiLog }}</dd></div></dl></div>
              </article>
			  <div v-if="!segment.hops.length" class="segment-empty"><AlertTriangle :size="15" /><span>{{ segment.gaps[0] || '该网关是下一跳候选，但没有本次请求的运行时证据。' }}</span></div>
              <p v-if="segment.logSource" class="segment-source">{{ segment.logSource }}</p>
            </section>
            <article v-if="result.hops.length" class="observed-hop endpoint-hop"><span><Server :size="17" /></span><div><strong>{{ result.hops[result.hops.length - 1].upstreamHost || '上游未记录' }}</strong><p>{{ result.hops[result.hops.length - 1].upstreamCluster }}</p></div><em>Envoy</em></article>
          </div>
          <div v-if="result.error || result.gaps.length" class="probe-gaps"><strong><Clock3 :size="15" />证据缺口</strong><p v-if="result.error">{{ result.error }}</p><p v-for="gap in result.gaps" :key="gap">{{ gap }}</p></div>
        </template>
        <div v-else class="empty-detail large"><RadioTower :size="28" aria-hidden="true" /><h2>等待实时请求</h2><p>结果按联邦拓扑发现跨网关链路，并用各集群访问日志分别验证。</p></div>
      </section>
    </div>
	</div>
  </section>
</template>
