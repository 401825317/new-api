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
import { SettingsPage } from '../components/settings-page'
import type { ContentSettings, SystemOption } from '../types'
import {
  CONTENT_DEFAULT_SECTION,
  getContentSectionContent,
  getContentSectionMeta,
} from './section-registry.tsx'

const DEFAULT_CLAWX_MODEL_OPTIONS =
  '{"text":{"defaultModel":"smart-latest","models":[{"id":"smart-latest","label":"智能路由","description":"自动选择合适的文本模型。","enabled":true},{"id":"qwen-latest","label":"通义千问","enabled":true},{"id":"deepseek-latest","label":"DeepSeek","enabled":true},{"id":"doubao-latest","label":"豆包","enabled":true},{"id":"kimi-latest","label":"Kimi","enabled":true},{"id":"glm-latest","label":"智谱 GLM","enabled":true}]},"image":{"defaultModel":"gpt-image-2","defaultSize":"1024x1024","defaultQuality":"medium","models":[{"id":"gpt-image-2","label":"Image 2","description":"Image generation and editing.","sizes":["1024x1024","2048x2048","3840x2160"],"qualities":["low","medium","high"],"defaultSize":"1024x1024","defaultQuality":"medium","supportsEditing":true,"enabled":true}]},"video":{"defaultModel":"grok-image-video","defaultSize":"1280x720","defaultDurationSeconds":4,"models":[{"id":"grok-image-video","label":"Grok Video","description":"Supports text-to-video and image-to-video.","modes":["text-to-video","image-to-video"],"sizes":["1280x720","720x1280","1024x1024"],"durations":[4,6,8,10,12,15],"defaultSize":"1280x720","defaultDurationSeconds":4,"requiresImage":false,"enabled":true},{"id":"grok-video-1.5","label":"Grok Video 1.5","description":"Image-to-video model that requires one reference image.","modes":["image-to-video"],"sizes":["1280x720","720x1280","1024x1024"],"durations":[4,6,8,10,12,15],"defaultSize":"1280x720","defaultDurationSeconds":4,"requiresImage":true,"enabled":true}]}}'

const defaultContentSettings: ContentSettings = {
  'console_setting.api_info': '[]',
  'console_setting.announcements': '[]',
  'console_setting.faq': '[]',
  'console_setting.uptime_kuma_groups': '[]',
  'console_setting.api_info_enabled': true,
  'console_setting.announcements_enabled': true,
  'console_setting.faq_enabled': true,
  'console_setting.uptime_kuma_enabled': false,
  'clawx_client_setting.announcements': '[]',
  'clawx_client_setting.announcements_enabled': false,
  'clawx_client_setting.support': '{}',
  'clawx_client_setting.support_enabled': false,
  'clawx_client_setting.model_options': DEFAULT_CLAWX_MODEL_OPTIONS,
  DataExportEnabled: false,
  DataExportDefaultTime: 'hour',
  DataExportInterval: 5,
  Chats: '[]',
  DrawingEnabled: false,
  MjNotifyEnabled: false,
  MjAccountFilterEnabled: false,
  MjForwardUrlEnabled: false,
  MjModeClearEnabled: false,
  MjActionCheckSuccessEnabled: false,
}

function resolveContentSettings(
  settings: ContentSettings,
  raw: SystemOption[] | undefined
): ContentSettings {
  if (!raw || raw.length === 0) return settings

  const optionMap = new Map(raw.map((item) => [item.key, item.value]))
  const next = { ...settings }

  const legacyMap = [
    { current: 'console_setting.announcements', legacy: 'Announcements' },
    { current: 'console_setting.api_info', legacy: 'ApiInfo' },
    { current: 'console_setting.faq', legacy: 'FAQ' },
  ] as const

  for (const { current, legacy } of legacyMap) {
    if (!optionMap.has(current)) {
      const legacyValue = optionMap.get(legacy)
      if (legacyValue !== undefined) {
        next[current] = legacyValue
      }
    }
  }

  if (!optionMap.has('console_setting.uptime_kuma_groups')) {
    const legacyUrl = optionMap.get('UptimeKumaUrl')
    const legacySlug = optionMap.get('UptimeKumaSlug')
    if (legacyUrl && legacySlug) {
      next['console_setting.uptime_kuma_groups'] = JSON.stringify([
        { id: 1, categoryName: 'Legacy', url: legacyUrl, slug: legacySlug },
      ])
    }
  }

  return next
}

export function ContentSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/content/$section'
      defaultSettings={defaultContentSettings}
      defaultSection={CONTENT_DEFAULT_SECTION}
      getSectionContent={getContentSectionContent}
      getSectionMeta={getContentSectionMeta}
      loadingMessage='Loading content settings...'
      resolveSettings={resolveContentSettings}
    />
  )
}
