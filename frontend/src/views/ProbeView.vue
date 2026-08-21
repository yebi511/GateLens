<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { AlertTriangle, CheckCircle2, Clock3, LoaderCircle, RadioTower, Server, Trash2, Waypoints } from '@lucide/vue'
import { api } from '../api/client'
import type { ExtProcObservation, ObservedHop, ProbeExecution, ProbeSegment, Topology, TopologyNode } from '../types'
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

function extProcLabel(observation: ExtProcObservation) {
  switch (observation.outcome) {
    case 'success': return '调用成功'
    case 'error': return '处理器错误'
    case 'timeout': return '处理超时'
    case 'fail-open': return '失败放行'
    case 'immediate-response': return '处理器直接响应'
    default: return observation.invoked ? '已调用' : '证据不足'
  }
}

function extProcCalls(observation: ExtProcObservation) {
  return (observation.requestHeaderCalls ?? 0) + (observation.requestBodyCalls ?? 0) +
    (observation.responseHeaderCalls ?? 0) + (observation.responseBodyCalls ?? 0)
}

function extProcLatency(observation: ExtProcObservation) {
  return ((observation.requestHeaderLatencyUs ?? 0) + (observation.requestBodyLatencyUs ?? 0) +
    (observation.responseHeaderLatencyUs ?? 0) + (observation.responseBodyLatencyUs ?? 0)) / 1000
}

function nodeHealthForProcessor(observation: ExtProcObservation, clusterID: string): TopologyNode | undefined {
  const identities = [observation.processor, observation.selectedPool]
    .filter((value): value is string => Boolean(value))
    .map((value) => value.toLowerCase())
  if (!identities.length) return undefined
  return props.topology.nodes.find((node) =>
    node.clusterID === clusterID &&
    ['BBR', 'BodyBasedRouting', 'EndpointPicker', 'EPP', 'InferencePool', 'Service'].includes(node.kind) &&
    identities.includes(node.name.toLowerCase()),
  )
}

interface ResolvedUpstream {
  endpoint?: TopologyNode
  service?: TopologyNode
  registry?: TopologyNode
  mcpBridge?: TopologyNode
  externalTarget?: TopologyNode
  resolutionAmbiguous?: boolean
  podName?: string
  podNamespace?: string
}

function conditionValue(node: TopologyNode | undefined, key: string) {
  const prefix = `${key}=`
  return node?.conditions.find((condition) => condition.startsWith(prefix))?.slice(prefix.length)
}

function upstreamAddress(upstreamHost?: string) {
  const value = (upstreamHost ?? '').trim().toLowerCase()
  const bracketed = /^\[([^\]]+)\](?::\d+)?$/.exec(value)
  if (bracketed) return bracketed[1]
  return value.split(':').length === 2 ? value.replace(/:\d+$/, '') : value
}

function hostFromCluster(upstreamCluster?: string) {
  const value = (upstreamCluster ?? '').trim().toLowerCase()
  const parts = value.split('|')
  return (parts.length >= 4 ? parts[3] : value).replace(/\.$/, '')
}

function serviceIdentityFromCluster(upstreamCluster?: string) {
  const host = hostFromCluster(upstreamCluster)
  const labels = host.split('.')
  if (labels.length >= 3 && labels[2] === 'svc') return { name: labels[0], namespace: labels[1] }
  return undefined
}

function registryMatchesCluster(node: TopologyNode, clusterHost: string) {
  if (node.kind !== 'Registry' || !clusterHost) return false
  const domain = conditionValue(node, 'Domain')?.trim().toLowerCase().replace(/\.$/, '')
  const type = conditionValue(node, 'Type')?.trim().toLowerCase()
  const identity = type ? `${node.name.trim().toLowerCase()}.${type}` : ''
  return clusterHost === domain || clusterHost === identity
}

