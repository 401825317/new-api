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
import type { TFunction } from 'i18next'
import { Save } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { SettingsCard } from '../components/settings-card'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

type JsonObject = Record<string, unknown>

type ObservabilityConfig = JsonObject & {
  enabled: boolean
  sentryDsn?: string
  tunnelPath: string
  rolloutPercentage: number
  crashSampleRate: number
  handledErrorSampleRate: number
  tracesSampleRate: number
  artifactSampleRate: number
  maxEventsPerHour: number
}

type FeatureGate = JsonObject & {
  enabled: boolean
  rolloutPercentage: number
}

type RuntimeFeatures = JsonObject & {
  artifacts: FeatureGate & {
    modelAlias: string
    upstreamModel: string
    policyVersion: string
  }
  ecommerceMainImage: FeatureGate & {
    skillVersion: string
  }
  htmlPreview: FeatureGate
  longTermRules: FeatureGate
}

type ClawXRuntimeControlsSectionProps = {
  observabilityData: string
  featuresData: string
}

type RuntimeValidationError =
  | 'sentry_dsn_required'
  | 'sentry_dsn_invalid'
  | 'tunnel_path_invalid'
  | 'sample_rate_invalid'
  | 'event_limit_invalid'
  | 'rollout_invalid'
  | 'artifact_alias_invalid'
  | 'artifact_upstream_required'
  | 'artifact_upstream_recursive'
  | 'artifact_policy_invalid'
  | 'ecommerce_artifact_dependency'
  | 'ecommerce_skill_invalid'

const defaultObservability: ObservabilityConfig = {
  enabled: false,
  rolloutPercentage: 0,
  tunnelPath: '/api/clawx/observability/envelope',
  crashSampleRate: 1,
  handledErrorSampleRate: 0.2,
  tracesSampleRate: 0.05,
  artifactSampleRate: 0.2,
  maxEventsPerHour: 30,
}

const defaultFeatures: RuntimeFeatures = {
  artifacts: {
    enabled: false,
    rolloutPercentage: 0,
    modelAlias: 'uclaw-artifact-v1',
    upstreamModel: '',
    policyVersion: 'v1',
  },
  ecommerceMainImage: {
    enabled: false,
    rolloutPercentage: 0,
    skillVersion: 'v1',
  },
  htmlPreview: {
    enabled: false,
    rolloutPercentage: 0,
  },
  longTermRules: {
    enabled: false,
    rolloutPercentage: 0,
  },
}

const VERSIONED_ARTIFACT_ALIAS_PATTERN = /^uclaw-artifact-v[1-9][0-9]*$/
const VERSION_IDENTIFIER_PATTERN = /^v[1-9][0-9]*$/

function isNumberInRange(value: number, min: number, max: number): boolean {
  return Number.isFinite(value) && value >= min && value <= max
}

function validateObservability(
  observability: ObservabilityConfig
): RuntimeValidationError | null {
  const sentryDsn = observability.sentryDsn?.trim() || ''
  if (observability.enabled && !sentryDsn) {
    return 'sentry_dsn_required'
  }
  if (sentryDsn) {
    try {
      const parsed = new URL(sentryDsn)
      if (
        !['http:', 'https:'].includes(parsed.protocol) ||
        !parsed.host ||
        !parsed.username
      ) {
        return 'sentry_dsn_invalid'
      }
    } catch {
      return 'sentry_dsn_invalid'
    }
  }
  const tunnelPath = observability.tunnelPath.trim()
  if (tunnelPath && tunnelPath !== '/api/clawx/observability/envelope') {
    return 'tunnel_path_invalid'
  }
  const sampleRates = [
    observability.crashSampleRate,
    observability.handledErrorSampleRate,
    observability.tracesSampleRate,
    observability.artifactSampleRate,
  ]
  if (sampleRates.some((value) => !isNumberInRange(value, 0, 1))) {
    return 'sample_rate_invalid'
  }
  if (!isNumberInRange(observability.rolloutPercentage, 0, 100)) {
    return 'rollout_invalid'
  }
  if (
    !Number.isInteger(observability.maxEventsPerHour) ||
    !isNumberInRange(observability.maxEventsPerHour, 1, 100)
  ) {
    return 'event_limit_invalid'
  }
  return null
}

