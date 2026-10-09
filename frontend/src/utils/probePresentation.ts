import type { ObservedHop, ProbeExecution, ProbeSegment, ProbeIssue } from '../types.js'

export type ProbeRequestDisplayState = 'error' | 'unknown' | 'success'
export type ProbeProcessDisplayState = 'observed' | 'partial' | 'ambiguous' | 'unknown'
export type ProbeRecordRole = 'intermediate-response' | 'final-response' | 'local-terminal' | 'terminal-candidate'
export const recordRoleLabels: Record<ProbeRecordRole, string> = {
  'intermediate-response': '中间响应', 'final-response': '最终响应', 'local-terminal': '网关终止尝试', 'terminal-candidate': '终止尝试候选',
}
const processLabels: Record<ProbeProcessDisplayState, string> = {
  observed: '已关联显示的尝试记录', partial: '内部过程存在证据缺口', ambiguous: '尝试关系存在歧义', unknown: '内部过程状态未知',
}
export function hasLogValue(value?: string): value is string {
  return Boolean(value?.trim() && value.trim() !== '-')
}
export function isRedirect(hop: ObservedHop) { return hop.internalRedirect === true }
export function probeSegments(result: ProbeExecution) { return result.segments }
function uniqueIssues(issues: ProbeIssue[]) {
  const seen = new Set<string>()
  return issues.filter((issue) => {
    const key = JSON.stringify([issue.scope, issue.code, issue.contextID, issue.hopID])
    if (seen.has(key)) return false
    seen.add(key); return true
  })
}
export function probeIssues(result: ProbeExecution) {
  return [ ...uniqueIssues(result.issues).map((issue) => ({ ...issue, ownerID: 'execution', owner: '探测请求' })),
    ...result.segments.flatMap((segment) => uniqueIssues(segment.issues).map((issue) => ({ ...issue, ownerID: segment.gatewayID, owner: `${segment.clusterID} / ${segment.gatewayName || segment.gatewayID}` }))) ]
}
export function probeSummary(result: ProbeExecution) {
  const segments = result.segments, records = segments.flatMap((segment) => segment.hops)
  const responseCode = result.responseCode ?? 0
  const finished = result.state === 'completed' && !result.error
  const successful = finished && responseCode >= 200 && responseCode < 400
  const issueList = probeIssues(result)
  const processState: ProbeProcessDisplayState = segments.some((segment) => segment.relationState === 'ambiguous') ? 'ambiguous'
    : segments.length === 0 || segments.some((segment) => segment.hops.length > 0 && segment.relationState === 'unconfirmed') || issueList.some((issue) => ['request-identity-incomplete', 'attempt-order-unconfirmed', 'log-order-unconfirmed', 'log-source-rotated', 'log-window-truncated'].includes(issue.code)) ? 'unknown'
    : issueList.length > 0 || segments.some((segment) => !segment.hops.length || segment.collection.state !== 'settled' || segment.relationState === 'missing-next') ? 'partial' : 'observed'
  const requestState: ProbeRequestDisplayState = !finished ? 'error' : responseCode <= 0 ? 'unknown' : successful ? 'success' : 'error'
  return {
    observedGateways: segments.filter((segment) => segment.hops.length > 0).length,
    candidateGateways: segments.filter((segment) => !segment.hops.length).length,
    recordCount: records.length, redirects: records.filter(isRedirect).length,
    linkedRedirects: segments.reduce((count, segment) => count + segment.links.length, 0),
    processState, processLabel: processLabels[processState],
    requestLabel: finished ? responseCode > 0 ? `HTTP ${responseCode} · ${successful ? '请求完成' : '请求返回错误'}` : '请求响应状态未知' : '探测请求失败',
    requestState, successful,
  }
}
export interface ProbeDisplayRecord {
  hop: ObservedHop
  key: string
  title: string
  role: ProbeRecordRole
  redirect: boolean
  redirectTo?: string
}
// ContextID only partitions records. Confirmed links alone determine attempt order.
export function displayContexts(segment: ProbeSegment, result: ProbeExecution) {
  const contexts = new Map<string, ObservedHop[]>()
  for (const hop of segment.hops) {
    const records = contexts.get(hop.contextID) ?? []
    records.push(hop); contexts.set(hop.contextID, records)
  }
  return [...contexts].map(([id, hops]) => {
    const byID = new Map(hops.map((hop) => [hop.id, hop]))
    const links = new Map(segment.links.filter((link) => byID.has(link.from) && byID.has(link.to)).map((link) => [link.from, link.to]))
    const incoming = new Set(links.values()), visited = new Set<string>()
    const records: ProbeDisplayRecord[] = []
    const append = (hop: ObservedHop, position?: number) => {
      visited.add(hop.id)
      const role: ProbeRecordRole = isRedirect(hop) ? 'intermediate-response' : hop.id === result.finalResponseHopID ? 'final-response' : segment.localTerminalHopIDs.includes(hop.id) ? 'local-terminal' : 'terminal-candidate'
      records.push({ hop, key: hop.id, title: position === undefined ? '访问记录' : `尝试 ${position}`, role, redirect: isRedirect(hop), redirectTo: links.get(hop.id) })
    }
    for (const root of hops.filter((hop) => !incoming.has(hop.id))) {
      if (visited.has(root.id)) continue
      let current: ObservedHop | undefined = root, index = 1
      const ordered = links.has(root.id) || segment.localTerminalHopIDs.includes(root.id)
      while (current && !visited.has(current.id)) {
        append(current, ordered ? index++ : undefined)
        current = byID.get(links.get(current.id) ?? '')
      }
    }
    for (const hop of hops) if (!visited.has(hop.id)) append(hop)
    const issues = uniqueIssues(segment.issues.filter((issue) => issue.contextID === id))
    return { id, title: hops[0]?.pod || '日志来源', records, issues,
      defaultOpen: hops.length <= 1 || records.some((record) => record.redirect || (record.hop.responseCode ?? 0) >= 400),
      relationLabel: issues.some((issue) => issue.code === 'terminal-ambiguous') ? '同请求多条记录，关系未确认'
        : issues.some((issue) => issue.code === 'attempt-order-unconfirmed' || issue.code === 'request-identity-incomplete') ? hops.length > 1 ? '同请求多条记录，关系未确认' : '内部过程状态未知'
        : issues.some((issue) => issue.code === 'redirect-next-missing') ? '后续尝试记录缺失'
        : links.size ? '已关联内部尝试' : segment.localTerminalHopIDs.includes(hops[0]!.id) ? '单条访问记录' : '内部过程状态未知' }
  })
}
export function finalUpstreamRecord(result: ProbeExecution) {
  return result.segments.flatMap((segment) => segment.hops).find((hop) => hop.id === result.finalUpstreamHopID)
}
export function collectionLabel(segment: ProbeSegment) {
  return ({ settled: '采集窗口内已观察到稳定终止候选', 'window-ended': '日志采集窗口已结束', cancelled: '日志采集已取消', 'read-error': '日志读取失败', unknown: '日志采集状态未知' })[segment.collection.state]
}
