/**
 * 运营后台 · 渠道用量。
 *
 * 只读页面：按时间范围聚合消费日志，展示各渠道 token 消耗、请求数与占比。
 * 数据来自 /option/channel_token_stats，前端不做任何写操作。
 */

import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useMemo, useState } from 'react'

import { copy } from '@/copy'
import {
  Empty,
  PageHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeaderCell,
  TableRow,
} from '@/ui'

import {
  channelTokenStatsQuery,
  type ChannelTokenStats,
  type ChannelTokenStatsRow,
} from '@/features/admin/settings-api'
import {
  ErrorBlock,
  FilterTabs,
  OpsTabNav,
  PageSkeleton,
  RootOnly,
  Section,
  type OpsTabLink,
} from '@/features/admin/components'

export const Route = createFileRoute('/console/ops/metrics')({
  component: MetricsPage,
})

const page = copy.pages.adminMetrics

type RangeKey = keyof typeof page.ranges

const RANGE_HOURS: Record<RangeKey, number> = {
  '1h': 1,
  '24h': 24,
  '7d': 24 * 7,
}

function formatTokens(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`
  return String(value)
}

function formatCount(value: number): string {
  return value.toLocaleString('zh-CN')
}

function MetricsPage() {
  return (
    <RootOnly>
      <MetricsContent />
    </RootOnly>
  )
}

const tabs: readonly OpsTabLink[] = [
  { to: '/console/ops/metrics', label: '渠道用量' },
  { to: '/console/ops/trace', label: '全链路排查' },
]

export function MetricsContent() {
  const [range, setRange] = useState<RangeKey>('24h')
  const hours = RANGE_HOURS[range]
  const stats = useQuery(channelTokenStatsQuery(hours))

  const rangeOptions = useMemo(
    () =>
      (Object.keys(RANGE_HOURS) as RangeKey[]).map((value) => ({
        value,
        label: page.ranges[value],
      })),
    [],
  )

  const data = stats.data ?? null
  const items = data?.channels ?? []

  return (
    <>
      <OpsTabNav tabs={tabs} ariaLabel="监控排查导航" />
      <PageHeader title={page.title} lead={page.lead} />

      <div className="mt-6 flex flex-wrap items-start gap-x-6 gap-y-4">
        <div className="min-w-0">
          <span className="mb-2 block text-body font-medium text-ink">
            {page.filters.range}
          </span>
          <FilterTabs
            options={rangeOptions}
            value={range}
            onChange={setRange}
            ariaLabel={page.filters.aria}
          />
        </div>
      </div>

      <div className="mt-6">
        {stats.isPending ? (
          <PageSkeleton rows={6} />
        ) : stats.isError ? (
          <ErrorBlock error={stats.error} onRetry={() => void stats.refetch()} />
        ) : items.length === 0 ? (
          <Empty
            title={page.empty.title}
            reason={page.empty.reason}
            next={page.empty.next}
          />
        ) : (
          <Section title={page.title} hint={items.length}>
            <ChannelTable rows={items} total={data!.total} />
          </Section>
        )}
      </div>
    </>
  )
}

function ChannelTable({
  rows,
  total,
}: {
  rows: ChannelTokenStatsRow[]
  total: ChannelTokenStats['total']
}) {
  return (
    <Table minWidth={760} size="compact">
      <TableHead>
        <TableRow interactive={false}>
          <TableHeaderCell>{page.columns.channel}</TableHeaderCell>
          <TableHeaderCell numeric>{page.columns.requests}</TableHeaderCell>
          <TableHeaderCell numeric>{page.columns.prompt}</TableHeaderCell>
          <TableHeaderCell numeric>{page.columns.completion}</TableHeaderCell>
          <TableHeaderCell numeric>{page.columns.total}</TableHeaderCell>
          <TableHeaderCell>{page.columns.share}</TableHeaderCell>
        </TableRow>
      </TableHead>
      <TableBody>
        <TableRow interactive={false} className="bg-bg-subtle">
          <TableCell>
            <span className="font-semibold text-ink">{page.totalRow}</span>
          </TableCell>
          <TableCell numeric className="font-semibold tabular-nums">
            {formatCount(total.requests)}
          </TableCell>
          <TableCell numeric className="font-semibold tabular-nums">
            {formatTokens(total.prompt_tokens)}
          </TableCell>
          <TableCell numeric className="font-semibold tabular-nums">
            {formatTokens(total.completion_tokens)}
          </TableCell>
          <TableCell numeric className="font-semibold tabular-nums">
            {formatTokens(total.total_tokens)}
          </TableCell>
          <TableCell />
        </TableRow>
        {rows.map((row) => (
          <TableRow key={row.channel_id}>
            <TableCell>
              <span className="font-medium text-ink">
                {row.channel_name || `#${row.channel_id}`}
              </span>
              <span className="ml-2 font-mono text-caption text-ink-3">
                #{row.channel_id}
              </span>
            </TableCell>
            <TableCell numeric className="tabular-nums">
              {formatCount(row.requests)}
            </TableCell>
            <TableCell numeric className="tabular-nums">
              {formatTokens(row.prompt_tokens)}
            </TableCell>
            <TableCell numeric className="tabular-nums">
              {formatTokens(row.completion_tokens)}
            </TableCell>
            <TableCell numeric className="tabular-nums font-medium text-ink">
              {formatTokens(row.total_tokens)}
            </TableCell>
            <TableCell className="w-48">
              <div className="flex items-center gap-2">
                <div className="h-1.5 flex-1 rounded-full bg-line/40">
                  <div
                    className="h-1.5 rounded-full bg-brand"
                    style={{ width: `${Math.max(row.token_share * 100, 1)}%` }}
                  />
                </div>
                <span className="w-12 shrink-0 text-right tabular-nums text-meta text-ink-2">
                  {(row.token_share * 100).toFixed(1)}%
                </span>
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