function validateFeatures(
  features: RuntimeFeatures
): RuntimeValidationError | null {
  if (
    !isNumberInRange(features.artifacts.rolloutPercentage, 0, 100) ||
    !isNumberInRange(features.ecommerceMainImage.rolloutPercentage, 0, 100) ||
    !isNumberInRange(features.htmlPreview.rolloutPercentage, 0, 100) ||
    !isNumberInRange(features.longTermRules.rolloutPercentage, 0, 100)
  ) {
    return 'rollout_invalid'
  }
  if (features.artifacts.enabled) {
    const modelAlias = features.artifacts.modelAlias.trim()
    if (!VERSIONED_ARTIFACT_ALIAS_PATTERN.test(modelAlias)) {
      return 'artifact_alias_invalid'
    }
    const upstreamModel = features.artifacts.upstreamModel.trim()
    if (!upstreamModel) {
      return 'artifact_upstream_required'
    }
    const upstreamName = upstreamModel.split('/').pop() || ''
    if (upstreamName.startsWith('uclaw-artifact-v')) {
      return 'artifact_upstream_recursive'
    }
    if (
      !VERSION_IDENTIFIER_PATTERN.test(features.artifacts.policyVersion.trim())
    ) {
      return 'artifact_policy_invalid'
    }
  }
  if (features.ecommerceMainImage.enabled) {
    if (!features.artifacts.enabled) {
      return 'ecommerce_artifact_dependency'
    }
    if (
      !VERSION_IDENTIFIER_PATTERN.test(
        features.ecommerceMainImage.skillVersion.trim()
      )
    ) {
      return 'ecommerce_skill_invalid'
    }
  }
  return null
}

function translateValidationError(
  error: RuntimeValidationError | null,
  t: TFunction
): string | null {
  switch (error) {
    case 'sentry_dsn_required':
      return t('Sentry DSN is required when observability is enabled.')
    case 'sentry_dsn_invalid':
      return t('Sentry DSN must be a valid HTTP(S) URL with a public key.')
    case 'tunnel_path_invalid':
      return t(
        'Envelope tunnel path must be /api/clawx/observability/envelope.'
      )
    case 'sample_rate_invalid':
      return t('Sample rates must be between 0 and 1.')
    case 'event_limit_invalid':
      return t(
        'Maximum events per client per hour must be an integer between 1 and 100.'
      )
    case 'rollout_invalid':
      return t('Rollout percentages must be between 0 and 100.')
    case 'artifact_alias_invalid':
      return t('Artifact model alias must use uclaw-artifact-vN.')
    case 'artifact_upstream_required':
      return t(
        'Fixed upstream model is required when artifact tasks are enabled.'
      )
    case 'artifact_upstream_recursive':
      return t('Fixed upstream model cannot reference an artifact alias.')
    case 'artifact_policy_invalid':
      return t('Policy version must use vN.')
    case 'ecommerce_artifact_dependency':
      return t('Ecommerce main image requires artifact tasks to be enabled.')
    case 'ecommerce_skill_invalid':
      return t('Skill version must use vN.')
    default:
      return null
  }
}

function finiteNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

function parseJsonObject(raw: string): JsonObject | null {
  try {
    const parsed: unknown = JSON.parse(raw || '{}')
    return parsed !== null &&
      typeof parsed === 'object' &&
      !Array.isArray(parsed)
      ? (parsed as JsonObject)
      : null
  } catch {
    return null
  }
}

function parseFeatureGate(value: unknown, fallback: FeatureGate): FeatureGate {
  const parsed =
    value !== null && typeof value === 'object' && !Array.isArray(value)
      ? (value as JsonObject)
      : {}
  return {
    ...fallback,
    ...parsed,
    enabled: parsed.enabled === true,
    rolloutPercentage: finiteNumber(
      parsed.rolloutPercentage,
      fallback.rolloutPercentage
    ),
  }
}

export function parseObservability(raw: string): ObservabilityConfig {
  const parsed = parseJsonObject(raw)
  if (!parsed) {
    return structuredClone(defaultObservability)
  }

  return {
    ...defaultObservability,
    ...parsed,
    enabled: parsed.enabled === true,
    sentryDsn:
      typeof parsed.sentryDsn === 'string' ? parsed.sentryDsn : undefined,
    tunnelPath:
      typeof parsed.tunnelPath === 'string'
        ? parsed.tunnelPath
        : defaultObservability.tunnelPath,
    rolloutPercentage: finiteNumber(
      parsed.rolloutPercentage,
      defaultObservability.rolloutPercentage
    ),
    crashSampleRate: finiteNumber(
      parsed.crashSampleRate,
      defaultObservability.crashSampleRate
    ),
    handledErrorSampleRate: finiteNumber(
      parsed.handledErrorSampleRate,
      defaultObservability.handledErrorSampleRate
    ),
    tracesSampleRate: finiteNumber(
      parsed.tracesSampleRate,
      defaultObservability.tracesSampleRate
    ),
    artifactSampleRate: finiteNumber(
      parsed.artifactSampleRate,
      defaultObservability.artifactSampleRate
    ),
    maxEventsPerHour: finiteNumber(
      parsed.maxEventsPerHour,
      defaultObservability.maxEventsPerHour
    ),
  }
}

