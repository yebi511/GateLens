import type { ObservedHop, ProbeExecution, ProbeSegment } from '../types.js'

export function hasLogValue(value?: string): value is string {
  return Boolean(value?.trim() && value.trim() !== '-')
}

export function isRedirect(hop: ObservedHop) {
  const details = hop.responseCodeDetails?.trim() ?? ''
  return details === 'internal_redirect' || (details.startsWith('internal_redirect:') && details.slice('internal_redirect:'.length).trim() !== '')
}

export function probeSegments(result: ProbeExecution): ProbeSegment[] {
  if (result.segments?.length) return result.segments
  return [{ index: 1, clusterID: result.sourceCluster, gatewayID: result.gatewayID,
    state: result.hops.length ? 'observed' : 'missing', evidence: result.hops.length ? 'observed' : 'inferred',
    logSource: result.logSource, hops: result.hops, gaps: result.gaps, collection: result.collection }]
}

export function probeSummary(result: ProbeExecution) {
  const segments = probeSegments(result)
  const records = segments.flatMap((segment) => segment.hops)
  const responseCode = result.responseCode ?? 0
  const finished = result.state === 'completed' && !result.error
  const successful = finished && responseCode >= 200 && responseCode < 400
  return {
    observedGateways: segments.filter((segment) => segment.hops.length > 0).length,
    candidateGateways: segments.filter((segment) => segment.hops.length === 0).length,
    recordCount: records.length,
    redirects: result.redirectSummary?.observedRedirects ?? records.filter(isRedirect).length,
    processLabel: ({ observed: '已关联显示的尝试记录', partial: '内部过程存在证据缺口', ambiguous: '尝试关系存在歧义', unknown: '内部过程状态未知' })[result.redirectSummary?.processState ?? 'unknown'],
    requestLabel: finished ? responseCode > 0 ? `HTTP ${responseCode} · ${successful ? '请求完成' : '请求返回错误'}` : '请求响应状态未知' : '探测请求失败',
    requestState: !finished ? 'error' : responseCode <= 0 ? 'unknown' : successful ? 'success' : 'error',
    successful,
  }
}

export interface ProbeDisplayRecord {
  hop: ObservedHop
  key: string
  title: string
  role: string
  redirect: boolean
  redirectTo?: string
}

export function displayAttemptGroups(segment: ProbeSegment, result: ProbeExecution) {
  const lookup = new Map(segment.hops.filter((hop) => hop.id).map((hop) => [hop.id!, hop]))
  const groups = segment.attemptGroups?.length ? segment.attemptGroups : [{
    id: 'legacy', hopIDs: [], relationState: 'unconfirmed' as const, orderBasis: 'unavailable' as const, gaps: [],
  }]
  const displays = groups.map((group) => {
    const hops = group.id === 'legacy' && group.hopIDs.length === 0 ? segment.hops : group.hopIDs.flatMap((id) => lookup.get(id) ? [lookup.get(id)!] : [])
    const ordered = group.orderBasis === 'source-sequence' && group.relationState !== 'ambiguous' && group.relationState !== 'unconfirmed'
    const links = new Map((group.links ?? []).map((link) => [link.from, link.to]))
    const records: ProbeDisplayRecord[] = hops.map((hop, index) => {
      const redirect = isRedirect(hop)
      const final = Boolean(hop.id && hop.id === result.finalResponseHopID)
      return { hop, key: hop.id ?? `${hop.pod}-${index}`, title: `${ordered ? '尝试' : '记录'} ${index + 1}`,
        role: redirect ? '中间响应' : final ? '最终响应' : group.relationState === 'linked' && hop.id && hop.id === group.localTerminalHopID ? '网关终止尝试' : '终止尝试候选',
        redirect, redirectTo: ordered && hop.id ? links.get(hop.id) : undefined }
    })
    return { id: group.id, title: hops[0]?.pod || '日志来源', records, gaps: group.gaps,
      defaultOpen: hops.length <= 1 || records.some((record) => record.redirect || (record.hop.responseCode ?? 0) >= 400),
      relationLabel: ({ linked: hops.length > 1 ? '已关联内部尝试' : '单条访问记录', unconfirmed: hops.length > 1 ? '同请求多条记录，关系未确认' : '内部过程状态未知', 'missing-next': '后续尝试记录缺失', ambiguous: '同请求多条记录，关系未确认' })[group.relationState] }
  })
  // Never silently omit records when a mixed-version payload has incomplete references.
  const represented = new Set(displays.flatMap((group) => group.records.map((record) => record.hop)))
  const missing = segment.hops.filter((hop) => !represented.has(hop))
  if (missing.length) displays.push({ id: 'unreferenced', title: '未归组记录', records: missing.map((hop, index) => ({ hop, key: hop.id ?? `extra-${index}`, title: `记录 ${index + 1}`, role: isRedirect(hop) ? '中间响应' : '终止尝试候选', redirect: isRedirect(hop), redirectTo: undefined })), gaps: ['记录缺少完整分组引用，关系未确认'], defaultOpen: true, relationLabel: '同请求多条记录，关系未确认' })
  return displays
}

export function finalUpstreamRecord(result: ProbeExecution) {
  if (!result.finalUpstreamHopID) return undefined
  return probeSegments(result).flatMap((segment) => segment.hops).find((hop) => hop.id === result.finalUpstreamHopID)
}

export function collectionLabel(segment: ProbeSegment) {
  return ({ settled: '采集窗口内已观察到稳定终止候选', 'window-ended': '日志采集窗口已结束', cancelled: '日志采集已取消', 'read-error': '日志读取失败', unknown: '日志采集状态未知' })[segment.collection?.state ?? 'unknown']
}
