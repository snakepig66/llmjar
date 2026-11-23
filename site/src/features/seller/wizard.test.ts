import { describe, expect, it } from 'vitest'

import type { ProbeResult } from './api'
import {
  buildVerificationMatrix,
  canSubmit,
  maskApiKey,
  parseApiKeys,
  validCustomURL,
  validRequestHeaders,
  type WizardSnapshot,
} from './wizard'

const okProbe: ProbeResult = {
  ok: true,
  models: ['deepseek-flash', 'deepseek-v4-pro'],
  verified_models: ['deepseek-flash'],
  missing_models: [],
  latency_ms: 120,
  prompt_cache: true,
  balance_micro: 10_000_000,
  key_valid: true,
  fail_reason: '',
  trust_pool: {
    matched: true,
    trust: 'trusted',
    kind: 'official',
    vendor_name: 'DeepSeek',
    endpoints: ['openai_chat', 'anthropic_messages'],
    badge_text: 'DeepSeek · 官方 · OpenAI + Anthropic · 免审核',
  },
}

function snapshot(overrides: Partial<WizardSnapshot> = {}): WizardSnapshot {
  return {
    url: 'https://api.deepseek.com',
    vendorId: 'deepseek',
    apiKeys: ['sk-test-1234567890'],
    hasPassedKey: true,
    trust: okProbe.trust_pool ?? null,
    probe: okProbe,
    availableModels: ['deepseek-flash', 'deepseek-v4-pro'],
    selectedModels: ['deepseek-flash'],
    modelMultipliers: { 'deepseek-flash': 500 },
    concurrency: 5,
    rpm: 0,
    dailyCapYuan: '0',
    hoursValid: true,
    ...overrides,
  }
}

describe('wizard key parsing', () => {
  it('splits lines, trims, drops blanks and de-duplicates', () => {
    expect(parseApiKeys(' sk-a \n\nsk-b\r\nsk-a\n')).toEqual(['sk-a', 'sk-b'])
    expect(parseApiKeys('')).toEqual([])
  })
})

describe('listing submission gate', () => {
  it('accepts the default limits and a valid multiplier', () => {
    expect(canSubmit(snapshot())).toBe(true)
  })

  it('requires a selected model the keys can call and a valid multiplier', () => {
    expect(canSubmit(snapshot({ selectedModels: [] }))).toBe(false)
    expect(canSubmit(snapshot({ selectedModels: ['not-served'] }))).toBe(false)
    expect(
      canSubmit(snapshot({ modelMultipliers: { 'deepseek-flash': 0 } })),
    ).toBe(false)
    expect(
      canSubmit(snapshot({ modelMultipliers: { 'deepseek-flash': 1100 } })),
    ).toBe(false)
  })

  it('requires valid limits and headers', () => {
    expect(canSubmit(snapshot({ concurrency: 0 }))).toBe(false)
    expect(canSubmit(snapshot({ rpm: -1 }))).toBe(false)
    expect(canSubmit(snapshot({ dailyCapYuan: 'abc' }))).toBe(false)
    expect(canSubmit(snapshot({ headersValid: false }))).toBe(false)
    expect(canSubmit(snapshot({ headersValid: true }))).toBe(true)
  })

  it('requires the plan-terms checkbox for a plan channel', () => {
    expect(canSubmit(snapshot({ requiresPlanTerms: true }))).toBe(false)
    expect(
      canSubmit(snapshot({ requiresPlanTerms: true, planTermsAccepted: true })),
    ).toBe(true)
  })
})

describe('wizard custom URL and headers', () => {
  it('validCustomURL refuses userinfo, query and whitespace', () => {
    expect(validCustomURL('https://user:pass@api.example.com')).toBe(false)
    expect(validCustomURL('https://api.example.com/v1?x=1')).toBe(false)
    expect(validCustomURL('  https://api.example.com  ')).toBe(true)
    expect(validCustomURL('https://api.example.com#frag')).toBe(false)
    expect(validCustomURL('https://')).toBe(false)
    // loopback allows http
    expect(validCustomURL('http://127.0.0.1:3202')).toBe(true)
    expect(validCustomURL('http://localhost:3202')).toBe(true)
    expect(validCustomURL('http://[::1]:3202')).toBe(true)
    // non-loopback requires https
    expect(validCustomURL('http://api.example.com')).toBe(false)
  })

  it('accepts key: value and key=value header lines', () => {
    expect(validRequestHeaders('')).toBe(true)
    expect(validRequestHeaders('X-Foo: bar\nX-Baz=qux')).toBe(true)
    expect(validRequestHeaders('X-Foo bar')).toBe(false)
    expect(validRequestHeaders('X Foo: bar')).toBe(false)
    expect(validRequestHeaders('X-Foo:')).toBe(false)
  })
})

describe('verification matrix', () => {
  it('crosses every key, model and endpoint and marks callable models', () => {
    const cells = buildVerificationMatrix(
      ['sk-one-123456789', 'sk-two-123456789'],
      ['deepseek-flash', 'deepseek-v4-pro'],
      ['openai_chat', 'anthropic_messages'],
      {
        'sk-one-123456789': okProbe,
        'sk-two-123456789': { ...okProbe, key_valid: false, fail_reason: 'key rejected' },
      },
    )
    expect(cells).toHaveLength(8)
    const firstKeyFlash = cells.filter(
      (cell) => cell.key === 'sk-one-123456789' && cell.model === 'deepseek-flash',
    )
    expect(firstKeyFlash).toHaveLength(2)
    expect(firstKeyFlash.every((cell) => cell.ok)).toBe(true)
    expect(
      cells.filter((cell) => cell.key === 'sk-one-123456789' && cell.model === 'deepseek-v4-pro'),
    ).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ ok: false, failReason: '该模型不在这个 Key 的可用列表中' }),
      ]),
    )
    expect(
      cells
        .filter((cell) => cell.key === 'sk-two-123456789')
        .every((cell) => !cell.ok && cell.failReason === 'key rejected'),
    ).toBe(true)
  })

  it('marks an unprobed key as not checked and defaults to one endpoint', () => {
    const cells = buildVerificationMatrix(['sk-missing'], ['deepseek-flash'], [], {})
    expect(cells).toHaveLength(1)
    expect(cells[0]).toMatchObject({ endpoint: 'openai_chat', ok: false, failReason: '尚未检测' })
  })

  it('masks keys to the last four characters', () => {
    expect(maskApiKey('sk-abcdefgh')).toBe('••••efgh')
    expect(maskApiKey('abc')).toBe('••••')
  })
})