function resolveUpstream(hop: ObservedHop): ResolvedUpstream {
  const address = upstreamAddress(hop.upstreamHost)
  const nodes = props.topology.nodes.filter((node) => node.clusterID === hop.clusterID)
  const endpoint = nodes.find((node) =>
    node.kind === 'Endpoint' && address && conditionValue(node, 'Address')?.toLowerCase() === address,
  )
  const clusterHost = hostFromCluster(hop.upstreamCluster)
  const registries = nodes.filter((node) => registryMatchesCluster(node, clusterHost))
  const registry = registries.length === 1 ? registries[0] : undefined
  const registryTargets = registry
    ? props.topology.edges
      .filter((edge) => edge.from === registry.id && edge.relation === 'resolves')
      .map((edge) => nodes.find((node) => node.id === edge.to))
      .filter((node): node is TopologyNode => Boolean(node))
    : []
  const registryTarget = registryTargets.length === 1 ? registryTargets[0] : undefined
  const registryService = registryTarget?.kind === 'Service' ? registryTarget : undefined
  const externalTarget = registryTarget?.kind === 'ExternalTarget' ? registryTarget : undefined
  const mcpBridge = registry
    ? props.topology.edges
      .filter((edge) => edge.to === registry.id && edge.relation === 'discovers')
      .map((edge) => nodes.find((node) => node.id === edge.from && node.kind === 'McpBridge'))
      .find((node): node is TopologyNode => Boolean(node))
    : undefined
  const endpointService = conditionValue(endpoint, 'Service')?.split('/')
  const clusterService = serviceIdentityFromCluster(hop.upstreamCluster)
  const serviceNamespace = endpointService?.length === 2
    ? endpointService[0]
    : registryService?.namespace ?? clusterService?.namespace
  const serviceName = endpointService?.length === 2
    ? endpointService[1]
    : registryService?.name ?? clusterService?.name
  const service = registryService ?? nodes.find((node) =>
    node.kind === 'Service' && node.namespace.toLowerCase() === serviceNamespace && node.name.toLowerCase() === serviceName,
  )
  const targetRef = conditionValue(endpoint, 'TargetRef')?.split('/')
  const isPod = targetRef?.length === 3 && targetRef[0].toLowerCase() === 'pod'
  return {
    endpoint,
    service,
    registry,
    mcpBridge,
    externalTarget,
    resolutionAmbiguous: registries.length > 1 || registryTargets.length > 1,
    podNamespace: isPod ? targetRef[1] : undefined,
    podName: isPod ? targetRef[2] : undefined,
  }
}

function registryLabel(resolved: ResolvedUpstream) {
  if (!resolved.registry) return ''
  const bridge = resolved.mcpBridge ? `${resolved.mcpBridge.namespace}/${resolved.mcpBridge.name}` : 'McpBridge'
  return `${bridge} / ${resolved.registry.name}`
}

function externalTargetLabel(target: TopologyNode) {
  const address = conditionValue(target, 'Address') || target.name
  const port = conditionValue(target, 'Port')
  return port ? `${address}:${port}` : address
}

function finalUpstreamTitle(hop: ObservedHop, resolved: ResolvedUpstream) {
  if (resolved.podName) return `${resolved.podNamespace}/${resolved.podName}`
  return hop.upstreamHost || '上游未记录'
}

function finalUpstreamKind(resolved: ResolvedUpstream) {
  if (resolved.podName) return 'Pod'
  if (resolved.endpoint) return 'Endpoint'
  if (resolved.resolutionAmbiguous) return 'Ambiguous'
  return 'Envoy'
}

function registryResolutionLabel(resolved: ResolvedUpstream) {
  const registry = registryLabel(resolved)
  if (resolved.service) return `${registry} -> Service ${resolved.service.namespace}/${resolved.service.name}`
  if (resolved.externalTarget) return `${registry} -> ${externalTargetLabel(resolved.externalTarget)}`
  return registry
}

function gatewaySnapshotNode(segment: ProbeSegment) {
  const exact = props.topology.nodes.find((node) => node.kind === 'Gateway' && node.id === segment.gatewayID)
  if (exact) return exact
  const matches = props.topology.nodes.filter((node) =>
    node.kind === 'Gateway' && node.clusterID === segment.clusterID && node.name === segment.gatewayName,
  )
  return matches.length === 1 ? matches[0] : undefined
}

