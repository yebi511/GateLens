import test from 'node:test'
import assert from 'node:assert/strict'
import { collectionLabel, displayContexts, finalUpstreamRecord, hasLogValue, isRedirect, probeSummary, probeIssues } from '../src/utils/probePresentation.js'
import { validateProbeExecution } from '../src/utils/probeProtocol.js'
import { caseNames, probeCase, type ProbeCase } from './probeCases.js'

test('success preserves intermediate failure and orders only by confirmed links', () => {
  const result = probeCase('custom'), segment = result.segments[0]!
  segment.hops.reverse()
  const summary = probeSummary(result), context = displayContexts(segment, result)[0]!
  assert.equal(summary.observedGateways, 1); assert.equal(summary.recordCount, 2)
  assert.equal(summary.redirects, 1); assert.equal(summary.linkedRedirects, 1); assert.equal(summary.successful, true)
  assert.equal(context.defaultOpen, true); assert.equal(context.records[0]!.title, '尝试 1')
  assert.equal(context.records[0]!.role, 'intermediate-response'); assert.equal(context.records[0]!.redirectTo, 'gateway/record-2')
  assert.equal(context.records[1]!.role, 'final-response'); assert.equal(context.records[0]!.hop.durationMillis, 440)
  assert.equal(result.durationMillis, 8750); assert.equal(finalUpstreamRecord(result)?.id, 'gateway/record-2')
  assert.equal(hasLogValue('-'), false)
})
test('unknown relations never invent order or final attribution', () => {
  for (const name of ['ambiguous', 'unknown'] as const) {
    const result = probeCase(name), context = displayContexts(result.segments[0]!, result)[0]!
    assert.ok(context.records.every((record) => record.title === '访问记录' && !record.redirectTo))
    assert.equal(context.relationLabel, '同请求多条记录，关系未确认')
    assert.equal(context.records.at(-1)!.role, 'terminal-candidate'); assert.equal(finalUpstreamRecord(result), undefined)
  }
  const result = probeCase('unknown')
  assert.equal(collectionLabel(result.segments[0]!), '日志采集状态未知')
  assert.equal(probeSummary(result).processState, 'unknown')
  delete result.responseCode; assert.equal(probeSummary(result).requestState, 'unknown')
})
test('mixed reliable and unknown contexts retain reliable links and local terminal roles', () => {
  const result = probeCase('mixed'), contexts = displayContexts(result.segments[0]!, result)
  assert.equal(contexts.length, 2); assert.equal(contexts[0]!.records[0]!.redirectTo, 'gateway/record-2')
  assert.equal(contexts[0]!.records[1]!.role, 'local-terminal')
  assert.equal(contexts[1]!.records[0]!.title, '访问记录'); assert.equal(contexts[1]!.records[0]!.redirectTo, undefined)
  assert.equal(probeSummary(result).processState, 'unknown'); assert.equal(finalUpstreamRecord(result), undefined)
})
test('issues deduplicate by identity and preserve gateway owners', () => {
  const result = probeCase('federation'), candidate = result.segments[2]!
  candidate.issues.push({ ...candidate.issues[0]!, message: 'different text' })
  result.segments[1]!.issues.push({ ...candidate.issues[0]! })
  assert.equal(probeIssues(result).length, 2); assert.notEqual(probeIssues(result)[0]!.owner, probeIssues(result)[1]!.owner)
  assert.equal(probeSummary(probeCase('error')).successful, false)
  assert.equal(probeSummary(probeCase('error')).requestLabel, '探测请求失败')
})
test('presentation trusts parser flags and never reparses raw redirect details', () => {
  const hop = probeCase('custom').segments[0]!.hops[0]!
  assert.equal(isRedirect({ ...hop, internalRedirect: false }), false)
  assert.equal(isRedirect({ ...hop, responseCodeDetails: 'other-detail' }), true)
})
test('protocol errors are distinct from valid missing metadata and open log fields', () => {
  for (const version of [undefined, 1, 3]) assert.throws(() => validateProbeExecution({ ...probeCase('normal'), schemaVersion: version }), /协议不支持/)
  const mutations: ((value: ReturnType<typeof probeCase>) => void)[] = [
    (v) => { Object.assign(v, { state: 'done' }) },
    (v) => { Object.assign(v, { method: 'CUSTOM' }) },
    (v) => { Object.assign(v, { snapshotConsistency: 'ok' }) },
    (v) => { Object.assign(v.segments[0], { inferenceConfidence: 'observed' }) },
    (v) => { Object.assign(v.segments[0]!.hops[0], { confidence: 'high' }) },
    (v) => { Object.assign(v.segments[0]!.collection, { state: 'ok' }) },
    (v) => { Object.assign(v.segments[0], { relationState: 'ok' }) },
    (v) => { Object.assign(v.segments[0]!.hops[0], { correlation: 'exact' }) },
    (v) => { Object.assign(v.segments[0]!.hops[0]!.extProcs![0], { outcome: 'ok' }) },
    (v) => { v.issues.push({ scope: 'execution', code: 'invalid' as never, message: '' }) },
    (v) => { v.segments[0]!.links = [{ from: 'missing', to: 'gateway/record-1' }] },
    (v) => { v.finalResponseHopID = 'missing' },
  ]
  for (const mutate of mutations) { const value = probeCase('normal'); mutate(value); assert.throws(() => validateProbeExecution(value)) }
  const value = probeCase('unknown'), hop = value.segments[0]!.hops[0]!
  Object.assign(hop, { method: 'CUSTOM', protocol: 'HTTP/99', responseFlags: 'NEW_FLAG', responseCodeDetails: 'new_filter:detail' })
  assert.equal(validateProbeExecution(value), value)
})
test('all acceptance cases preserve counts and conservative attribution', () => {
  for (const name of Object.keys(caseNames) as ProbeCase[]) {
    const result = validateProbeExecution(probeCase(name)), summary = probeSummary(result)
    const records = result.segments.flatMap((segment) => displayContexts(segment, result).flatMap((context) => context.records))
    assert.equal(records.length, result.segments.flatMap((segment) => segment.hops).length, name)
    assert.equal(summary.observedGateways, name === 'federation' ? 2 : 1, name)
    assert.equal(summary.redirects, name === 'multiple' ? 2 : name === 'normal' || name === 'error' ? 0 : 1, name)
    assert.equal(summary.successful, name !== 'error', name)
    if (['missing', 'ambiguous', 'unknown', 'mixed', 'federation', 'error'].includes(name)) assert.equal(finalUpstreamRecord(result), undefined, name)
    assert.ok(!JSON.stringify(records.map((record) => record.hop.aiRouting)).includes('PRIVATE_'))
  }
})
