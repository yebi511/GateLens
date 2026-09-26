import type { ObservedHop, ProbeExecution, ProbeSegment } from '../src/types.js'

export const caseNames = {
  normal: '普通单记录', standard: '标准 internal_redirect', custom: '自定义 filter 扩展',
  multiple: '连续两次 redirect', missing: '只有中间记录', ambiguous: '多终止候选',
  legacy: '旧 Agent 元数据缺失', federation: '多个网关', error: '实际请求失败',
}
export type ProbeCase = keyof typeof caseNames

export function probeCase(name: ProbeCase): ProbeExecution {
  const start = '2026-09-17T07:34:01.306Z'
  const hop = (index: number, details: string, code = 200): ObservedHop => ({
    id: `gateway/record-${index}`, observedAt: start, requestStartTime: start,
    clusterID: 'edge', pod: 'demo/higress-0', runtimeSource: 'edge/demo/higress-0/uid/proxy',
    logSourceID: 'edge/demo/higress-0/uid/proxy', logSequence: index, correlation: 'probe-id',
    routeName: index === 1 ? 'ai-route-default-route.internal' : 'ai-route-fallback-model-route.internal',
    upstreamCluster: `outbound|443||llm-provider-${index}.internal.dns`, upstreamHost: '-',
    downstreamRemoteAddress: '192.0.2.10:52068', downstreamLocalAddress: '192.0.2.20:80',
    authority: 'api.example.test', path: '/v1/chat/completions', responseCode: code,
    responseCodeDetails: details, durationMillis: index === 1 ? 440 : 8736,
    upstreamServiceTimeMillis: index === 1 ? 233 : 8286,
    aiRouting: { provider: index === 1 ? 'provider-a' : 'provider-b', requestModel: 'Qwen3.6-35B-A3B', upstreamModel: index === 1 ? 'Qwen3.5-35B-A3B' : 'qwen3.6-35b-a3b', responseModel: code === 200 ? 'qwen3.6-35b-a3b' : undefined },
    aiLog: '{"question":"PRIVATE_PROMPT_SENTINEL","answer":"PRIVATE_ANSWER_SENTINEL"}',
    evidenceSource: 'kubernetes-pod-log', confidence: 'observed',
  })
  let hops = [hop(1, 'via_upstream')]
  if (name !== 'normal' && name !== 'error') hops = [hop(1, name === 'standard' ? 'internal_redirect' : 'internal_redirect:ai_usage_via_upstream', 400), hop(2, 'via_upstream')]
  if (name === 'multiple') hops = [hop(1, 'internal_redirect', 400), hop(2, 'internal_redirect:another_filter', 503), hop(3, 'via_upstream')]
  if (name === 'missing') hops = [hops[0]!]
  if (name === 'ambiguous') hops.push(hop(3, 'via_upstream'))
  const redirects = hops.filter((record) => record.responseCodeDetails?.startsWith('internal_redirect')).length
  const segment: ProbeSegment = { index: 1, clusterID: 'edge', gatewayID: 'gateway', gatewayName: 'higress',
    state: 'observed', evidence: 'observed', hops, gaps: [], logSource: 'kubernetes-pod-log',
    collection: { state: name === 'missing' ? 'window-ended' : 'settled' },
    attemptGroups: [{ id: 'group-1', runtimeSource: hops[0]!.runtimeSource, hopIDs: hops.map((record) => record.id!),
      orderBasis: 'source-sequence', relationState: name === 'ambiguous' ? 'ambiguous' : name === 'missing' ? 'missing-next' : 'linked',
      links: name === 'ambiguous' ? [] : hops.slice(0, redirects).flatMap((record, index) => hops[index + 1] ? [{ from: record.id!, to: hops[index + 1]!.id! }] : []),
      terminalCandidateIDs: hops.slice(redirects).map((record) => record.id!),
      localTerminalHopID: name === 'ambiguous' || name === 'missing' ? undefined : hops.at(-1)!.id,
      gaps: name === 'ambiguous' ? ['存在多个终止候选，最终尝试归属未确认'] : name === 'missing' ? ['已发生内部重定向，后续尝试记录缺失'] : [],
    }],
  }
  segment.gaps = [...segment.attemptGroups![0]!.gaps]
  const result: ProbeExecution = { id: `synthetic-${name}`, traceID: '0123456789abcdef0123456789abcdef', sourceCluster: 'edge', gatewayID: 'gateway',
    method: 'POST', target: 'http://higress.demo.svc.cluster.local/v1/chat/completions', state: 'completed',
    startedAt: start, completedAt: '2026-09-17T07:34:10.056Z', responseCode: 200, durationMillis: 8750,
    logSource: 'kubernetes-pod-log', hops, segments: [segment], gaps: [...segment.gaps], evidenceComplete: true,
    collection: segment.collection,
    redirectSummary: { observedRedirects: redirects, linkedRedirects: segment.attemptGroups![0]!.links!.length, processState: name === 'missing' ? 'partial' : name === 'ambiguous' ? 'ambiguous' : 'observed' },
  }
  if (name !== 'missing' && name !== 'ambiguous') result.finalResponseHopID = result.finalUpstreamHopID = hops.at(-1)!.id
  if (name === 'legacy') {
    delete segment.collection
    delete segment.attemptGroups
    delete result.collection
    delete result.redirectSummary
    delete result.finalResponseHopID
    delete result.finalUpstreamHopID
    for (const record of hops) { delete record.runtimeSource; delete record.logSourceID; delete record.logSequence; delete record.aiRouting }
  }
  if (name === 'federation') {
    const remoteHop = { ...hop(3, 'via_upstream'), id: 'remote/record-1', clusterID: 'gpu', pod: 'demo/remote-0', runtimeSource: 'gpu/demo/remote-0/uid/proxy', logSourceID: 'gpu/demo/remote-0/uid/proxy', logSequence: 1 }
    result.segments.push({ ...segment, index: 2, clusterID: 'gpu', gatewayID: 'remote', gatewayName: 'remote-gateway', hops: [remoteHop],
      attemptGroups: [{ id: 'group-1', hopIDs: [remoteHop.id], relationState: 'linked', orderBasis: 'source-sequence', terminalCandidateIDs: [remoteHop.id], localTerminalHopID: remoteHop.id, gaps: [] }] })
    result.segments.push({ index: 3, clusterID: 'gpu', gatewayID: 'candidate', gatewayName: 'candidate-gateway', state: 'missing', evidence: 'inferred', hops: [], gaps: ['配置可关联，但缺少本次请求日志'], inferenceBasis: '上游 Cluster 精确匹配', inferenceConfidence: 'medium' })
    result.hops = result.segments.flatMap((gateway) => gateway.hops)
    result.evidenceComplete = false
    result.gaps = ['候选网关缺少运行时证据']
    result.redirectSummary!.processState = 'partial'
    delete result.finalUpstreamHopID
  }
  if (name === 'error') {
    result.state = 'failed'; result.error = '合成样例：响应读取失败'; delete result.finalResponseHopID; delete result.finalUpstreamHopID
  }
  hops.at(-1)!.extProcs = [{ processor: 'bbr-demo', invoked: true, requestHeaderCalls: 1, requestHeaderLatencyUs: 1800, failedOpen: true, failureModeAllowed: true, messageTimeout: true, httpError: false, receivedImmediateResponse: false, grpcStatus: '4', outcome: 'fail-open' }]
  return result
}
