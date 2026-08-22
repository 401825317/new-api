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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { StatusBadge } from '@/components/status-badge'
import { getUClawVersionUsageStats } from '../api'

const ranges = [
  { value: '24', labelKey: '24 hours' },
  { value: '168', labelKey: '7 days' },
  { value: '720', labelKey: '30 days' },
] as const

function formatRate(value: number): string {
  return new Intl.NumberFormat(undefined, {
    style: 'percent',
    maximumFractionDigits: 1,
  }).format(value)
}

function formatLatency(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '-'
  if (value >= 1000) return `${(value / 1000).toFixed(2)} s`
  return `${Math.round(value)} ms`
}

export function UClawVersionStats() {
  const { t } = useTranslation()
  const [rangeHours, setRangeHours] = useState('24')
  const query = useQuery({
    queryKey: ['uclaw-version-usage-stats', rangeHours],
    queryFn: () => {
      const endTimestamp = Math.floor(Date.now() / 1000)
      const startTimestamp = endTimestamp - Number(rangeHours) * 60 * 60
      return getUClawVersionUsageStats(startTimestamp, endTimestamp)
    },
    refetchInterval: 60_000,
  })
  const summary = query.data?.data
  const successCount =
    summary?.items.reduce((total, item) => total + item.success_count, 0) ?? 0
  const errorCount =
    summary?.items.reduce((total, item) => total + item.error_count, 0) ?? 0
  const versionCount = new Set(summary?.items.map((item) => item.version) ?? [])
    .size
  const overallSuccessRate =
    summary && summary.total_requests > 0
      ? successCount / summary.total_requests
      : 0
  const overallErrorRate =
    summary && summary.total_requests > 0
      ? errorCount / summary.total_requests
      : 0
  const dataStatus = query.isLoading
    ? { label: t('Loading...'), variant: 'neutral' as const }
    : query.isError
      ? { label: t('Loading failed'), variant: 'danger' as const }
      : summary?.truncated
        ? { label: t('Truncated'), variant: 'warning' as const }
        : { label: t('Complete'), variant: 'success' as const }

  return (
    <div className='flex h-full min-h-0 flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Tabs value={rangeHours} onValueChange={setRangeHours}>
          <TabsList>
            {ranges.map((range) => (
              <TabsTrigger key={range.value} value={range.value}>
                {t(range.labelKey)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Button
          type='button'
          size='icon'
          variant='outline'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
          title={t('Refresh')}
          aria-label={t('Refresh')}
        >
          <RefreshCw
            className={`size-4 ${query.isFetching ? 'animate-spin' : ''}`}
          />
        </Button>
      </div>

      <div className='grid grid-cols-2 gap-3 border-y py-4 sm:grid-cols-3 lg:grid-cols-5'>
        <div>
          <div className='text-muted-foreground text-xs'>{t('Requests')}</div>
          <div className='mt-1 text-lg font-semibold tabular-nums'>
            {(summary?.total_requests ?? 0).toLocaleString()}
          </div>
        </div>
        <div>
          <div className='text-muted-foreground text-xs'>{t('Versions')}</div>
          <div className='mt-1 text-lg font-semibold tabular-nums'>
            {versionCount}
          </div>
        </div>
        <div>
          <div className='text-muted-foreground text-xs'>
            {t('Success rate')}
          </div>
          <div className='mt-1 text-lg font-semibold tabular-nums'>
            {formatRate(overallSuccessRate)}
          </div>
        </div>
        <div>
          <div className='text-muted-foreground text-xs'>{t('Error rate')}</div>
          <div className='mt-1 text-lg font-semibold text-rose-600 tabular-nums'>
            {formatRate(overallErrorRate)}
          </div>
        </div>
        <div>
          <div className='text-muted-foreground text-xs'>
            {t('Data status')}
          </div>
          <div className='mt-1'>
            <StatusBadge
              label={dataStatus.label}
              variant={dataStatus.variant}
              copyable={false}
            />
          </div>
        </div>
      </div>

      <div className='min-h-0 flex-1 overflow-auto rounded-md border'>
        <Table>
          <TableHeader className='bg-muted/50 sticky top-0 z-10'>
            <TableRow>
              <TableHead>{t('Version')}</TableHead>
              <TableHead>{t('Commit')}</TableHead>
              <TableHead>{t('Build ID')}</TableHead>
              <TableHead>{t('Channel')}</TableHead>
              <TableHead>{t('Mode')}</TableHead>
              <TableHead className='text-right'>{t('Requests')}</TableHead>
              <TableHead className='text-right'>{t('Success rate')}</TableHead>
              <TableHead className='text-right'>{t('Error rate')}</TableHead>
              <TableHead className='text-right'>
                {t('Average latency')}
              </TableHead>
              <TableHead className='text-right'>{t('P95 latency')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.isLoading && (
              <TableRow>
                <TableCell
                  colSpan={10}
                  className='text-muted-foreground h-32 text-center'
                >
                  {t('Loading...')}
                </TableCell>
              </TableRow>
            )}
            {summary?.items.map((item) => (
              <TableRow
                key={`${item.version}:${item.commit}:${item.build_id}:${item.channel}:${item.mode}`}
              >
                <TableCell className='font-medium'>{item.version}</TableCell>
                <TableCell className='max-w-36 truncate font-mono text-xs'>
                  {item.commit || '-'}
                </TableCell>
                <TableCell className='max-w-44 truncate font-mono text-xs'>
                  {item.build_id || '-'}
                </TableCell>
                <TableCell>{item.channel || '-'}</TableCell>
                <TableCell>{item.mode || '-'}</TableCell>
                <TableCell className='text-right'>
                  {item.request_count}
                </TableCell>
                <TableCell className='text-right text-emerald-600'>
                  {formatRate(item.success_rate)}
                </TableCell>
                <TableCell className='text-right text-rose-600'>
                  {formatRate(item.error_rate)}
                </TableCell>
                <TableCell className='text-right'>
                  {formatLatency(item.average_latency_ms)}
                </TableCell>
                <TableCell className='text-right'>
                  {formatLatency(item.p95_latency_ms)}
                </TableCell>
              </TableRow>
            ))}
            {!query.isLoading && (summary?.items.length ?? 0) === 0 && (
              <TableRow>
                <TableCell
                  colSpan={10}
                  className='text-muted-foreground h-32 text-center'
                >
                  {query.isError ? t('Failed to load data') : t('No data')}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
