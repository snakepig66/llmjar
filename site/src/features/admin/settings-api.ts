/**
 * 站点系统设置 API。
 *
 * 后端 /api/option/ 要求 RootAuth：
 *  - GET  返回 [{ key, value }]，但会过滤掉所有以 Token / Secret / Key 结尾的
 *         敏感项（SMTPToken、TurnstileSecretKey、EpayKey …），所以敏感值只写不回显。
 *  - PUT  接收单个 { key, value }，值统一按字符串保存。
 */

import { queryOptions } from '@tanstack/react-query'

import { api } from '@/lib/api'

export interface OptionRow {
  key: string
  value: string
}

export type OptionMap = Record<string, string>

export function toOptionMap(rows: unknown): OptionMap {
  const map: OptionMap = {}
  if (!Array.isArray(rows)) return map
  for (const raw of rows) {
    if (!raw || typeof raw !== 'object') continue
    const row = raw as Record<string, unknown>
    const key = typeof row.key === 'string' ? row.key : typeof row.Key === 'string' ? row.Key : ''
    if (!key) continue
    const value = row.value ?? row.Value
    map[key] = typeof value === 'string' ? value : String(value ?? '')
  }
  return map
}

export const adminOptionsQuery = () =>
  queryOptions({
    queryKey: ['admin', 'options'],
    queryFn: async (): Promise<OptionMap> => toOptionMap(await api.get<unknown>('/option/')),
    staleTime: 30_000,
  })

export function updateOption(key: string, value: string): Promise<unknown> {
  return api.put<unknown>('/option/', { key, value })
}

export async function updateOptions(entries: [string, string][]): Promise<void> {
  for (const [key, value] of entries) {
    await updateOption(key, value)
  }
}

/** 1 元等于多少额度。/api/status 的 data.quota_per_unit，读不到时回退 500000。 */
export const siteStatusQuery = () =>
  queryOptions({
    queryKey: ['admin', 'site-status'],
    queryFn: async (): Promise<number> => {
      const raw = (await api.get<Record<string, unknown>>('/status')) ?? {}
      const parsed = Number(raw.quota_per_unit)
      return Number.isFinite(parsed) && parsed > 0 ? parsed : 500_000
    },
    staleTime: 5 * 60_000,
  })

/**
 * 请求策略（重试、自动禁用等）。与 /option/ 分开读写：后端按快照校验整组
 * 策略，PATCH 只提交要改的键，成功后快照与运行时的全局值同步更新。
 */
export const requestPolicyQuery = () =>
  queryOptions({
    queryKey: ['admin', 'request-policy'],
    queryFn: async (): Promise<OptionMap> => {
      const raw = await api.get<{ options?: OptionMap }>('/option/request_policy')
      return raw?.options ?? {}
    },
    staleTime: 30_000,
  })

export function updateRequestPolicy(options: OptionMap): Promise<unknown> {
  return api.patch<unknown>('/option/request_policy', { options })
}

export interface CacheShaveBucket {
  channel_id?: number
  channel_name?: string
  requests: number
  applied: number
  observed: number
  original: number
  effective: number
  shaved: number
}

export interface CacheShaveStats {
  window_hours: number
  total: CacheShaveBucket
  channels: CacheShaveBucket[]
}

/** 缓存削减统计：聚合消费日志管理侧的 cache_shave 记录。 */
export const cacheShaveStatsQuery = (hours = 24) =>
  queryOptions({
    queryKey: ['admin', 'cache-shave-stats', hours],
    queryFn: async (): Promise<CacheShaveStats | null> =>
      (await api.get<CacheShaveStats>('/option/cache_shave/stats', { query: { hours } })) ?? null,
    staleTime: 60_000,
  })

export interface ChannelTokenStatsRow {
  channel_id: number
  channel_name: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  token_share: number
  request_share: number
}

export interface ChannelTokenStats {
  window_hours: number
  total: {
    requests: number
    prompt_tokens: number
    completion_tokens: number
    total_tokens: number
  }
  channels: ChannelTokenStatsRow[]
}

export const channelTokenStatsQuery = (hours = 24) =>
  queryOptions({
    queryKey: ['admin', 'channel-token-stats', hours],
    queryFn: async (): Promise<ChannelTokenStats | null> =>
      (await api.get<ChannelTokenStats>('/option/channel_token_stats', { query: { hours } })) ?? null,
    staleTime: 30_000,
  })
