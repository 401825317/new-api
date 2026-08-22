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
import { useMemo, useState } from 'react'
import { ChevronDown, ChevronUp, Plus, Save, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { SettingsCard } from '../components/settings-card'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

type ClawXModelOptionsSectionProps = {
  data: string
}

type ParsedModelOptions = {
  text?: {
    defaultModel?: string
    fallbackModels?: unknown
    defaultThinkingLevel?: string
    models?: unknown[]
  }
  image?: {
    defaultModel?: string
    models?: unknown[]
  }
  video?: {
    defaultModel?: string
    models?: unknown[]
  }
}

type ThinkingLevel = 'off' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh'

type EnabledTextModel = {
  id: string
  label: string
}

type TextFallbackState = {
  models: string[]
  hasError: boolean
}

const thinkingLevelOptions: Array<{
  value: ThinkingLevel
  labelKey: string
}> = [
  { value: 'off', labelKey: 'Off' },
  { value: 'minimal', labelKey: 'Minimal' },
  { value: 'low', labelKey: 'Low' },
  { value: 'medium', labelKey: 'Medium' },
  { value: 'high', labelKey: 'High' },
  { value: 'xhigh', labelKey: 'Extra High' },
]

function normalizeThinkingLevel(value: string | undefined): ThinkingLevel {
  const normalized = value?.trim().toLowerCase()
  return thinkingLevelOptions.some((option) => option.value === normalized)
    ? (normalized as ThinkingLevel)
    : 'medium'
}

function formatJson(data: string): string {
  try {
    return JSON.stringify(JSON.parse(data || '{}'), null, 2)
  } catch {
    return data
  }
}

function parseModelOptions(data: string): ParsedModelOptions | null {
  try {
    const parsed = JSON.parse(data || '{}')
    if (!parsed || typeof parsed !== 'object') {
      return null
    }
    return parsed as ParsedModelOptions
  } catch {
    return null
  }
}

function countModels(models: unknown[] | undefined): number {
  return Array.isArray(models) ? models.length : 0
}

function getEnabledTextModels(
  models: unknown[] | undefined
): EnabledTextModel[] {
  if (!Array.isArray(models)) {
    return []
  }

  const seen = new Set<string>()
  const enabledModels: EnabledTextModel[] = []

  for (const value of models) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      continue
    }
    const model = value as Record<string, unknown>
    const id = typeof model.id === 'string' ? model.id.trim() : ''
    if (!id || model.enabled === false || seen.has(id)) {
      continue
    }
    const label =
      typeof model.label === 'string' && model.label.trim()
        ? model.label.trim()
        : id
    seen.add(id)
    enabledModels.push({ id, label })
  }

  return enabledModels
}

function getTextFallbackState(
  textOptions: ParsedModelOptions['text'],
  enabledModels: EnabledTextModel[]
): TextFallbackState {
  const rawFallbacks = textOptions?.fallbackModels
  if (rawFallbacks === undefined || rawFallbacks === null) {
    return { models: [], hasError: false }
  }
  if (
    !Array.isArray(rawFallbacks) ||
    rawFallbacks.some((model) => typeof model !== 'string')
  ) {
    return { models: [], hasError: true }
  }

  const models = rawFallbacks.map((model) => model.trim())
  const enabledModelIds = new Set(enabledModels.map((model) => model.id))
  const primaryModel = textOptions?.defaultModel?.trim() || ''
  const seen = new Set<string>()
  let hasError = models.length > 100

  for (const model of models) {
    if (
      !model ||
      model === primaryModel ||
      !enabledModelIds.has(model) ||
      seen.has(model)
    ) {
      hasError = true
    }
    seen.add(model)
  }

  return { models, hasError }
}

