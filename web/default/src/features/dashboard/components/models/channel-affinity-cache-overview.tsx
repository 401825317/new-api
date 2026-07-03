/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, type ComponentType } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Gauge, Hash, Network, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatCompactNumber } from '@/lib/format'
import { computeTimeRange } from '@/lib/time'
import { cn } from '@/lib/utils'
import { Skeleton } from '@/components/ui/skeleton'
import { getPromptCacheUsageSummary } from '@/features/dashboard/api'
import { getDefaultDays } from '@/features/dashboard/lib'
import type {
  DashboardFilters,
  PromptCacheUsageAggregate,
  PromptCacheUsageSummary,
} from '@/features/dashboard/types'

const TOP_MODEL_LIMIT = 6
const TOP_CHANNEL_LIMIT = 4

function formatRate(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return `${(value * 100).toFixed(1)}%`
}

function getRateTextClass(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value)) return 'text-muted-foreground'
  if (value >= 0.6) return 'text-success'
  if (value >= 0.2) return 'text-warning'
  return 'text-muted-foreground'
}

function tokenCacheRate(summary?: PromptCacheUsageSummary): string {
  if (!summary?.token_cache_rate_available) return '-'
  return formatRate(summary.token_cache_rate)
}

function requestHitValue(summary: PromptCacheUsageSummary): string {
  return `${formatCompactNumber(summary.hit_requests)}/${formatCompactNumber(summary.total_requests)}`
}

interface ChannelAffinityCacheOverviewProps {
  filters?: DashboardFilters
}

export function ChannelAffinityCacheOverview(
  props: ChannelAffinityCacheOverviewProps
) {
  const { t } = useTranslation()
  const filters = props.filters
  const timeRange = useMemo(
    () =>
      computeTimeRange(
        getDefaultDays(filters?.time_granularity),
        filters?.start_timestamp,
        filters?.end_timestamp
      ),
    [
      filters?.end_timestamp,
      filters?.start_timestamp,
      filters?.time_granularity,
    ]
  )

  const statsQuery = useQuery({
    queryKey: [
      'dashboard',
      'prompt-cache-usage-summary',
      timeRange.start_timestamp,
      timeRange.end_timestamp,
      filters?.username,
    ],
    queryFn: () =>
      getPromptCacheUsageSummary({
        startTimestamp: timeRange.start_timestamp,
        endTimestamp: timeRange.end_timestamp,
        username: filters?.username,
        limit: 12,
      }),
    staleTime: 30 * 1000,
    retry: false,
  })

  const summary = statsQuery.data?.data
  const topModels = useMemo(
    () => (summary?.by_model ?? []).slice(0, TOP_MODEL_LIMIT),
    [summary?.by_model]
  )
  const topChannels = useMemo(
    () => (summary?.by_channel ?? []).slice(0, TOP_CHANNEL_LIMIT),
    [summary?.by_channel]
  )
  const hasData = !!summary && summary.total_requests > 0

  if (statsQuery.isLoading) {
    return <ChannelAffinityCacheSkeleton />
  }

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex flex-wrap items-center gap-x-5 gap-y-2.5 px-4 py-2.5 sm:px-5 sm:py-3'>
        <div className='flex items-center gap-1.5'>
          <Gauge
            className='text-muted-foreground/60 size-3.5 shrink-0'
            aria-hidden='true'
          />
          <span className='text-xs font-semibold whitespace-nowrap'>
            {t('Prompt cache hit rate')}
          </span>
        </div>

        <div className='bg-border hidden h-4 w-px sm:block' />

        <div className='flex flex-wrap items-center gap-x-5 gap-y-2'>
          <InlineMetric
            icon={Gauge}
            label={t('Request hit rate')}
            value={hasData ? formatRate(summary.request_hit_rate) : '-'}
            valueClassName={
              hasData ? getRateTextClass(summary.request_hit_rate) : undefined
            }
          />
          <InlineMetric
            icon={Network}
            label={t('Token cache rate')}
            value={hasData ? tokenCacheRate(summary) : '-'}
            valueClassName={
              hasData && summary.token_cache_rate_available
                ? getRateTextClass(summary.token_cache_rate)
                : undefined
            }
          />
          <InlineMetric
            icon={Route}
            label={t('Requests')}
            value={hasData ? requestHitValue(summary) : '0/0'}
          />
          <InlineMetric
            icon={Hash}
            label={t('Cache Read')}
            value={hasData ? formatCompactNumber(summary.cached_tokens) : '0'}
          />
        </div>

        {!hasData && (
          <>
            <div className='bg-border hidden h-4 w-px md:block' />
            <span className='text-muted-foreground text-xs'>
              {statsQuery.isError
                ? t('Failed to load prompt cache hit data')
                : t('No prompt cache usage in current window')}
            </span>
          </>
        )}

        {hasData && topModels.length > 0 && (
          <>
            <div className='bg-border hidden h-4 w-px lg:block' />
            <BadgeGroup
              items={topModels}
              getLabel={(item) => item.model}
              prefix={t('Models')}
            />
          </>
        )}

        {hasData && topChannels.length > 0 && (
          <>
            <div className='bg-border hidden h-4 w-px xl:block' />
            <BadgeGroup
              items={topChannels}
              getLabel={(item) => `#${item.channel_id}`}
              prefix={t('Channels')}
            />
          </>
        )}
      </div>
    </div>
  )
}

function ChannelAffinityCacheSkeleton() {
  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3 sm:px-5'>
        <Skeleton className='h-4 w-28' />
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className='flex items-center gap-1.5'>
            <Skeleton className='h-3 w-16' />
            <Skeleton className='h-4 w-14' />
          </div>
        ))}
        <div className='ml-auto flex items-center gap-2'>
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className='h-5 w-28 rounded-full' />
          ))}
        </div>
      </div>
    </div>
  )
}

function InlineMetric(props: {
  icon: ComponentType<{ className?: string }>
  label: string
  value: string
  valueClassName?: string
}) {
  const Icon = props.icon

  return (
    <div className='flex items-center gap-1.5'>
      <Icon
        className='text-muted-foreground/50 size-3 shrink-0'
        aria-hidden='true'
      />
      <span className='text-muted-foreground text-[11px]'>{props.label}</span>
      <span
        className={cn(
          'font-mono text-xs font-semibold tabular-nums',
          props.valueClassName
        )}
      >
        {props.value}
      </span>
    </div>
  )
}

function BadgeGroup(props: {
  items: PromptCacheUsageAggregate[]
  prefix: string
  getLabel: (item: PromptCacheUsageAggregate) => string
}) {
  return (
    <div className='flex flex-wrap items-center gap-1.5'>
      <span className='text-muted-foreground mr-0.5 text-[11px]'>
        {props.prefix}
      </span>
      {props.items.map((item) => {
        const label = props.getLabel(item)
        return (
          <span
            key={`${props.prefix}:${label}`}
            className='bg-muted/50 inline-flex max-w-56 items-center gap-1.5 rounded-full px-2.5 py-1'
          >
            <span className='truncate font-mono text-[11px]' title={label}>
              {label}
            </span>
            <span
              className={cn(
                'font-mono text-[11px] font-semibold tabular-nums',
                getRateTextClass(item.request_hit_rate)
              )}
            >
              {formatRate(item.request_hit_rate)}
            </span>
          </span>
        )
      })}
    </div>
  )
}