export function parseFeatures(raw: string): RuntimeFeatures {
  const parsed = parseJsonObject(raw)
  if (!parsed) {
    return structuredClone(defaultFeatures)
  }

  const artifacts = parseFeatureGate(
    parsed.artifacts,
    defaultFeatures.artifacts
  ) as RuntimeFeatures['artifacts']
  const ecommerce = parseFeatureGate(
    parsed.ecommerceMainImage,
    defaultFeatures.ecommerceMainImage
  ) as RuntimeFeatures['ecommerceMainImage']
  const artifactsEnabled = artifacts.enabled

  return {
    ...defaultFeatures,
    ...parsed,
    artifacts: {
      ...artifacts,
      modelAlias:
        typeof artifacts.modelAlias === 'string'
          ? artifacts.modelAlias
          : defaultFeatures.artifacts.modelAlias,
      upstreamModel:
        typeof artifacts.upstreamModel === 'string'
          ? artifacts.upstreamModel
          : '',
      policyVersion:
        typeof artifacts.policyVersion === 'string'
          ? artifacts.policyVersion
          : defaultFeatures.artifacts.policyVersion,
    },
    ecommerceMainImage: {
      ...ecommerce,
      enabled: artifactsEnabled && ecommerce.enabled,
      skillVersion:
        typeof ecommerce.skillVersion === 'string'
          ? ecommerce.skillVersion
          : defaultFeatures.ecommerceMainImage.skillVersion,
    },
    htmlPreview: parseFeatureGate(
      parsed.htmlPreview,
      defaultFeatures.htmlPreview
    ),
    longTermRules: parseFeatureGate(
      parsed.longTermRules,
      defaultFeatures.longTermRules
    ),
  }
}

export function serializeObservability(
  observability: ObservabilityConfig
): string {
  return JSON.stringify({
    ...observability,
    sentryDsn: observability.sentryDsn?.trim() || undefined,
    tunnelPath:
      observability.tunnelPath.trim() || defaultObservability.tunnelPath,
  })
}

export function serializeFeatures(features: RuntimeFeatures): string {
  return JSON.stringify({
    ...features,
    artifacts: {
      ...features.artifacts,
      modelAlias: features.artifacts.modelAlias.trim(),
      upstreamModel: features.artifacts.upstreamModel.trim(),
      policyVersion: features.artifacts.policyVersion.trim(),
    },
    ecommerceMainImage: {
      ...features.ecommerceMainImage,
      skillVersion: features.ecommerceMainImage.skillVersion.trim(),
    },
  })
}

function NumericField(props: {
  id: string
  label: string
  value: number
  min: number
  max: number
  step: number
  disabled?: boolean
  onChange: (value: number) => void
}) {
  return (
    <div className='space-y-2'>
      <Label htmlFor={props.id}>{props.label}</Label>
      <Input
        id={props.id}
        type='number'
        min={props.min}
        max={props.max}
        step={props.step}
        disabled={props.disabled}
        value={props.value}
        onChange={(event) => props.onChange(Number(event.target.value))}
      />
    </div>
  )
}

