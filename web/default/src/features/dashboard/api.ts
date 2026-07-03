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
import { api } from '@/lib/api'
import type {
  ChannelAffinityUsageCacheSummary,
  PromptCacheUsageSummary,
  QuotaDataItem,
  UptimeGroupResult,
} from './types'

// ============================================================================
// Dashboard APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Quota & Usage Data
// ----------------------------------------------------------------------------

// Get user quota data within a time range
// Admin users get all users' data by default (matching classic frontend behavior)
export async function getUserQuotaDates(
  params: {
    start_timestamp: number
    end_timestamp: number
    default_time?: string
    username?: string
  },
  isAdmin = false
) {
  const endpoint = isAdmin ? '/api/data' : '/api/data/self'
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    endpoint,
    { params }
  )
  return res.data
}

// ----------------------------------------------------------------------------
// System Monitoring
// ----------------------------------------------------------------------------

export async function getUserQuotaDataByUsers(params: {
  start_timestamp: number
  end_timestamp: number
}) {
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    '/api/data/users',
    { params }
  )
  return res.data
}

export async function getChannelAffinityUsageCacheSummary(params?: {
  limit?: number
  topKeyLimit?: number
}) {
  const res = await api.get<{
    success: boolean
    message?: string
    data?: ChannelAffinityUsageCacheSummary
  }>('/api/log/channel_affinity_usage_cache/summary', {
    params: {
      limit: params?.limit,
      top_key_limit: params?.topKeyLimit,
    },
    disableDuplicate: true,
  } as Record<string, unknown>)
  return res.data
}

export async function getPromptCacheUsageSummary(params?: {
  startTimestamp?: number
  endTimestamp?: number
  username?: string
  model?: string
  channelId?: number
  group?: string
  limit?: number
}) {
  const res = await api.get<{
    success: boolean
    message?: string
    data?: PromptCacheUsageSummary
  }>('/api/log/prompt_cache/summary', {
    params: {
      start_timestamp: params?.startTimestamp,
      end_timestamp: params?.endTimestamp,
      username: params?.username,
      model: params?.model,
      channel_id: params?.channelId,
      group: params?.group,
      limit: params?.limit,
    },
    disableDuplicate: true,
  } as Record<string, unknown>)
  return res.data
}

// Get uptime monitoring status for all services
export async function getUptimeStatus() {
  const res = await api.get<{ success: boolean; data: UptimeGroupResult[] }>(
    '/api/uptime/status'
  )
  return res.data
}
