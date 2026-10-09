import { validateProbeExecution } from '../src/utils/probeProtocol'
import { createApp, h, ref } from 'vue'
import ProbeView from '../src/views/ProbeView.vue'
import { api } from '../src/api/client'
import '../src/styles.css'
import { caseNames, probeCase, type ProbeCase } from './probeCases'
import type { Topology } from '../src/types'

// Local Vite acceptance page: the real view uses synthetic results; no backend request is sent.
const selected = ref<ProbeCase>('custom')
const calls = ref(0)
const errorMessage = ref('')
api.createProbe = async () => { calls.value++; errorMessage.value = ''; const value = structuredClone(probeCase(selected.value)); return validateProbeExecution(selected.value === 'protocol' ? { ...value, schemaVersion: 1 } : value) }
const topology: Topology = { snapshotID: 'synthetic-snapshot', observedAt: '2026-09-17T07:34:01Z', nodes: [], edges: [], truncated: false,
  probeEntries: [{ id: 'entry', gatewayID: 'gateway', clusterID: 'edge', namespace: 'demo', serviceName: 'higress', dnsName: 'higress.demo.svc.cluster.local', port: 80, scheme: 'http', protocol: 'http', displayName: '合成入口（不发送真实流量）' }] }
createApp({ setup: () => () => h('main', { style: 'max-width:1400px;margin:auto;padding:16px;min-width:0' }, [
  h('label', { style: 'display:block;margin-bottom:16px' }, ['合成验收场景 ', h('select', { value: selected.value, onChange: (event: Event) => { selected.value = (event.target as HTMLSelectElement).value as ProbeCase; errorMessage.value = '' } }, Object.entries(caseNames).map(([value, label]) => h('option', { value }, label))), h('span', ` · 已调用 ${calls.value} 次合成结果，未发送 HTTP 请求`)]),
  errorMessage.value ? h('p', { role: 'alert', style: 'color:#b91c1c' }, errorMessage.value) : null,
  h(ProbeView, { key: selected.value, topology, clusterId: 'edge', onError: (message: string) => { errorMessage.value = message } }),
]) }).mount('#app')
