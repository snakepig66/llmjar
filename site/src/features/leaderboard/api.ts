/**
 * Public leaderboard API shapes and query options (P6).
 *
 * Ranges are sliding windows owned by the backend: "24h" is now-24h, "7d" is
 * now-7d. The trend endpoint buckets by Asia/Shanghai calendar day.
 *
 * The seller board is already anonymized server-side: it carries only the
 * alias, never a seller id. Money arrives as integer micro-yuan, so render it
 * through lib/money.ts and never do arithmetic on it here.
 */

import { queryOptions } from '@tanstack/react-query'

import { api } from '@/lib/api'

export type LeaderboardRange = '24h' | '7d' | '30d'

export interface ModelLeaderboardItem {
  rank: number
  model: string
  requests: number
  /** Thousandths of a percent: 995 = 99.5%; -1 = no sample. */
  success_rate_milli: number
  /** Average relay latency in milliseconds; 0 = no sample. */
  avg_latency_ms: number
  /** Input + output tokens over the window. */
  tokens: number
}

export interface ModelLeaderboard {
  range: LeaderboardRange
  window: 'sliding'
  window_start: string
  window_end: string
  generated_at?: string
  items: ModelLeaderboardItem[]
}

export interface SellerLeaderboardItem {
  rank: number
  /** Anonymized server-side, e.g. "分享者 #a1b2". */
  alias: string
  requests: number
  /** Thousandths of a percent; -1 = no sample. */
  success_rate_milli: number
  /** Seller income in micro-yuan. */
  income_micro: number
}

export interface SellerLeaderboard {
  range: LeaderboardRange
  window: 'sliding'
  window_start: string
  window_end: string
  generated_at?: string
  items: SellerLeaderboardItem[]
}

export interface BuyerLeaderboardItem {
  rank: number
  /** Anonymized server-side, e.g. "用户 #a1b2". */
  alias: string
  requests: number
  /** Thousandths of a percent; -1 = no sample. */
  success_rate_milli: number
  /** Input + output tokens over the window. */
  tokens: number
}

export interface BuyerLeaderboard {
  range: LeaderboardRange
  window: 'sliding'
  window_start: string
  window_end: string
  generated_at?: string
  items: BuyerLeaderboardItem[]
}

export interface VendorLeaderboardItem {
  rank: number
  /** Channel slug; every seller-defined "custom-…" channel folds into 'custom'. */
  vendor: string
  /** Pool display name; empty for the folded 'custom' bucket. */
  display_name: string
  requests: number
  tokens: number
  /** Distinct models served over the window. */
  models: number
  /** Thousandths of a percent of the window's qualifying tokens. */
  share_milli: number
}

export interface VendorLeaderboard {
  range: LeaderboardRange
  window: 'sliding'
  window_start: string
  window_end: string
  generated_at?: string
  total_tokens: number
  items: VendorLeaderboardItem[]
}

export interface TrendDay {
  /** Asia/Shanghai calendar day, YYYY-MM-DD. */
  date: string
  requests: number
  tokens: number
}

export interface LeaderboardTrend {
  days: number
  /** First and last day covered; the backend returns dense zero-filled days. */
  start?: string
  end?: string
  model?: string
  items: TrendDay[]
}

const CACHE_TTL = 60_000

export const leaderboardModelsQuery = (range: LeaderboardRange, limit = 20) =>
  queryOptions({
    queryKey: ['marketplace', 'leaderboard', 'models', range, limit],
    queryFn: () =>
      api.get<ModelLeaderboard>('/marketplace/leaderboard/models', {
        query: { range, limit },
      }),
    staleTime: CACHE_TTL,
    refetchInterval: CACHE_TTL,
  })

export const leaderboardSellersQuery = (range: LeaderboardRange, limit = 20) =>
  queryOptions({
    queryKey: ['marketplace', 'leaderboard', 'sellers', range, limit],
    queryFn: () =>
      api.get<SellerLeaderboard>('/marketplace/leaderboard/sellers', {
        query: { range, limit },
      }),
    staleTime: CACHE_TTL,
    refetchInterval: CACHE_TTL,
  })

export const leaderboardBuyersQuery = (range: LeaderboardRange, limit = 20) =>
  queryOptions({
    queryKey: ['marketplace', 'leaderboard', 'buyers', range, limit],
    queryFn: () =>
      api.get<BuyerLeaderboard>('/marketplace/leaderboard/buyers', {
        query: { range, limit },
      }),
    staleTime: CACHE_TTL,
    refetchInterval: CACHE_TTL,
  })

export const leaderboardVendorsQuery = (range: LeaderboardRange, limit = 20) =>
  queryOptions({
    queryKey: ['marketplace', 'leaderboard', 'vendors', range, limit],
    queryFn: () =>
      api.get<VendorLeaderboard>('/marketplace/leaderboard/vendors', {
        query: { range, limit },
      }),
    staleTime: CACHE_TTL,
    refetchInterval: CACHE_TTL,
  })

export const leaderboardTrendQuery = (days = 30, model?: string) =>
  queryOptions({
    queryKey: ['marketplace', 'leaderboard', 'trend', days, model ?? null],
    queryFn: () =>
      api.get<LeaderboardTrend>('/marketplace/leaderboard/trend', {
        query: { days, ...(model ? { model } : {}) },
      }),
    staleTime: CACHE_TTL,
    refetchInterval: CACHE_TTL,
  })
