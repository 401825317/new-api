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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2, RefreshCw } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/dialog'

export type QrPaymentPayload = {
  qrCode: string
  tradeNo?: string
  amount?: string
  paymentType?: string
  expiresAt?: number
}

type QrPaymentDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  payload: QrPaymentPayload | null
  onRefresh?: () => void | Promise<void>
  onConfirmed?: () => void | Promise<void>
}

type WxPayQueryResponse = {
  success?: boolean
  message?: string
  data?: {
    paid?: boolean
    trade_state?: string
    trade_state_desc?: string
    local_status?: string
  }
}

export function QrPaymentDialog({
  open,
  onOpenChange,
  payload,
  onRefresh,
  onConfirmed,
}: QrPaymentDialogProps) {
  const { t } = useTranslation()
  const [checking, setChecking] = useState(false)
  const [statusText, setStatusText] = useState<string | null>(null)
  const isWxPayQr = payload?.paymentType === 'wxpay' && !!payload.tradeNo
  const expiresText = useMemo(() => {
    if (!payload?.expiresAt) return null
    return new Date(payload.expiresAt * 1000).toLocaleString()
  }, [payload?.expiresAt])

  const refreshAfterConfirmed = useCallback(async () => {
    if (onConfirmed) {
      await onConfirmed()
      return
    }
    await onRefresh?.()
  }, [onConfirmed, onRefresh])

  const checkPaymentStatus = useCallback(
    async (manual = false) => {
      if (!payload?.tradeNo || payload.paymentType !== 'wxpay' || checking) {
        return
      }

      try {
        setChecking(true)
        const res = await api.post(
          '/api/user/wxpay/query',
          { trade_no: payload.tradeNo },
          { skipBusinessError: true }
        )
        const body = res.data as WxPayQueryResponse
        if (!body.success) {
          if (manual) toast.error(body.message || t('Payment status check failed'))
          return
        }

        if (body.data?.paid || body.data?.local_status === 'success') {
          setStatusText(t('Payment confirmed'))
          toast.success(t('Payment confirmed'))
          await refreshAfterConfirmed()
          onOpenChange(false)
          return
        }

        const stateText = body.data?.trade_state_desc || body.data?.trade_state
        setStatusText(
          stateText
            ? `${t('Payment pending')}: ${stateText}`
            : t('Waiting for payment confirmation')
        )
        if (manual) {
          toast.info(t('Payment is not confirmed yet'))
        }
      } catch {
        if (manual) {
          toast.error(t('Payment status check failed'))
        }
      } finally {
        setChecking(false)
      }
    },
    [
      checking,
      onOpenChange,
      payload?.paymentType,
      payload?.tradeNo,
      refreshAfterConfirmed,
      t,
    ]
  )

  useEffect(() => {
    setStatusText(null)
  }, [payload?.tradeNo])

  useEffect(() => {
    if (!open || !isWxPayQr) return

    const initialTimer = window.setTimeout(() => {
      void checkPaymentStatus(false)
    }, 3000)
    const interval = window.setInterval(() => {
      void checkPaymentStatus(false)
    }, 5000)

    return () => {
      window.clearTimeout(initialTimer)
      window.clearInterval(interval)
    }
  }, [checkPaymentStatus, isWxPayQr, open])

  if (!payload) return null

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Scan QR code to pay')}
      description={t(
        'Use WeChat to scan the QR code. Balance will update after the payment callback is processed.'
      )}
      contentClassName='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-sm'
      bodyClassName='space-y-4'
      footer={
        <div className='flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
          {isWxPayQr ? (
            <Button
              onClick={() => void checkPaymentStatus(true)}
              disabled={checking}
              className='gap-2'
            >
              {checking ? (
                <Loader2 className='h-4 w-4 animate-spin' />
              ) : (
                <RefreshCw className='h-4 w-4' />
              )}
              {checking ? t('Checking...') : t('Check payment status')}
            </Button>
          ) : onRefresh ? (
            <Button onClick={() => void onRefresh()} className='gap-2'>
              <RefreshCw className='h-4 w-4' />
              {t('Refresh')}
            </Button>
          ) : null}
        </div>
      }
    >
      <div className='flex flex-col items-center gap-4 text-center'>
        <div className='rounded-md border bg-white p-3'>
          <QRCodeSVG value={payload.qrCode} size={220} />
        </div>
        {payload.amount ? (
          <div className='text-sm font-medium'>
            {t('Amount')}: {payload.amount}
          </div>
        ) : null}
        {payload.tradeNo ? (
          <div className='text-muted-foreground max-w-full text-xs break-all'>
            {t('Order')}: {payload.tradeNo}
          </div>
        ) : null}
        {expiresText ? (
          <div className='text-muted-foreground text-xs'>
            {t('QR code expires at')}: {expiresText}
          </div>
        ) : null}
        {statusText ? (
          <div className='text-muted-foreground text-xs' aria-live='polite'>
            {statusText}
          </div>
        ) : null}
      </div>
    </Dialog>
  )
}
