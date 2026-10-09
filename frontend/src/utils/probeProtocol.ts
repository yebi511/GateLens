import type { ProbeExecution } from '../types.js'
import { ProbeExecutionStateValues, ProbeHTTPMethodValues, SnapshotConsistencyValues, ProbeCollectionStateValues,
  ProbeAttemptRelationStateValues, ProbeInferenceConfidenceValues, ProbeIssueScopeValues, ProbeIssueCodeValues,
  ProbeCorrelationValues, ObservationConfidenceValues, ExtProcOutcomeValues } from '../types.js'

type ObjectValue = Record<string, unknown>
function object(value: unknown): ObjectValue {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('探测结果结构无效')
  return value as ObjectValue
}
function array(value: unknown): unknown[] {
  if (!Array.isArray(value)) throw new Error('探测结果缺少记录或问题数组')
  return value
}
function member(value: unknown, values: readonly string[], field: string, optional = false) {
  if (optional && value === undefined) return
  if (typeof value !== 'string' || !values.includes(value)) throw new Error(`探测结果包含非法 ${field}`)
}
function identity(value: unknown): string {
  if (typeof value !== 'string' || !value) throw new Error('探测结果缺少身份或引用')
  return value
}
function issues(value: unknown, execution: boolean) {
  for (const entry of array(value)) {
    const issue = object(entry)
    member(issue.scope, ProbeIssueScopeValues, 'issue.scope')
    member(issue.code, ProbeIssueCodeValues, 'issue.code')
    if ((issue.scope === 'execution') !== execution || typeof issue.message !== 'string') throw new Error('探测问题归属无效')
  }
}
// Version and closed values are checked before any probe reaches the view.
export function validateProbeExecution(payload: unknown): ProbeExecution {
  const value = object(payload)
  if (value.schemaVersion !== 2) throw new Error('探测协议不支持：需要 schemaVersion 2，请同步升级前端、Server 和 Agent')
  for (const field of ['id', 'traceID', 'sourceCluster', 'gatewayID']) identity(value[field])
  member(value.state, ProbeExecutionStateValues, 'state')
  member(value.method, ProbeHTTPMethodValues, 'method')
  member(value.snapshotConsistency, SnapshotConsistencyValues, 'snapshotConsistency', true)
  issues(value.issues, true)
  const ids = new Map<string, ObjectValue>()
  const gateways = new Set<string>()
  for (const entry of array(value.segments)) {
    const segment = object(entry)
    const gateway = identity(segment.gatewayID)
    identity(segment.clusterID)
    if (gateways.has(gateway)) throw new Error('探测结果包含重复网关')
    gateways.add(gateway)
    member(object(segment.collection).state, ProbeCollectionStateValues, 'collection.state')
    member(segment.relationState, ProbeAttemptRelationStateValues, 'relationState')
    member(segment.inferenceConfidence, ProbeInferenceConfidenceValues, 'inferenceConfidence', true)
    issues(segment.issues, false)
    const local = new Map<string, ObjectValue>()
    for (const record of array(segment.hops)) {
      const hop = object(record)
      const id = identity(hop.id)
      identity(hop.contextID)
      if (ids.has(id) || hop.clusterID !== segment.clusterID) throw new Error('探测记录身份无效')
      member(hop.correlation, ProbeCorrelationValues, 'correlation', true)
      member(hop.confidence, ObservationConfidenceValues, 'confidence')
      if (hop.internalRedirect !== undefined && typeof hop.internalRedirect !== 'boolean') throw new Error('探测重定向标记无效')
      for (const proc of hop.extProcs === undefined ? [] : array(hop.extProcs)) member(object(proc).outcome, ExtProcOutcomeValues, 'ext_proc.outcome')
      ids.set(id, hop); local.set(id, hop)
    }
    const froms = new Set<string>(), tos = new Set<string>()
    for (const entry of array(segment.links)) {
      const link = object(entry), from = identity(link.from), to = identity(link.to)
      const a = local.get(from), b = local.get(to)
      if (!a || !b || from === to || a.contextID !== b.contextID || !a.internalRedirect || froms.has(from) || tos.has(to)) throw new Error('探测连接引用无效')
      froms.add(from); tos.add(to)
    }
    for (const ref of array(segment.localTerminalHopIDs)) {
      const hop = local.get(identity(ref))
      if (!hop || hop.internalRedirect) throw new Error('探测终止引用无效')
    }
    for (const entry of array(segment.issues)) {
      const issue = object(entry)
      if (issue.hopID !== undefined && !local.has(identity(issue.hopID))) throw new Error('探测问题记录引用无效')
      if (issue.contextID !== undefined && ![...local.values()].some((hop) => hop.contextID === issue.contextID)) throw new Error('探测问题上下文引用无效')
    }
  }
  for (const field of ['finalResponseHopID', 'finalUpstreamHopID']) {
    if (value[field] !== undefined && !ids.has(identity(value[field]))) throw new Error('探测最终引用无效')
  }
  return payload as ProbeExecution
}
