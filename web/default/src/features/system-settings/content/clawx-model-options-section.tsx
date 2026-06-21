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
import { Save } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
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

export function ClawXModelOptionsSection({
  data,
}: ClawXModelOptionsSectionProps) {
  const updateOption = useUpdateOption()
  const [value, setValue] = useState(() => formatJson(data))

  const parsed = useMemo(() => parseModelOptions(value), [value])
  const hasInvalidJson = parsed === null

  const handleFormat = () => {
    const next = parseModelOptions(value)
    if (!next) {
      toast.error('Invalid JSON')
      return
    }
    setValue(JSON.stringify(next, null, 2))
  }

  const handleSave = () => {
    const next = parseModelOptions(value)
    if (!next) {
      toast.error('Invalid JSON')
      return
    }
    updateOption.mutate({
      key: 'clawx_client_setting.model_options',
      value: JSON.stringify(next),
    })
  }

  return (
    <SettingsSection title='ClawX Model Options'>
      <SettingsCard
        title='Client model catalog'
        description='Served by /api/clawx/client-config and consumed by UClaw for text, image, and video model selectors.'
      >
        <div className='flex flex-col gap-4'>
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
              disabled={hasInvalidJson || updateOption.isPending}
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
