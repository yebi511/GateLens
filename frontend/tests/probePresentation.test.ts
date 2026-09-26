import test from 'node:test'
import assert from 'node:assert/strict'
import { collectionLabel, displayAttemptGroups, finalUpstreamRecord, hasLogValue, isRedirect, probeSummary } from '../src/utils/probePresentation.js'
import type { ProbeExecution, ProbeSegment } from '../src/types.js'
import { caseNames, probeCase, type ProbeCase } from './probeCases.js'

function fixture(): ProbeExecution {
  const hops = [
    { id: 'gateway/1', observedAt: '2026-09-17T07:34:01.306Z', clusterID: 'edge', pod: 'ns/higress', responseCode: 400, responseCodeDetails: 'internal_redirect:ai_usage_via_upstream', durationMillis: 440, evidenceSource: 'log', confidence: 'observed' },
    { id: 'gateway/2', observedAt: '2026-09-17T07:34:01.306Z', clusterID: 'edge', pod: 'ns/higress', responseCode: 200, responseCodeDetails: 'ai_usage_via_upstream', durationMillis: 8736, evidenceSource: 'log', confidence: 'observed', upstreamHost: '-' },
  ]
  const segment: ProbeSegment = { index: 1, clusterID: 'edge', gatewayID: 'gateway', state: 'observed', evidence: 'observed', hops, gaps: [], collection: { state: 'settled' }, attemptGroups: [{ id: 'g1', hopIDs: hops.map((hop) => hop.id), relationState: 'linked', orderBasis: 'source-sequence', links: [{ from: hops[0].id, to: hops[1].id }], terminalCandidateIDs: [hops[1].id], localTerminalHopID: hops[1].id, gaps: [] }] }
  return { id: 'probe', traceID: 'trace', sourceCluster: 'edge', gatewayID: 'gateway', method: 'POST', target: '/v1/chat/completions', state: 'completed', startedAt: hops[0].observedAt, responseCode: 200, durationMillis: 8750, logSource: 'log', hops, segments: [segment], gaps: [], evidenceComplete: true, redirectSummary: { observedRedirects: 1, linkedRedirects: 1, processState: 'observed' }, finalResponseHopID: hops[1].id, finalUpstreamHopID: hops[1].id }
}

test('success preserves redirect and intermediate failure without adding gateway hops', () => {
  const result = fixture()
  const summary = probeSummary(result)
  assert.equal(summary.observedGateways, 1)
  assert.equal(summary.recordCount, 2)
  assert.equal(summary.redirects, 1)
  assert.equal(summary.successful, true)
  const groups = displayAttemptGroups(result.segments[0], result)
  assert.equal(groups[0].defaultOpen, true)
  assert.equal(groups[0].records[0].title, '尝试 1')
  assert.equal(groups[0].records[0].role, '中间响应')
  assert.equal(groups[0].records[0].redirectTo, 'gateway/2')
  assert.equal(groups[0].records[1].role, '最终响应')
  assert.equal(result.durationMillis, 8750)
  assert.equal(groups[0].records[0].hop.durationMillis, 440)
  assert.equal(groups[0].records[1].hop.durationMillis, 8736)
  assert.equal(finalUpstreamRecord(result)?.id, 'gateway/2')
  assert.equal(hasLogValue('-'), false)
})

test('unknown relations and final attribution never use global last record', () => {
  const result = fixture()
  delete result.finalResponseHopID
  delete result.finalUpstreamHopID
  result.segments[0].attemptGroups![0].relationState = 'ambiguous'
  const group = displayAttemptGroups(result.segments[0], result)[0]
  assert.equal(group.records[0].title, '记录 1')
  assert.equal(group.records[0].redirectTo, undefined)
  assert.equal(group.records[1].role, '终止尝试候选')
  assert.equal(finalUpstreamRecord(result), undefined)
  delete result.responseCode
  assert.equal(probeSummary(result).requestLabel, '请求响应状态未知')
  assert.equal(probeSummary(result).requestState, 'unknown')
})

test('legacy fields stay readable, process is unknown, candidate gateways excluded', () => {
  const result = fixture()
  delete result.redirectSummary
  delete result.finalUpstreamHopID
  delete result.segments[0].attemptGroups
  delete result.segments[0].collection
  result.segments.push({ index: 2, clusterID: 'gpu', gatewayID: 'candidate', state: 'missing', evidence: 'inferred', hops: [], gaps: ['missing'] })
  assert.equal(probeSummary(result).observedGateways, 1)
  assert.equal(probeSummary(result).candidateGateways, 1)
  assert.equal(probeSummary(result).processLabel, '内部过程状态未知')
  assert.equal(displayAttemptGroups(result.segments[0], result)[0].records.length, 2)
  assert.equal(collectionLabel(result.segments[0]), '日志采集状态未知')
  assert.equal(finalUpstreamRecord(result), undefined)
})

test('request errors cannot be hidden by a successful log', () => {
  const result = fixture()
  result.state = 'failed'
  result.error = 'response read failed'
  assert.equal(probeSummary(result).successful, false)
  assert.equal(probeSummary(result).requestLabel, '探测请求失败')
})

test('marker boundaries and incomplete group references', () => {
  const result = fixture()
  for (const details of ['via_upstream', 'not_internal_redirect', 'internal_redirect_failed', 'internal_redirect: ']) assert.equal(isRedirect({ ...result.hops[0], responseCodeDetails: details }), false)
  assert.equal(isRedirect({ ...result.hops[0], responseCodeDetails: 'internal_redirect' }), true)
  result.segments[0].attemptGroups![0].hopIDs = ['gateway/1']
  assert.equal(displayAttemptGroups(result.segments[0], result).flatMap((group) => group.records).length, 2)
})

test('reproducible acceptance scenarios preserve counts, gaps, and conservative attribution', () => {
  for (const name of Object.keys(caseNames) as ProbeCase[]) {
    const result = probeCase(name)
    const summary = probeSummary(result)
    const records = result.segments.flatMap((segment) => displayAttemptGroups(segment, result).flatMap((group) => group.records))
    assert.equal(records.length, result.hops.length, name)
    assert.equal(summary.observedGateways, name === 'federation' ? 2 : 1, name)
    assert.equal(summary.redirects, name === 'multiple' ? 2 : name === 'normal' || name === 'error' ? 0 : 1, name)
    assert.equal(summary.successful, name !== 'error', name)
    if (['missing', 'ambiguous', 'legacy', 'federation', 'error'].includes(name)) assert.equal(finalUpstreamRecord(result), undefined, name)
    if (['missing', 'ambiguous', 'legacy'].includes(name)) assert.equal(records.filter((record) => record.redirectTo).length, 0, name)
    if (name === 'multiple') assert.equal(records.filter((record) => record.redirectTo).length, 2)
    assert.ok(!JSON.stringify(records.map((record) => record.hop.aiRouting)).includes('PRIVATE_'))
  }
})
