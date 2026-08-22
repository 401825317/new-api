import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  parseFeatures,
  parseObservability,
  serializeFeatures,
  serializeObservability,
} from './clawx-runtime-controls-section'

describe('ClawX runtime controls config round trips', () => {
  test('preserves observability rollout, sampling, and hourly limit fields', () => {
    const raw = JSON.stringify({
      enabled: true,
      rolloutPercentage: 37,
      sentryDsn: 'https://public@sentry.example.com/42',
      tunnelPath: '/api/clawx/observability/envelope',
      crashSampleRate: 0.8,
      handledErrorSampleRate: 0.4,
      tracesSampleRate: 0.15,
      artifactSampleRate: 0.25,
      maxEventsPerHour: 19,
      serverManaged: { region: 'cn' },
    })

    assert.deepEqual(
      JSON.parse(serializeObservability(parseObservability(raw))),
      JSON.parse(raw)
    )
  })

  test('preserves every feature gate and server-managed gate fields', () => {
    const raw = JSON.stringify({
      artifacts: {
        enabled: true,
        rolloutPercentage: 61,
        modelAlias: 'uclaw-artifact-v1',
        upstreamModel: 'smart-latest',
        policyVersion: 'v2',
        internalUserIds: [7, 11],
      },
      ecommerceMainImage: {
        enabled: true,
        rolloutPercentage: 23,
        skillVersion: 'v3',
        internalUserIds: [13],
      },
      htmlPreview: {
        enabled: true,
        rolloutPercentage: 47,
        internalUserIds: [17],
      },
      longTermRules: {
        enabled: true,
        rolloutPercentage: 59,
        internalUserIds: [19],
      },
    })

    assert.deepEqual(
      JSON.parse(serializeFeatures(parseFeatures(raw))),
      JSON.parse(raw)
    )
  })

  test('only trims editable string fields while retaining hidden feature data', () => {
    const parsed = parseFeatures(
      JSON.stringify({
        artifacts: {
          enabled: true,
          rolloutPercentage: 10,
          modelAlias: ' uclaw-artifact-v1 ',
          upstreamModel: ' smart-latest ',
          policyVersion: ' v1 ',
          internalUserIds: [23],
        },
        ecommerceMainImage: {
          enabled: false,
          rolloutPercentage: 20,
          skillVersion: ' v1 ',
        },
        htmlPreview: { enabled: false, rolloutPercentage: 30 },
        longTermRules: { enabled: false, rolloutPercentage: 40 },
      })
    )

    assert.deepEqual(JSON.parse(serializeFeatures(parsed)), {
      artifacts: {
        enabled: true,
        rolloutPercentage: 10,
        modelAlias: 'uclaw-artifact-v1',
        upstreamModel: 'smart-latest',
        policyVersion: 'v1',
        internalUserIds: [23],
      },
      ecommerceMainImage: {
        enabled: false,
        rolloutPercentage: 20,
        skillVersion: 'v1',
      },
      htmlPreview: { enabled: false, rolloutPercentage: 30 },
      longTermRules: { enabled: false, rolloutPercentage: 40 },
    })
  })
})