export function ClawXModelOptionsSection({
  data,
}: ClawXModelOptionsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [value, setValue] = useState(() => formatJson(data))
  const [pendingFallbackModel, setPendingFallbackModel] = useState<
    string | null
  >(null)

  const parsed = useMemo(() => parseModelOptions(value), [value])
  const hasInvalidJson = parsed === null
  const enabledTextModels = useMemo(
    () => getEnabledTextModels(parsed?.text?.models),
    [parsed]
  )
  const fallbackState = useMemo(
    () => getTextFallbackState(parsed?.text, enabledTextModels),
    [enabledTextModels, parsed]
  )
  const fallbackModelDetails = useMemo(
    () => new Map(enabledTextModels.map((model) => [model.id, model])),
    [enabledTextModels]
  )
  const fallbackCandidates = useMemo(() => {
    const primaryModel = parsed?.text?.defaultModel?.trim() || ''
    const selectedModels = new Set(fallbackState.models)
    return enabledTextModels.filter(
      (model) => model.id !== primaryModel && !selectedModels.has(model.id)
    )
  }, [enabledTextModels, fallbackState.models, parsed])
  const pendingFallbackValue = fallbackCandidates.some(
    (model) => model.id === pendingFallbackModel
  )
    ? pendingFallbackModel
    : null
  const defaultThinkingLevel = normalizeThinkingLevel(
    parsed?.text?.defaultThinkingLevel
  )

  const handleFormat = () => {
    const next = parseModelOptions(value)
    if (!next) {
      toast.error(t('Invalid JSON'))
      return
    }
    setValue(JSON.stringify(next, null, 2))
  }

  const handleSave = () => {
    const next = parseModelOptions(value)
    if (!next) {
      toast.error(t('Invalid JSON'))
      return
    }
    if (fallbackState.hasError) {
      toast.error(
        t(
          'Fallback models must be an ordered list of unique enabled text model IDs and cannot include the primary model.'
        )
      )
      return
    }
    updateOption.mutate({
      key: 'clawx_client_setting.model_options',
      value: JSON.stringify(next),
    })
  }

  const handleThinkingLevelChange = (level: ThinkingLevel | null) => {
    if (!level || !parsed) {
      return
    }
    setValue(
      JSON.stringify(
        {
          ...parsed,
          text: {
            ...parsed.text,
            defaultThinkingLevel: level,
          },
        },
        null,
        2
      )
    )
  }

  const handleFallbackModelsChange = (fallbackModels: string[]) => {
    if (!parsed) {
      return
    }
    setValue(
      JSON.stringify(
        {
          ...parsed,
          text: {
            ...parsed.text,
            fallbackModels,
          },
        },
        null,
        2
      )
    )
  }

  const handleAddFallbackModel = () => {
    if (!pendingFallbackValue) {
      return
    }
    handleFallbackModelsChange([...fallbackState.models, pendingFallbackValue])
    setPendingFallbackModel(null)
  }

  const handleRemoveFallbackModel = (index: number) => {
    handleFallbackModelsChange(
      fallbackState.models.filter((_, modelIndex) => modelIndex !== index)
    )
  }

  const handleMoveFallbackModel = (index: number, offset: -1 | 1) => {
    const destination = index + offset
    if (destination < 0 || destination >= fallbackState.models.length) {
      return
    }
    const nextModels = [...fallbackState.models]
    const movingModel = nextModels[index]
    nextModels[index] = nextModels[destination]
    nextModels[destination] = movingModel
    handleFallbackModelsChange(nextModels)
  }

  return (
    <SettingsSection title='ClawX Model Options'>
      <SettingsCard
        title='Client model catalog'
        description='Served by /api/clawx/client-config and consumed by UClaw for text, image, and video model selectors.'
      >
        <div className='flex flex-col gap-4'>
          <div className='grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(12rem,18rem)] md:items-center'>
            <div>
              <Label htmlFor='clawx-default-thinking-level'>
                {t('Default reasoning level')}
              </Label>
              <p className='text-muted-foreground mt-1 text-sm'>
                {t('Used when a conversation has no explicit reasoning level.')}
              </p>
            </div>
            <Select
              value={defaultThinkingLevel}
              onValueChange={handleThinkingLevelChange}
              disabled={hasInvalidJson || updateOption.isPending}
            >
              <SelectTrigger
                id='clawx-default-thinking-level'
                className='w-full'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {thinkingLevelOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {t(option.labelKey)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>

          <div className='grid gap-3 md:grid-cols-3'>
            <div className='rounded-md border p-3'>
              <div className='text-sm font-medium'>Text</div>
              <div className='text-muted-foreground mt-1 text-sm'>
                {countModels(parsed?.text?.models)} models, default{' '}
                {parsed?.text?.defaultModel || '-'}
              </div>
            </div>
            <div className='rounded-md border p-3'>
              <div className='text-sm font-medium'>Image</div>
              <div className='text-muted-foreground mt-1 text-sm'>
                {countModels(parsed?.image?.models)} models, default{' '}
                {parsed?.image?.defaultModel || '-'}
              </div>
            </div>
            <div className='rounded-md border p-3'>
              <div className='text-sm font-medium'>Video</div>
              <div className='text-muted-foreground mt-1 text-sm'>
                {countModels(parsed?.video?.models)} models, default{' '}
                {parsed?.video?.defaultModel || '-'}
              </div>
            </div>
          </div>

          <div className='flex flex-col gap-3 rounded-md border p-3'>
            <div>
              <Label>{t('Text fallback models')}</Label>
              <p className='text-muted-foreground mt-1 text-sm'>
                {t(
                  'Tried in order when the primary text model fails. Only enabled text models can be selected.'
                )}
              </p>
            </div>

            <div className='overflow-hidden rounded-md border'>
              {fallbackState.models.length === 0 ? (
                <p className='text-muted-foreground px-3 py-4 text-center text-sm'>
                  {t('No fallback models configured.')}
                </p>
              ) : (
                fallbackState.models.map((modelId, index) => {
                  const model = fallbackModelDetails.get(modelId)
                  return (
                    <div
                      key={`${modelId}-${index}`}
                      className='flex min-h-12 items-center gap-2 border-b px-3 py-2 last:border-b-0'
                    >
                      <span className='text-muted-foreground w-6 shrink-0 text-sm tabular-nums'>
                        {index + 1}
                      </span>
                      <div className='min-w-0 flex-1'>
                        <div className='truncate text-sm font-medium'>
                          {model?.label || modelId || '-'}
                        </div>
                        {model && model.label !== modelId && (
                          <div className='text-muted-foreground truncate font-mono text-xs'>
                            {modelId}
                          </div>
                        )}
                      </div>
                      <div className='flex shrink-0 items-center gap-1'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon-sm'
                          onClick={() => handleMoveFallbackModel(index, -1)}
                          disabled={index === 0 || updateOption.isPending}
                          aria-label={t('Move up')}
                          title={t('Move up')}
                        >
                          <ChevronUp />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon-sm'
                          onClick={() => handleMoveFallbackModel(index, 1)}
                          disabled={
                            index === fallbackState.models.length - 1 ||
                            updateOption.isPending
                          }
                          aria-label={t('Move down')}
                          title={t('Move down')}
                        >
                          <ChevronDown />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon-sm'
                          className='text-destructive'
                          onClick={() => handleRemoveFallbackModel(index)}
                          disabled={updateOption.isPending}
                          aria-label={t('Remove')}
                          title={t('Remove')}
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </div>
                  )
                })
              )}
            </div>

            <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]'>
              <Select
                value={pendingFallbackValue}
                onValueChange={setPendingFallbackModel}
                disabled={
                  hasInvalidJson ||
                  fallbackCandidates.length === 0 ||
                  updateOption.isPending
                }
              >
                <SelectTrigger className='w-full'>
                  <SelectValue placeholder={t('Select')} />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {fallbackCandidates.map((model) => (
                      <SelectItem key={model.id} value={model.id}>
                        {model.label === model.id
                          ? model.id
                          : `${model.label} (${model.id})`}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                type='button'
                variant='outline'
                onClick={handleAddFallbackModel}
                disabled={!pendingFallbackValue || updateOption.isPending}
              >
                <Plus data-icon='inline-start' />
                {t('Add')}
              </Button>
            </div>

            {fallbackState.hasError && !hasInvalidJson && (
              <div className='text-destructive text-sm'>
                {t(
                  'Fallback models must be an ordered list of unique enabled text model IDs and cannot include the primary model.'
                )}
              </div>
            )}
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='clawx-model-options-json'>Model options JSON</Label>
            <Textarea
              id='clawx-model-options-json'
              className='min-h-[420px] font-mono text-xs'
              value={value}
              onChange={(event) => setValue(event.target.value)}
              aria-invalid={hasInvalidJson}
              spellCheck={false}
            />
            {hasInvalidJson && (
              <div className='text-destructive text-sm'>
                JSON is invalid. Fix it before saving.
              </div>
            )}
          </div>

          <div className='flex justify-end gap-2'>
            <Button
              type='button'
              variant='outline'
              onClick={handleFormat}
              disabled={updateOption.isPending}
            >
              Format JSON
            </Button>
            <Button
              type='button'
              onClick={handleSave}
              disabled={
                hasInvalidJson ||
                fallbackState.hasError ||
                updateOption.isPending
              }
            >
              <Save className='mr-2 size-4' />
              Save
            </Button>
          </div>
        </div>
      </SettingsCard>
    </SettingsSection>
  )
}
