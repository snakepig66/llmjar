import { useQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'

import { copy } from '@/copy'
import type {
  BuyerLeaderboardItem,
  LeaderboardRange,
  LeaderboardTrend,
  ModelLeaderboardItem,
  SellerLeaderboardItem,
  VendorLeaderboardItem,
} from '@/features/leaderboard/api'
import {
  leaderboardBuyersQuery,
  leaderboardModelsQuery,
  leaderboardSellersQuery,
  leaderboardTrendQuery,
  leaderboardVendorsQuery,
} from '@/features/leaderboard/api'
import { formatCount } from '@/features/market/format'
import { QueryError } from '@/features/market/query-error'
import { cn } from '@/lib/cn'
import { formatYuan } from '@/lib/money'
import { Empty, ModelName, PageHeader } from '@/ui'

interface LeaderboardSearch {
  range?: LeaderboardRange
}

const validateSearch = (
  search: Record<string, unknown>,
): LeaderboardSearch => {
  if (search.range === '7d' || search.range === '24h' || search.range === '30d') {
    return { range: search.range }
  }
  return {}
}

export const Route = createFileRoute('/leaderboard')({
  validateSearch,
  component: LeaderboardPage,
})

const page = copy.pages.leaderboard

const RANGES: readonly LeaderboardRange[] = ['24h', '7d', '30d']

const RANGE_LABELS: Record<LeaderboardRange, string> = {
  '24h': page.range24h,
  '7d': page.range7d,
  '30d': page.range30d,
}

const RANGE_LEADS: Record<LeaderboardRange, string> = {
  '24h': page.lead24h,
  '7d': page.lead7d,
  '30d': page.lead30d,
}

/** Thousandths of a percent, e.g. 995 -> "99.5%"; no sample -> 暂无数据. */
function formatRate(milli: number | null | undefined): string {
  if (milli === null || milli === undefined || !Number.isFinite(milli) || milli < 0) {
    return page.noData
  }
  return `${(milli / 10).toFixed(1)}%`
}

function hasRate(milli: number | null | undefined): boolean {
  return milli !== null && milli !== undefined && Number.isFinite(milli) && milli >= 0
}

/** Under one second stays in ms; longer durations switch to seconds. */
function formatLatency(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms) || ms < 0) {
    return page.noData
  }
  return ms < 1000 ? `${Math.round(ms)}ms` : `${(ms / 1000).toFixed(1)}s`
}

function rankTone(rank: number): string {
  return rank <= 3 ? 'font-semibold text-brand' : 'text-ink-3'
}

function Board({
  title,
  hint,
  children,
}: {
  title: string
  hint: string
  children: React.ReactNode
}) {
  return (
    <section className="min-w-0 glass p-4 sm:p-6">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 border-b border-line pb-3">
        <h2 className="text-title font-semibold tracking-tight text-ink">{title}</h2>
        <span className="text-caption text-ink-3">{hint}</span>
      </div>
      {children}
    </section>
  )
}

function Loading() {
  return <p className="py-10 text-center text-meta text-ink-3">{page.loading}</p>
}

