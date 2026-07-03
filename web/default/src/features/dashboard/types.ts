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
import type { TimeGranularity } from '@/lib/time'

// ============================================================================
// Quota & Usage Data Types
// ============================================================================

export interface QuotaDataItem {
  id?: number
  user_id?: number
  username?: string
  model_name?: string
  created_at: number
  token_used?: number
  count?: number
  quota?: number
}

// ============================================================================
// Uptime Monitoring Types
// ============================================================================

export interface UptimeMonitor {
  name: string
  uptime: number
  status: number
  group?: string
}

export interface UptimeGroupResult {
  categoryName: string
  monitors: UptimeMonitor[]
}

// ============================================================================
// Dashboard Filter Types
// ============================================================================

export interface DashboardFilters {
  start_timestamp?: Date
  end_timestamp?: Date
  time_granularity?: TimeGranularity
  username?: string
}

export type ConsumptionDistributionChartType = 'bar' | 'area'

export type ModelAnalyticsChartTab = 'trend' | 'proportion' | 'top'

export interface DashboardChartPreferences {
  consumptionDistributionChart: ConsumptionDistributionChartType
  modelAnalyticsChart: ModelAnalyticsChartTab
  defaultTimeRangeDays: number
  defaultTimeGranularity: TimeGranularity
}

// ============================================================================
// Channel Affinity Cache Stats
// ============================================================================

export interface ChannelAffinityUsageCacheTopKey {
  rule_name: string
  using_group: string
  model: string
  channel_id: number
  key_fp: string
  cached_token_rate_mode: string
  hit: number
  total: number
  request_hit_rate: number
  prompt_tokens: number
  cached_tokens: number
  prompt_cache_hit_tokens: number
  last_seen_at: number
}

export interface ChannelAffinityUsageCacheAggregate {
  rule_name: string
  using_group: string
  model: string
  channel_id: number
  cached_token_rate_mode: string
  key_count: number
  hit: number
  total: number
  request_hit_rate: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cached_tokens: number
  prompt_cache_hit_tokens: number
  token_cache_rate: number
  token_cache_rate_available: boolean
  last_seen_at: number
  window_seconds: number
  top_keys?: ChannelAffinityUsageCacheTopKey[]
}

export interface ChannelAffinityUsageCacheSummary {
  enabled: boolean
  total_keys: number
  unknown: number
  cache_capacity: number
  cache_algo: string
  cached_token_rate_mode: string
  hit: number
  total: number
  request_hit_rate: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cached_tokens: number
  prompt_cache_hit_tokens: number
  token_cache_rate: number
  token_cache_rate_available: boolean
  last_seen_at: number
  generated_at: number
  top_keys?: ChannelAffinityUsageCacheTopKey[]
  by_rule_name: Record<string, ChannelAffinityUsageCacheAggregate>
  by_rule_group: ChannelAffinityUsageCacheAggregate[]
  by_model: ChannelAffinityUsageCacheAggregate[]
  by_channel: ChannelAffinityUsageCacheAggregate[]
}

// ============================================================================
// API Info Types
// ============================================================================

export interface ApiInfoItem {
  url: string
  route: string
  description: string
  color: string
}

export interface PingStatus {
  latency: number | null
  testing: boolean
  error: boolean
}

export type PingStatusMap = Record<string, PingStatus>

// ============================================================================
// Chart Types
// ============================================================================

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type VChartSpec = Record<string, any>

export interface ProcessedChartData {
  spec_pie: VChartSpec
  spec_line: VChartSpec
  spec_area: VChartSpec
  spec_model_line: VChartSpec
  spec_rank_bar: VChartSpec
  totalQuotaDisplay: string
  totalCountDisplay: string
}

export interface ProcessedUserChartData {
  spec_user_rank: VChartSpec
  spec_user_trend: VChartSpec
}

// ============================================================================
// Announcement Types
// ============================================================================

export interface AnnouncementItem {
  id?: number
  content: string
  publishDate?: string
  type?: 'default' | 'ongoing' | 'success' | 'warning' | 'error'
  extra?: string
}

// ============================================================================
// FAQ Types
// ============================================================================

export interface FAQItem {
  id?: number
  question: string
  answer: string
}
