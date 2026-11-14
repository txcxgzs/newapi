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
import { Eye, QrCode, Trash2, Upload } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import {
  compressPayoutQrCodeFile,
  isValidPayoutQrDataUrl,
} from '../lib/payout-qrcode'

interface PayoutQrcodeUploaderProps {
  value: string
  onChange: (next: string) => void
  id?: string
}

export function PayoutQrcodeUploader({
  value,
  onChange,
  id = 'payout-qrcode-input',
}: PayoutQrcodeUploaderProps) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement | null>(null)
  const [processing, setProcessing] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)

  async function handleFileChange(file: File | undefined) {
    if (!file) return
    setProcessing(true)
    try {
      const dataUrl = await compressPayoutQrCodeFile(file)
      onChange(dataUrl)
      toast.success(t('Payout QR code uploaded'))
    } catch (error) {
      toast.error(
        t(
          error instanceof Error
            ? error.message
            : 'Failed to load QR code image'
        )
      )
    } finally {
      setProcessing(false)
      if (inputRef.current) {
        inputRef.current.value = ''
      }
    }
  }

  const hasValidQr = isValidPayoutQrDataUrl(value)
  let uploadButtonLabel = t('Upload QR Code')
  if (processing) {
    uploadButtonLabel = t('Processing...')
  } else if (hasValidQr) {
    uploadButtonLabel = t('Replace QR Code')
  }

  return (
    <div className='space-y-2'>
      <input
        ref={inputRef}
        id={id}
        type='file'
        accept='image/png,image/jpeg,image/jpg,image/webp'
        className='hidden'
        onChange={(e) => handleFileChange(e.target.files?.[0])}
      />
      <div className='bg-background flex flex-wrap items-center gap-3 rounded-lg border border-dashed p-3'>
        {hasValidQr ? (
          <button
            type='button'
            onClick={() => setPreviewOpen(true)}
            className='group relative shrink-0 overflow-hidden rounded-md border bg-white p-1'
            title={t('Click to enlarge QR code')}
          >
            <img
              src={value}
              alt={t('Payout QR Code')}
              className='size-16 object-contain'
            />
          </button>
        ) : (
          <div className='bg-muted/40 text-muted-foreground flex size-16 shrink-0 items-center justify-center rounded-md border'>
            <QrCode className='size-7' />
          </div>
        )}
        <div className='min-w-0 flex-1 space-y-1'>
          <p className='text-xs font-medium'>
            {hasValidQr
              ? t('Payout QR code ready for finance verification & scanning')
              : t('Upload payout collection QR code (optional)')}
          </p>
          <p className='text-muted-foreground text-[11px] leading-relaxed'>
            {t(
              'Supports Alipay, WeChat Pay, or USDT wallet QR code (PNG/JPG/WebP). Automatically snapshotted onto withdrawal requests.'
            )}
          </p>
          <div className='flex flex-wrap gap-1.5 pt-0.5'>
            <Button
              type='button'
              size='sm'
              variant='outline'
              className='h-7 text-xs'
              disabled={processing}
              onClick={() => inputRef.current?.click()}
            >
              <Upload className='mr-1.5 size-3.5' />
              {uploadButtonLabel}
            </Button>
            {hasValidQr && (
              <>
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  className='h-7 text-xs'
                  onClick={() => setPreviewOpen(true)}
                >
                  <Eye className='mr-1 size-3.5' />
                  {t('Preview')}
                </Button>
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  className='text-destructive hover:text-destructive h-7 text-xs'
                  onClick={() => onChange('')}
                >
                  <Trash2 className='mr-1 size-3.5' />
                  {t('Remove')}
                </Button>
              </>
            )}
          </div>
        </div>
      </div>

      <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
        <DialogContent className='max-w-sm'>
          <DialogHeader>
            <DialogTitle>{t('Payout QR Code Preview')}</DialogTitle>
          </DialogHeader>
          {hasValidQr && (
            <div className='flex flex-col items-center gap-3 py-2'>
              <div className='rounded-xl border bg-white p-3 shadow-xs'>
                <img
                  src={value}
                  alt={t('Payout QR Code')}
                  className='max-h-72 max-w-72 object-contain'
                />
              </div>
              <p className='text-muted-foreground text-center text-xs'>
                {t('Finance officers scan this code directly when settling withdrawals.')}
              </p>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