function ModelBoard({
  items,
  pending,
  failed,
  retrying,
  onRetry,
}: {
  items: ModelLeaderboardItem[]
  pending: boolean
  failed: boolean
  retrying: boolean
  onRetry: () => void
}) {
  return (
    <Board title={page.models.title} hint={page.models.hint}>
      {pending ? (
        <Loading />
      ) : failed ? (
        <QueryError
          title={page.models.errorTitle}
          reason={page.models.errorReason}
          onRetry={onRetry}
          retrying={retrying}
        />
      ) : items.length === 0 ? (
        <Empty title={page.models.emptyTitle} reason={page.models.emptyReason} />
      ) : (
        <>
          <div className="mt-2 flex items-baseline gap-x-3 border-b border-line pb-1.5 text-caption text-ink-3">
            <span className="w-6 shrink-0 text-center">{page.models.rank}</span>
            <span className="min-w-0 flex-1">{page.models.model}</span>
            <span className="shrink-0">{page.models.tokens}</span>
          </div>
          <ul>
            {items.map((item) => (
              <li
                key={`${item.rank}-${item.model}`}
                className="border-b border-line py-3 last:border-b-0"
              >
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span
                    className={cn(
                      'w-6 shrink-0 text-center text-body tabular-nums',
                      rankTone(item.rank),
                    )}
                  >
                    {item.rank}
                  </span>
                  <span
                    className="min-w-0 flex-1 truncate text-body font-medium text-ink"
                    title={item.model}
                  >
                    <ModelName model={item.model} size="sm" />
                  </span>
                  <span className="shrink-0 whitespace-nowrap text-caption text-ink-3">
                    {page.models.tokens}{' '}
                    <span className="text-meta tabular-nums text-ink-2">
                      {formatCount(item.tokens)}
                    </span>
                  </span>
                </div>
                <dl className="mt-1 flex flex-wrap gap-x-4 gap-y-1 pl-9 text-caption text-ink-3">
                  <div className="flex items-baseline gap-1">
                    <dt>{page.models.requests}</dt>
                    <dd className="tabular-nums text-ink-2">{formatCount(item.requests)}</dd>
                  </div>
                  <div className="flex items-baseline gap-1">
                    <dt>{page.models.success}</dt>
                    <dd
                      className={cn(
                        'tabular-nums',
                        hasRate(item.success_rate_milli) ? 'text-ink-2' : 'text-ink-3',
                      )}
                    >
                      {formatRate(item.success_rate_milli)}
                    </dd>
                  </div>
                  <div className="flex items-baseline gap-1">
                    <dt>{page.models.latency}</dt>
                    <dd className="tabular-nums text-ink-2">
                      {formatLatency(item.avg_latency_ms)}
                    </dd>
                  </div>
                </dl>
              </li>
            ))}
          </ul>
        </>
      )}
    </Board>
  )
}

function SellerBoard({
  items,
  pending,
  failed,
  retrying,
  onRetry,
}: {
  items: SellerLeaderboardItem[]
  pending: boolean
  failed: boolean
  retrying: boolean
  onRetry: () => void
}) {
  return (
    <Board title={page.sellers.title} hint={page.sellers.hint}>
      {pending ? (
        <Loading />
      ) : failed ? (
        <QueryError
          title={page.sellers.errorTitle}
          reason={page.sellers.errorReason}
          onRetry={onRetry}
          retrying={retrying}
        />
      ) : items.length === 0 ? (
        <Empty title={page.sellers.emptyTitle} reason={page.sellers.emptyReason} />
      ) : (
        <>
          <div className="mt-2 flex items-baseline gap-x-3 border-b border-line pb-1.5 text-caption text-ink-3">
            <span className="w-6 shrink-0 text-center">{page.sellers.rank}</span>
            <span className="min-w-0 flex-1">{page.sellers.alias}</span>
            <span className="shrink-0">{page.sellers.income}</span>
          </div>
          <ul>
            {items.map((item) => (
              <li
                key={`${item.rank}-${item.alias}`}
                className="border-b border-line py-3 last:border-b-0"
              >
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span
                    className={cn(
                      'w-6 shrink-0 text-center text-body tabular-nums',
                      rankTone(item.rank),
                    )}
                  >
                    {item.rank}
                  </span>
                  <span
                    className="min-w-0 flex-1 truncate text-body font-medium text-ink"
                    title={item.alias}
                  >
                    {item.alias}
                  </span>
                  <span className="shrink-0 whitespace-nowrap text-caption text-ink-3">
                    {page.sellers.income}{' '}
                    <span className="text-meta tabular-nums text-ink">
                      {formatYuan(item.income_micro)}
                    </span>
                  </span>
                </div>
                <dl className="mt-1 flex flex-wrap gap-x-4 gap-y-1 pl-9 text-caption text-ink-3">
                  <div className="flex items-baseline gap-1">
                    <dt>{page.sellers.requests}</dt>
                    <dd className="tabular-nums text-ink-2">{formatCount(item.requests)}</dd>
                  </div>
                  <div className="flex items-baseline gap-1">
                    <dt>{page.sellers.success}</dt>
                    <dd
                      className={cn(
                        'tabular-nums',
                        hasRate(item.success_rate_milli) ? 'text-ink-2' : 'text-ink-3',
                      )}
                    >
                      {formatRate(item.success_rate_milli)}
                    </dd>
                  </div>
                </dl>
              </li>
            ))}
          </ul>
        </>
      )}
    </Board>
  )
}