export function ClawXRuntimeControlsSection(
  props: ClawXRuntimeControlsSectionProps
) {
  const { t } = useTranslation()
  const observabilityMutation = useUpdateOption()
  const featuresMutation = useUpdateOption()
  const [observability, setObservability] = useState(() =>
    parseObservability(props.observabilityData)
  )
  const [features, setFeatures] = useState(() =>
    parseFeatures(props.featuresData)
  )
  const observabilityError = validateObservability(observability)
  const featuresError = validateFeatures(features)
  const observabilityErrorMessage = translateValidationError(
    observabilityError,
    t
  )
  const featuresErrorMessage = translateValidationError(featuresError, t)

  const saveObservability = () => {
    if (observabilityErrorMessage) {
      toast.error(observabilityErrorMessage)
      return
    }
    observabilityMutation.mutate({
      key: 'clawx_client_setting.observability',
      value: serializeObservability(observability),
    })
  }

  const saveFeatures = () => {
    if (featuresErrorMessage) {
      toast.error(featuresErrorMessage)
      return
    }
    featuresMutation.mutate({
      key: 'clawx_client_setting.features',
      value: serializeFeatures(features),
    })
  }

  return (
    <SettingsSection title={t('UClaw Runtime Controls')}>
      <SettingsCard
        title={t('Observability')}
        description={t('Remote crash reporting and diagnostic sampling.')}
      >
        <div className='space-y-5'>
          <div className='flex items-center justify-between gap-4'>
            <Label htmlFor='uclaw-observability-enabled'>
              {t('Enable observability')}
            </Label>
            <Switch
              id='uclaw-observability-enabled'
              checked={observability.enabled}
              onCheckedChange={(enabled) =>
                setObservability((current) => ({ ...current, enabled }))
              }
            />
          </div>

          <div className='grid gap-4 lg:grid-cols-2'>
            <NumericField
              id='uclaw-observability-rollout'
              label={t('Rollout percentage')}
              value={observability.rolloutPercentage}
              min={0}
              max={100}
              step={1}
              onChange={(rolloutPercentage) =>
                setObservability((current) => ({
                  ...current,
                  rolloutPercentage,
                }))
              }
            />
            <div className='space-y-2 lg:col-span-2'>
              <Label htmlFor='uclaw-sentry-dsn'>{t('Sentry DSN')}</Label>
              <Input
                id='uclaw-sentry-dsn'
                value={observability.sentryDsn || ''}
                onChange={(event) =>
                  setObservability((current) => ({
                    ...current,
                    sentryDsn: event.target.value,
                  }))
                }
                spellCheck={false}
              />
            </div>
            <div className='space-y-2 lg:col-span-2'>
              <Label htmlFor='uclaw-tunnel-path'>
                {t('Envelope tunnel path')}
              </Label>
              <Input
                id='uclaw-tunnel-path'
                value={observability.tunnelPath}
                onChange={(event) =>
                  setObservability((current) => ({
                    ...current,
                    tunnelPath: event.target.value,
                  }))
                }
                spellCheck={false}
              />
            </div>
            <NumericField
              id='uclaw-crash-sample-rate'
              label={t('Crash sample rate')}
              value={observability.crashSampleRate}
              min={0}
              max={1}
              step={0.01}
              onChange={(crashSampleRate) =>
                setObservability((current) => ({
                  ...current,
                  crashSampleRate,
                }))
              }
            />
            <NumericField
              id='uclaw-handled-sample-rate'
              label={t('Handled error sample rate')}
              value={observability.handledErrorSampleRate}
              min={0}
              max={1}
              step={0.01}
              onChange={(handledErrorSampleRate) =>
                setObservability((current) => ({
                  ...current,
                  handledErrorSampleRate,
                }))
              }
            />
            <NumericField
              id='uclaw-trace-sample-rate'
              label={t('Performance trace sample rate')}
              value={observability.tracesSampleRate}
              min={0}
              max={1}
              step={0.01}
              onChange={(tracesSampleRate) =>
                setObservability((current) => ({
                  ...current,
                  tracesSampleRate,
                }))
              }
            />
            <NumericField
              id='uclaw-artifact-sample-rate'
              label={t('Artifact task sample rate')}
              value={observability.artifactSampleRate}
              min={0}
              max={1}
              step={0.01}
              onChange={(artifactSampleRate) =>
                setObservability((current) => ({
                  ...current,
                  artifactSampleRate,
                }))
              }
            />
            <NumericField
              id='uclaw-max-events'
              label={t('Maximum events per client per hour')}
              value={observability.maxEventsPerHour}
              min={1}
              max={100}
              step={1}
              onChange={(maxEventsPerHour) =>
                setObservability((current) => ({
                  ...current,
                  maxEventsPerHour,
                }))
              }
            />
          </div>

          <div className='flex flex-wrap items-center justify-between gap-3'>
            {observabilityErrorMessage && (
              <p className='text-destructive text-sm' role='alert'>
                {observabilityErrorMessage}
              </p>
            )}
            <Button
              className='ml-auto'
              type='button'
              onClick={saveObservability}
              disabled={
                Boolean(observabilityError) || observabilityMutation.isPending
              }
            >
              <Save className='mr-2 size-4' />
              {t('Save')}
            </Button>
          </div>
        </div>
      </SettingsCard>

      <SettingsCard
        title={t('Feature rollout')}
        description={t(
          'Control artifact and ecommerce image rollout independently.'
        )}
      >
        <div className='space-y-6'>
          <section className='space-y-4 border-b pb-6'>
            <div className='flex items-center justify-between gap-4'>
              <Label htmlFor='uclaw-artifacts-enabled'>
                {t('Artifact tasks')}
              </Label>
              <Switch
                id='uclaw-artifacts-enabled'
                checked={features.artifacts.enabled}
                onCheckedChange={(enabled) =>
                  setFeatures((current) => ({
                    ...current,
                    artifacts: { ...current.artifacts, enabled },
                    ecommerceMainImage: enabled
                      ? current.ecommerceMainImage
                      : { ...current.ecommerceMainImage, enabled: false },
                  }))
                }
              />
            </div>
            <div className='grid gap-4 md:grid-cols-2'>
              <NumericField
                id='uclaw-artifacts-rollout'
                label={t('Rollout percentage')}
                value={features.artifacts.rolloutPercentage}
                min={0}
                max={100}
                step={1}
                onChange={(rolloutPercentage) =>
                  setFeatures((current) => ({
                    ...current,
                    artifacts: {
                      ...current.artifacts,
                      rolloutPercentage,
                    },
                  }))
                }
              />
              <div className='space-y-2'>
                <Label htmlFor='uclaw-artifact-policy'>
                  {t('Policy version')}
                </Label>
                <Input
                  id='uclaw-artifact-policy'
                  value={features.artifacts.policyVersion}
                  onChange={(event) =>
                    setFeatures((current) => ({
                      ...current,
                      artifacts: {
                        ...current.artifacts,
                        policyVersion: event.target.value,
                      },
                    }))
                  }
                />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='uclaw-artifact-alias'>{t('Model alias')}</Label>
                <Input
                  id='uclaw-artifact-alias'
                  value={features.artifacts.modelAlias}
                  onChange={(event) =>
                    setFeatures((current) => ({
                      ...current,
                      artifacts: {
                        ...current.artifacts,
                        modelAlias: event.target.value,
                      },
                    }))
                  }
                  spellCheck={false}
                />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='uclaw-artifact-upstream'>
                  {t('Fixed upstream model')}
                </Label>
                <Input
                  id='uclaw-artifact-upstream'
                  value={features.artifacts.upstreamModel}
                  onChange={(event) =>
                    setFeatures((current) => ({
                      ...current,
                      artifacts: {
                        ...current.artifacts,
                        upstreamModel: event.target.value,
                      },
                    }))
                  }
                  spellCheck={false}
                />
              </div>
            </div>
          </section>

          <section className='space-y-4'>
            <div className='flex items-center justify-between gap-4'>
              <Label htmlFor='uclaw-ecommerce-enabled'>
                {t('Ecommerce main image')}
              </Label>
              <Switch
                id='uclaw-ecommerce-enabled'
                checked={features.ecommerceMainImage.enabled}
                disabled={!features.artifacts.enabled}
                onCheckedChange={(enabled) =>
                  setFeatures((current) => ({
                    ...current,
                    ecommerceMainImage: {
                      ...current.ecommerceMainImage,
                      enabled,
                    },
                  }))
                }
              />
            </div>
            <div className='grid gap-4 md:grid-cols-2'>
              <NumericField
                id='uclaw-ecommerce-rollout'
                label={t('Rollout percentage')}
                value={features.ecommerceMainImage.rolloutPercentage}
                min={0}
                max={100}
                step={1}
                disabled={!features.artifacts.enabled}
                onChange={(rolloutPercentage) =>
                  setFeatures((current) => ({
                    ...current,
                    ecommerceMainImage: {
                      ...current.ecommerceMainImage,
                      rolloutPercentage,
                    },
                  }))
                }
              />
              <div className='space-y-2'>
                <Label htmlFor='uclaw-ecommerce-skill'>
                  {t('Skill version')}
                </Label>
                <Input
                  id='uclaw-ecommerce-skill'
                  value={features.ecommerceMainImage.skillVersion}
                  disabled={!features.artifacts.enabled}
                  onChange={(event) =>
                    setFeatures((current) => ({
                      ...current,
                      ecommerceMainImage: {
                        ...current.ecommerceMainImage,
                        skillVersion: event.target.value,
                      },
                    }))
                  }
                />
              </div>
            </div>
          </section>

          <div className='flex flex-wrap items-center justify-between gap-3'>
            {featuresErrorMessage && (
              <p className='text-destructive text-sm' role='alert'>
                {featuresErrorMessage}
              </p>
            )}
            <Button
              className='ml-auto'
              type='button'
              onClick={saveFeatures}
              disabled={Boolean(featuresError) || featuresMutation.isPending}
            >
              <Save className='mr-2 size-4' />
              {t('Save')}
            </Button>
          </div>
        </div>
      </SettingsCard>
    </SettingsSection>
  )
}