const finalHop = computed(() => result.value?.hops[result.value.hops.length - 1])
const finalUpstream = computed(() => finalHop.value ? resolveUpstream(finalHop.value) : {})

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
                <div><strong>{{ segmentTitle(segment) }} · {{ segment.gatewayName || segment.gatewayID }}</strong><p>{{ segment.clusterID }}<template v-if="segment.snapshotID"> · {{ segment.snapshotID }}</template><template v-if="segment.observedAt"> · {{ formatCSTDateTime(segment.observedAt) }}</template></p><small v-if="gatewaySnapshotNode(segment)" class="segment-gateway-snapshot" :class="`text-${gatewaySnapshotNode(segment)?.status}`">网关快照 · {{ gatewaySnapshotNode(segment)?.namespace }}/{{ gatewaySnapshotNode(segment)?.name }} · {{ gatewaySnapshotNode(segment)?.statusText }}</small></div>
				<em :class="`evidence-${segment.evidence === 'observed' ? 'observed' : segment.inferenceBasis ? 'inferred' : 'missing'}`">{{ evidenceLabel(segment) }}</em>
              </header>
              <div v-if="segment.transport || segment.destination" class="transit-boundary"><span>{{ segment.transport || '跨集群' }}</span><code>{{ segment.destination || '远端入口' }}</code></div>
			  <div v-if="segment.inferenceBasis" class="segment-inference"><strong>推断依据</strong><span>{{ segment.inferenceBasis }}</span></div>
              <article v-for="(hop, index) in segment.hops" :key="`${hop.pod}-${hop.observedAt}-${index}`" class="segment-event">
                <div><strong>{{ hop.routeName || '未命名 Route' }}</strong><p>{{ hop.pod || segment.gatewayName }} · {{ hop.authority }}{{ hop.path }}</p><dl><div><dt>上游 Cluster</dt><dd>{{ hop.upstreamCluster || '-' }}</dd></div><div><dt>实际上游地址</dt><dd>{{ hop.upstreamHost || '-' }}</dd></div><div v-if="resolveUpstream(hop).registry"><dt>McpBridge Registry</dt><dd>{{ registryLabel(resolveUpstream(hop)) }}</dd></div><div v-if="resolveUpstream(hop).resolutionAmbiguous"><dt>Registry 解析</dt><dd class="text-warning">同域名对应多个 Registry 或解析目标，无法唯一确认</dd></div><div v-else-if="resolveUpstream(hop).externalTarget"><dt>Registry 解析目标</dt><dd>{{ externalTargetLabel(resolveUpstream(hop).externalTarget!) }}</dd></div><div v-if="resolveUpstream(hop).service"><dt>上游 Service</dt><dd>{{ resolveUpstream(hop).service?.namespace }}/{{ resolveUpstream(hop).service?.name }}</dd></div><div v-if="resolveUpstream(hop).podName"><dt>实际上游 Pod</dt><dd>{{ resolveUpstream(hop).podNamespace }}/{{ resolveUpstream(hop).podName }}</dd></div><div><dt>上游响应</dt><dd>HTTP {{ hop.responseCode || '-' }} · {{ hop.durationMillis }} ms</dd></div><div v-if="resolveUpstream(hop).endpoint"><dt>实际 Endpoint 快照</dt><dd :class="`text-${resolveUpstream(hop).endpoint?.status}`">{{ resolveUpstream(hop).endpoint?.statusText }}</dd></div><div v-else-if="resolveUpstream(hop).service"><dt>Registry Service 快照</dt><dd :class="`text-${resolveUpstream(hop).service?.status}`">{{ resolveUpstream(hop).service?.statusText }}</dd></div><div v-else-if="resolveUpstream(hop).externalTarget"><dt>Registry 目标快照</dt><dd :class="`text-${resolveUpstream(hop).externalTarget?.status}`">{{ resolveUpstream(hop).externalTarget?.statusText }}</dd></div><div v-if="hop.responseFlags && hop.responseFlags !== '-'"><dt>响应标志</dt><dd>{{ hop.responseFlags }}</dd></div><div v-if="hop.responseCodeDetails"><dt>响应详情</dt><dd>{{ hop.responseCodeDetails }}</dd></div><div v-if="hop.upstreamTransportFailureReason"><dt>传输失败</dt><dd>{{ hop.upstreamTransportFailureReason }}</dd></div><div v-if="hop.aiLog"><dt>AI log</dt><dd>{{ hop.aiLog }}</dd></div></dl>
                  <section v-for="(extProc, extProcIndex) in hop.extProcs" :key="`${extProc.processor}-${extProcIndex}`" class="ext-proc-evidence" :class="`outcome-${extProc.outcome}`">
                    <header><strong>{{ extProc.processor || 'ext_proc' }}</strong><span>{{ extProcLabel(extProc) }}</span></header>
                    <dl>
                      <div><dt>处理消息 / 耗时</dt><dd>{{ extProcCalls(extProc) }} 次 · {{ extProcLatency(extProc).toFixed(2) }} ms</dd></div>
                      <div><dt>gRPC</dt><dd>{{ extProc.grpcStatus || '-' }}</dd></div>
                      <div v-if="nodeHealthForProcessor(extProc, hop.clusterID)"><dt>K8s 快照健康</dt><dd :class="`text-${nodeHealthForProcessor(extProc, hop.clusterID)?.status}`">{{ nodeHealthForProcessor(extProc, hop.clusterID)?.name }} · {{ nodeHealthForProcessor(extProc, hop.clusterID)?.statusText }}</dd></div>
                      <div v-if="extProc.ruleID"><dt>规则</dt><dd>{{ extProc.ruleID }}</dd></div>
                      <div v-if="extProc.selectedPool"><dt>Pool</dt><dd>{{ extProc.selectedPool }}</dd></div>
                      <div v-if="extProc.selectedEndpoint"><dt>选择 Endpoint</dt><dd>{{ extProc.selectedEndpoint }}</dd></div>
                      <div v-if="extProc.reasonCode"><dt>原因码</dt><dd>{{ extProc.reasonCode }}</dd></div>
                    </dl>
                    <p v-if="extProc.failedOpen || (extProc.failureModeAllowed && extProc.outcome !== 'success')">该处理器失败后继续转发，最终 HTTP 成功不能代表此节点正常。</p>
                  </section>
                  <p v-if="!hop.extProcs?.length" class="ext-proc-missing">未输出 ext_proc Filter State，无法证明本次请求是否调用 BBR/EPP。</p>
                </div>
              </article>
			  <div v-if="!segment.hops.length" class="segment-empty"><AlertTriangle :size="15" /><span>{{ segment.gaps[0] || '该网关是下一跳候选，但没有本次请求的运行时证据。' }}</span></div>
              <p v-if="segment.logSource" class="segment-source">{{ segment.logSource }}</p>
            </section>
            <article v-if="finalHop" class="observed-hop endpoint-hop"><span><Server :size="17" /></span><div><strong>{{ finalUpstreamTitle(finalHop, finalUpstream) }}</strong><p><template v-if="finalUpstream.podName">{{ finalHop.upstreamHost }} · </template>{{ finalHop.upstreamCluster }}</p><p v-if="finalUpstream.registry">配置映射 · {{ registryResolutionLabel(finalUpstream) }}</p><p v-if="finalUpstream.resolutionAmbiguous" class="text-warning">Registry 解析有歧义</p></div><em>{{ finalUpstreamKind(finalUpstream) }}</em></article>
          </div>
          <div v-if="result.error || result.gaps.length" class="probe-gaps"><strong><Clock3 :size="15" />证据缺口</strong><p v-if="result.error">{{ result.error }}</p><p v-for="gap in result.gaps" :key="gap">{{ gap }}</p></div>
        </template>
        <div v-else class="empty-detail large"><RadioTower :size="28" aria-hidden="true" /><h2>等待实时请求</h2><p>结果按联邦拓扑发现跨网关链路，并用各集群访问日志分别验证。</p></div>
      </section>
    </div>
	</div>
  </section>
</template>