/** Thin underline progress bar under a board row, percent of the top row. */
function ShareBar({ percent, color }: { percent: number; color?: string }) {
  return (
    <div className="mt-1.5 h-[3px] w-full overflow-hidden rounded-full bg-bg-soft">
      <div
        className="h-full rounded-full bg-brand"
        style={{
          width: `${Math.min(100, Math.max(0, percent))}%`,
          ...(color ? { backgroundColor: color } : {}),
        }}
      />
    </div>
  )
}

function BuyerBoard({
  items,
  pending,
  failed,
  retrying,
  onRetry,
}: {
  items: BuyerLeaderboardItem[]
  pending: boolean
  failed: boolean
  retrying: boolean
  onRetry: () => void
}) {
  const maxTokens = Math.max(1, ...items.map((item) => item.tokens))
  return (
    <Board title={page.buyers.title} hint={page.buyers.hint}>
      {pending ? (
        <Loading />
      ) : failed ? (
        <QueryError
          title={page.buyers.errorTitle}
          reason={page.buyers.errorReason}
          onRetry={onRetry}
          retrying={retrying}
        />
      ) : items.length === 0 ? (
        <Empty title={page.buyers.emptyTitle} reason={page.buyers.emptyReason} />
      ) : (
        <>
          <div className="mt-2 flex items-baseline gap-x-3 border-b border-line pb-1.5 text-caption text-ink-3">
            <span className="w-6 shrink-0 text-center">{page.buyers.rank}</span>
            <span className="min-w-0 flex-1">{page.buyers.alias}</span>
            <span className="shrink-0">{page.buyers.tokens}</span>
          </div>
          <ul>
            {items.map((item) => (
              <li
                key={`${item.rank}-${item.alias}`}
                className="border-b border-line py-3 last:border-b-0"
              >
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span
                    className={cn(
                      'w-6 shrink-0 text-center text-body tabular-nums',
                      rankTone(item.rank),
                    )}
                  >
                    {item.rank}
                  </span>
                  <span
                    className="min-w-0 flex-1 truncate text-body font-medium text-ink"
                    title={item.alias}
                  >
                    {item.alias}
                  </span>
                  <span className="shrink-0 whitespace-nowrap text-caption text-ink-3">
                    {page.buyers.tokens}{' '}
                    <span className="text-meta tabular-nums text-ink">
                      {formatCount(item.tokens)}
                    </span>
                  </span>
                </div>
                <dl className="mt-1 flex flex-wrap gap-x-4 gap-y-1 pl-9 text-caption text-ink-3">
                  <div className="flex items-baseline gap-1">
                    <dt>{page.buyers.requests}</dt>
                    <dd className="tabular-nums text-ink-2">{formatCount(item.requests)}</dd>
                  </div>
                  <div className="flex items-baseline gap-1">
                    <dt>{page.buyers.success}</dt>
                    <dd
                      className={cn(
                        'tabular-nums',
                        hasRate(item.success_rate_milli) ? 'text-ink-2' : 'text-ink-3',
                      )}
                    >
                      {formatRate(item.success_rate_milli)}
                    </dd>
                  </div>
                </dl>
                <div className="pl-9">
                  <ShareBar percent={(item.tokens / maxTokens) * 100} />
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
    </Board>
  )
}

/** Board colors for the vendor share strip, cycling by rank. */
const VENDOR_COLORS = ['#4d6bfe', '#6a8bff', '#2f4bd8', '#16a34a', '#f59e0b', '#38bdf8', '#94a3c8']

function vendorColor(rank: number): string {
  return VENDOR_COLORS[(rank - 1) % VENDOR_COLORS.length]
}

function VendorBoard({
  items,
  totalTokens,
  pending,
  failed,
  retrying,
  onRetry,
}: {
  items: VendorLeaderboardItem[]
  totalTokens: number
  pending: boolean
  failed: boolean
  retrying: boolean
  onRetry: () => void
}) {
  return (
    <Board title={page.vendors.title} hint={page.vendors.hint}>
      {pending ? (
        <Loading />
      ) : failed ? (
        <QueryError
          title={page.vendors.errorTitle}
          reason={page.vendors.errorReason}
          onRetry={onRetry}
          retrying={retrying}
        />
      ) : items.length === 0 ? (
        <Empty title={page.vendors.emptyTitle} reason={page.vendors.emptyReason} />
      ) : (
        <>
          <div className="mt-3 flex items-baseline justify-end gap-1.5">
            <span className="text-h3 font-semibold tabular-nums text-ink">
              {formatCount(totalTokens)}
            </span>
            <span className="text-caption text-ink-3">{page.vendors.tokens}</span>
          </div>
          <div
            className="mt-2 flex h-2 w-full overflow-hidden rounded-full bg-bg-soft"
            role="img"
            aria-label={page.vendors.title}
          >
            {items.map((item) => (
              <div
                key={item.vendor}
                className="h-full"
                style={{
                  width: `${item.share_milli / 10}%`,
                  backgroundColor: vendorColor(item.rank),
                }}
                title={`${item.display_name || page.vendors.custom} ${(item.share_milli / 10).toFixed(1)}%`}
              />
            ))}
          </div>
          <div className="mt-2 flex items-baseline gap-x-3 border-b border-line pb-1.5 pt-2 text-caption text-ink-3">
            <span className="w-6 shrink-0 text-center">{page.vendors.rank}</span>
            <span className="min-w-0 flex-1">{page.vendors.vendor}</span>
            <span className="shrink-0">{page.vendors.tokens}</span>
          </div>
          <ul>
            {items.map((item) => (
              <li
                key={item.vendor}
                className="border-b border-line py-3 last:border-b-0"
              >
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span
                    className={cn(
                      'w-6 shrink-0 text-center text-body tabular-nums',
                      rankTone(item.rank),
                    )}
                  >
                    {item.rank}
                  </span>
                  <span className="flex min-w-0 flex-1 items-center gap-2">
                    <span
                      aria-hidden="true"
                      className="h-2 w-2 shrink-0 rounded-full"
                      style={{ backgroundColor: vendorColor(item.rank) }}
                    />
                    <span
                      className="truncate text-body font-medium text-ink"
                      title={item.vendor}
                    >
                      {item.vendor === 'custom'
                        ? page.vendors.custom
                        : item.display_name || item.vendor}
                    </span>
                  </span>
                  <span className="shrink-0 whitespace-nowrap text-caption text-ink-3">
                    {page.vendors.tokens}{' '}
                    <span className="text-meta tabular-nums text-ink">
                      {formatCount(item.tokens)}
                    </span>
                  </span>
                  <span className="shrink-0 whitespace-nowrap text-meta tabular-nums text-ink-2">
                    {(item.share_milli / 10).toFixed(1)}%
                  </span>
                </div>
                <dl className="mt-1 flex flex-wrap gap-x-4 gap-y-1 pl-9 text-caption text-ink-3">
                  <div className="flex items-baseline gap-1">
                    <dt>{page.vendors.requests}</dt>
                    <dd className="tabular-nums text-ink-2">{formatCount(item.requests)}</dd>
                  </div>
                  <div className="flex items-baseline gap-1">
                    <dt>{page.vendors.models}</dt>
                    <dd className="tabular-nums text-ink-2">{formatCount(item.models)}</dd>
                  </div>
                </dl>
                <div className="pl-9">
                  <ShareBar
                    percent={item.share_milli / 10}
                    color={vendorColor(item.rank)}
                  />
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
    </Board>
  )
}

function TrendChart({ trend }: { trend: LeaderboardTrend }) {
  const items = trend.items
  const maxRequests = Math.max(1, ...items.map((item) => item.requests))
  const start = trend.start ?? items[0]?.date ?? ''
  const end = trend.end ?? items[items.length - 1]?.date ?? ''

  return (
    <>
      <div
        className="mt-4 flex h-[160px] items-end gap-[2px] border-b border-line sm:gap-1"
        role="img"
        aria-label={page.trend.aria}
      >
        {items.map((item) => {
          const height =
            item.requests > 0 ? Math.max(2, (item.requests / maxRequests) * 100) : 0
          return (
            <div
              key={item.date}
              className="flex h-full min-w-0 flex-1 items-end"
              title={page.trend.barTitle(item.date, formatCount(item.requests))}
            >
              <div
                className="w-full rounded-t-[2px] bg-brand"
                style={{ height: `${height}%` }}
              />
            </div>
          )
        })}
      </div>
      <p className="mt-3 text-caption tabular-nums text-ink-3">
        {page.trend.range(start, end)}
      </p>
    </>
  )
}

function TrendSection({
  data,
  pending,
  failed,
  retrying,
  onRetry,
}: {
  data: LeaderboardTrend | undefined
  pending: boolean
  failed: boolean
  retrying: boolean
  onRetry: () => void
}) {
  const items = data?.items ?? []
  const hasTraffic = items.some((item) => item.requests > 0)

  return (
    <Board title={page.trend.title} hint={page.trend.hint}>
      {pending ? (
        <Loading />
      ) : failed ? (
        <QueryError
          title={page.trend.errorTitle}
          reason={page.trend.errorReason}
          onRetry={onRetry}
          retrying={retrying}
        />
      ) : !data || items.length === 0 || !hasTraffic ? (
        <Empty title={page.trend.emptyTitle} reason={page.trend.emptyReason} />
      ) : (
        <TrendChart trend={data} />
      )}
    </Board>
  )
}

export function LeaderboardPage() {
  const navigate = useNavigate()
  const search = Route.useSearch()
  const range: LeaderboardRange = search.range ?? '24h'

  const models = useQuery(leaderboardModelsQuery(range))
  const sellers = useQuery(leaderboardSellersQuery(range))
  const buyers = useQuery(leaderboardBuyersQuery(range))
  const vendors = useQuery(leaderboardVendorsQuery(range))
  const trend = useQuery(leaderboardTrendQuery(30))

  function selectRange(next: LeaderboardRange) {
    if (next === range) return
    void navigate({
      to: '/leaderboard',
      search: { range: next },
      replace: true,
      resetScroll: false,
    })
  }

  return (
    <div className="mx-auto w-full max-w-[1200px] px-5 py-12 sm:px-6 sm:py-16">
      <PageHeader
        title={page.title}
        lead={RANGE_LEADS[range]}
        actions={
          <div
            role="tablist"
            aria-label={page.rangeAria}
            className="inline-flex glass-inset p-1"
          >
            {RANGES.map((value) => (
              <button
                key={value}
                type="button"
                role="tab"
                aria-selected={range === value}
                onClick={() => selectRange(value)}
                className={
                  range === value
                    ? 'rounded-md bg-white/90 px-3 py-1.5 text-meta font-medium text-brand-ink shadow-2xs dark:bg-slate-800 dark:text-white'
                    : 'rounded-md px-3 py-1.5 text-meta text-ink-2 transition-colors hover:text-ink'
                }
              >
                {RANGE_LABELS[value]}
              </button>
            ))}
          </div>
        }
      />

      <div className="mt-6 grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ModelBoard
          items={models.data?.items ?? []}
          pending={models.isPending}
          failed={models.isError}
          retrying={models.isFetching}
          onRetry={() => void models.refetch()}
        />
        <VendorBoard
          items={vendors.data?.items ?? []}
          totalTokens={vendors.data?.total_tokens ?? 0}
          pending={vendors.isPending}
          failed={vendors.isError}
          retrying={vendors.isFetching}
          onRetry={() => void vendors.refetch()}
        />
        <SellerBoard
          items={sellers.data?.items ?? []}
          pending={sellers.isPending}
          failed={sellers.isError}
          retrying={sellers.isFetching}
          onRetry={() => void sellers.refetch()}
        />
        <BuyerBoard
          items={buyers.data?.items ?? []}
          pending={buyers.isPending}
          failed={buyers.isError}
          retrying={buyers.isFetching}
          onRetry={() => void buyers.refetch()}
        />
      </div>

      <div className="mt-4">
        <TrendSection
          data={trend.data}
          pending={trend.isPending}
          failed={trend.isError}
          retrying={trend.isFetching}
          onRetry={() => void trend.refetch()}
        />
      </div>
    </div>
  )
}
