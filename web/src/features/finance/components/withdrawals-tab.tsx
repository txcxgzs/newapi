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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  CheckCircle2,
  Clock,
  Info,
  QrCode,
  ShieldCheck,
  XCircle,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { isValidPayoutQrDataUrl } from '@/features/merchant/lib/payout-qrcode'
import { getPayoutMethodLabel } from '@/features/merchant/payout-methods'
import type { MerchantWithdrawal } from '@/features/merchant/types'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { getFinanceWithdrawals, reviewFinanceWithdrawal } from '../api'
import {
  FINANCE_PAGE_SIZE,
  formatFinanceUsd,
  getWithdrawalStatusMeta,
} from '../format'
import { FinancePagination } from './shared'

const STATUS_FILTERS = [
  { value: -1, labelKey: 'All' },
  { value: 0, labelKey: 'Pending Review' },
  { value: 1, labelKey: 'Approved · Awaiting Pay' },
  { value: 2, labelKey: 'Paid' },
  { value: 3, labelKey: 'Rejected' },
] as const

export function FinanceWithdrawalsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const authUser = useAuthStore((state) => state.auth.user)
  const canPayout =
    !authUser ||
    hasPermission(
      authUser,
      ADMIN_PERMISSION_RESOURCES.FINANCE,
      ADMIN_PERMISSION_ACTIONS.PAYOUT
    )

  const [page, setPage] = useState(1)
  const [statusFilter, setStatusFilter] = useState<number>(-1)
  const [qrPayoutTarget, setQrPayoutTarget] =
    useState<MerchantWithdrawal | null>(null)
  const [rejectTarget, setRejectTarget] = useState<MerchantWithdrawal | null>(
    null
  )
  const [detailTarget, setDetailTarget] = useState<MerchantWithdrawal | null>(
    null
  )
  const [paidProof, setPaidProof] = useState('')
  const [reviewNote, setReviewNote] = useState('')
  const [rejectReason, setRejectReason] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const withdrawalsQuery = useQuery({
    queryKey: ['finance', 'withdrawals', page, statusFilter],
    queryFn: () =>
      getFinanceWithdrawals(page, FINANCE_PAGE_SIZE, statusFilter),
  })

  const items = withdrawalsQuery.data?.items ?? []

  function openQrPayModal(w: MerchantWithdrawal) {
    setPaidProof(w.paid_proof ?? '')
    setReviewNote(w.review_note ?? '')
    setQrPayoutTarget(w)
  }

  function openRejectModal(w: MerchantWithdrawal) {
    setRejectReason('')
    setRejectTarget(w)
  }

  async function executeReview(
    id: number,
    action: 'approve' | 'reject' | 'paid',
    note?: string,
    proof?: string
  ) {
    setSubmitting(true)
    try {
      await reviewFinanceWithdrawal(id, {
        action,
        review_note: note?.trim() || undefined,
        paid_proof: proof?.trim() || undefined,
      })
      toast.success(t('Withdrawal status updated'))
      setQrPayoutTarget(null)
      setRejectTarget(null)
      setPaidProof('')
      setReviewNote('')
      setRejectReason('')
      queryClient.invalidateQueries({ queryKey: ['finance'] })
    } catch (error) {
      handleServerError(error, t('Failed to update withdrawal'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='flex flex-wrap items-center gap-1.5'>
          {STATUS_FILTERS.map((filter) => (
            <Button
              key={filter.value}
              size='sm'
              variant={statusFilter === filter.value ? 'default' : 'outline'}
              className='h-8 text-xs'
              onClick={() => {
                setStatusFilter(filter.value)
                setPage(1)
              }}
            >
              {t(filter.labelKey)}
            </Button>
          ))}
        </div>
        <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
          <ShieldCheck className='text-primary size-4' />
          <span>
            {t(
              'Closed-loop flow: Review -> Scan Merchant QR -> Enter Payment Proof -> Mark Paid'
            )}
          </span>
        </div>
      </div>

      <Card className='overflow-hidden py-0'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Trade No / Time')}</TableHead>
              <TableHead>{t('Shop & Merchant')}</TableHead>
              <TableHead className='text-right'>{t('Amount (USD)')}</TableHead>
              <TableHead>{t('Payout Channel & Account')}</TableHead>
              <TableHead>{t('QR Snapshot')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Proof / Note')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((w) => {
              const statusMeta = getWithdrawalStatusMeta(w.status)
              const hasQr = isValidPayoutQrDataUrl(w.payout_qrcode)
              return (
                <TableRow key={w.id}>
                  <TableCell>
                    <p className='font-mono text-xs font-medium'>
                      {w.trade_no || `#${w.id}`}
                    </p>
                    <p className='text-muted-foreground text-xs'>
                      {formatTimestampToDate(w.created_at)}
                    </p>
                  </TableCell>
                  <TableCell>
                    <p className='text-sm font-medium'>
                      {w.shop_name || `Merchant #${w.merchant_user_id}`}
                    </p>
                    <p className='text-muted-foreground font-mono text-xs'>
                      {w.merchant_code ? `${w.merchant_code} · ` : ''}
                      {w.merchant_username ? `@${w.merchant_username}` : `#${w.merchant_user_id}`}
                    </p>
                  </TableCell>
                  <TableCell className='text-right font-mono text-sm font-semibold'>
                    {formatFinanceUsd(w.amount, 2)}
                  </TableCell>
                  <TableCell className='max-w-xs text-xs'>
                    <div className='flex flex-wrap items-center gap-1'>
                      {w.payout_method && (
                        <Badge variant='outline'>
                          {t(getPayoutMethodLabel(w.payout_method))}
                        </Badge>
                      )}
                      <span className='break-all'>{w.account_info || '--'}</span>
                    </div>
                    {w.note && (
                      <p className='text-muted-foreground mt-0.5 truncate'>
                        {w.note}
                      </p>
                    )}
                  </TableCell>
                  <TableCell>
                    {hasQr ? (
                      <button
                        type='button'
                        onClick={() => openQrPayModal(w)}
                        className='hover:ring-primary inline-flex items-center gap-1.5 rounded-md border bg-white p-1 pr-2 text-xs text-slate-900 shadow-2xs transition hover:ring-1'
                      >
                        <img
                          src={w.payout_qrcode}
                          alt={t('Payout QR Code')}
                          className='size-7 object-contain'
                        />
                        <span className='font-medium'>{t('Scan QR')}</span>
                      </button>
                    ) : (
                      <span className='text-muted-foreground text-xs'>--</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <Badge variant={statusMeta.variant}>
                      {t(statusMeta.labelKey)}
                    </Badge>
                  </TableCell>
                  <TableCell className='max-w-44 text-xs'>
                    {w.paid_proof && (
                      <p className='truncate font-mono'>{w.paid_proof}</p>
                    )}
                    <p className='text-muted-foreground truncate'>
                      {w.review_note || (!w.paid_proof ? '--' : '')}
                    </p>
                  </TableCell>
                  <TableCell className='text-right'>
                    <div className='flex flex-wrap justify-end gap-1.5'>
                      {canPayout && w.status === 0 && (
                        <>
                          <Button
                            size='sm'
                            variant='outline'
                            className='h-7'
                            disabled={submitting}
                            onClick={() => executeReview(w.id, 'approve')}
                          >
                            {t('Approve')}
                          </Button>
                          <Button
                            size='sm'
                            className='h-7'
                            disabled={submitting}
                            onClick={() => openQrPayModal(w)}
                          >
                            <QrCode className='mr-1 size-3.5' />
                            {t('Pay via QR')}
                          </Button>
                          <Button
                            size='sm'
                            variant='ghost'
                            className='text-destructive h-7'
                            disabled={submitting}
                            onClick={() => openRejectModal(w)}
                          >
                            {t('Reject')}
                          </Button>
                        </>
                      )}
                      {canPayout && w.status === 1 && (
                        <>
                          <Button
                            size='sm'
                            className='h-7'
                            disabled={submitting}
                            onClick={() => openQrPayModal(w)}
                          >
                            <QrCode className='mr-1 size-3.5' />
                            {t('Scan QR & Confirm Paid')}
                          </Button>
                          <Button
                            size='sm'
                            variant='ghost'
                            className='text-destructive h-7'
                            disabled={submitting}
                            onClick={() => openRejectModal(w)}
                          >
                            {t('Reject')}
                          </Button>
                        </>
                      )}
                      <Button
                        size='sm'
                        variant='ghost'
                        className='h-7 px-2'
                        onClick={() => setDetailTarget(w)}
                      >
                        <Info className='size-3.5' />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
            {!items.length && (
              <TableRow>
                <TableCell
                  colSpan={8}
                  className='text-muted-foreground py-10 text-center'
                >
                  {t('No withdrawal requests matching the filter')}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        <FinancePagination
          page={page}
          total={withdrawalsQuery.data?.total ?? 0}
          onPageChange={setPage}
        />
      </Card>

      {/* Scan Merchant QR & Mark Paid Modal */}
      <Dialog
        open={Boolean(qrPayoutTarget)}
        onOpenChange={(open) => !open && setQrPayoutTarget(null)}
      >
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Scan QR & Confirm Payout')}</DialogTitle>
            <DialogDescription>
              {qrPayoutTarget?.trade_no || `#${qrPayoutTarget?.id}`} ·{' '}
              {qrPayoutTarget?.shop_name ||
                `Merchant #${qrPayoutTarget?.merchant_user_id}`}
            </DialogDescription>
          </DialogHeader>
          {qrPayoutTarget && (
            <div className='space-y-4'>
              <div className='bg-muted/20 flex items-center justify-between rounded-xl border p-3'>
                <div className='min-w-0 space-y-0.5 text-xs'>
                  <p className='text-muted-foreground'>{t('Recipient')}</p>
                  <p className='truncate font-medium'>
                    {qrPayoutTarget.payout_method
                      ? `${t(getPayoutMethodLabel(qrPayoutTarget.payout_method))} · `
                      : ''}
                    {qrPayoutTarget.account_info || '--'}
                  </p>
                </div>
                <div className='shrink-0 text-right'>
                  <p className='text-muted-foreground text-xs'>
                    {t('Payout Amount')}
                  </p>
                  <p className='text-primary font-mono text-xl font-bold'>
                    {formatFinanceUsd(qrPayoutTarget.amount, 2)}
                  </p>
                </div>
              </div>

              {isValidPayoutQrDataUrl(qrPayoutTarget.payout_qrcode) ? (
                <div className='flex flex-col items-center gap-2 rounded-xl border bg-slate-50/70 p-4 dark:bg-slate-900/50'>
                  <div className='rounded-xl border bg-white p-3 shadow-xs'>
                    <img
                      src={qrPayoutTarget.payout_qrcode}
                      alt={t('Payout QR Code')}
                      className='max-h-60 max-w-60 object-contain'
                    />
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Scan the QR code above to transfer funds, then enter payment proof below to mark paid.'
                    )}
                  </p>
                </div>
              ) : (
                <div className='text-muted-foreground rounded-lg border border-dashed p-3 text-center text-xs'>
                  {t(
                    'No QR code snapshot attached to this withdrawal. Transfer via the account info shown above.'
                  )}
                </div>
              )}

              {(qrPayoutTarget.status === 0 || qrPayoutTarget.status === 1) &&
                canPayout && (
                  <div className='space-y-3'>
                    <div className='space-y-1.5'>
                      <Label htmlFor='finance-qr-paid-proof'>
                        {t('Payment Proof / Transaction ID')}
                      </Label>
                      <Input
                        id='finance-qr-paid-proof'
                        value={paidProof}
                        onChange={(e) => setPaidProof(e.target.value)}
                        placeholder={t('e.g. Alipay/Bank TxID or screenshot URL')}
                      />
                    </div>
                    <div className='space-y-1.5'>
                      <Label htmlFor='finance-qr-review-note'>
                        {t('Review Note')}
                      </Label>
                      <Input
                        id='finance-qr-review-note'
                        value={reviewNote}
                        onChange={(e) => setReviewNote(e.target.value)}
                        placeholder={t('Optional confirmation memo')}
                      />
                    </div>
                    <div className='flex justify-end gap-2 pt-1'>
                      <Button
                        variant='outline'
                        onClick={() => setQrPayoutTarget(null)}
                      >
                        {t('Cancel')}
                      </Button>
                      <Button
                        disabled={submitting}
                        onClick={() =>
                          executeReview(
                            qrPayoutTarget.id,
                            'paid',
                            reviewNote,
                            paidProof
                          )
                        }
                      >
                        <CheckCircle2 className='mr-1.5 size-4' />
                        {submitting ? t('Submitting...') : t('Confirm Paid')}
                      </Button>
                    </div>
                  </div>
                )}
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* Reject & Refund Modal */}
      <Dialog
        open={Boolean(rejectTarget)}
        onOpenChange={(open) => !open && setRejectTarget(null)}
      >
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>
              {t('Reject Withdrawal')}: {rejectTarget?.trade_no || `#${rejectTarget?.id}`}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Rejecting automatically unfreezes and returns {{amount}} to the merchant wallet.',
                { amount: formatFinanceUsd(rejectTarget?.amount, 2) }
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-3'>
            <div className='space-y-1.5'>
              <Label htmlFor='finance-reject-reason'>
                {t('Rejection Reason')}
              </Label>
              <Input
                id='finance-reject-reason'
                value={rejectReason}
                onChange={(e) => setRejectReason(e.target.value)}
                placeholder={t('e.g. Invalid QR code or account mismatch')}
              />
            </div>
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setRejectTarget(null)}>
                {t('Cancel')}
              </Button>
              <Button
                variant='destructive'
                disabled={submitting || !rejectTarget}
                onClick={() =>
                  rejectTarget &&
                  executeReview(rejectTarget.id, 'reject', rejectReason)
                }
              >
                <XCircle className='mr-1.5 size-4' />
                {t('Reject & Refund')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* Closed-loop Timeline Detail Modal */}
      <Dialog
        open={Boolean(detailTarget)}
        onOpenChange={(open) => !open && setDetailTarget(null)}
      >
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Withdrawal Lifecycle Details')}</DialogTitle>
            <DialogDescription>
              {detailTarget?.trade_no || `#${detailTarget?.id}`}
            </DialogDescription>
          </DialogHeader>
          {detailTarget && (
            <div className='space-y-3 text-xs'>
              <div className='bg-muted/20 space-y-1.5 rounded-lg border p-3'>
                <div className='flex justify-between'>
                  <span className='text-muted-foreground'>{t('Shop Name')}</span>
                  <span className='font-medium'>
                    {detailTarget.shop_name || `#${detailTarget.merchant_user_id}`}
                  </span>
                </div>
                <div className='flex justify-between'>
                  <span className='text-muted-foreground'>{t('Amount (USD)')}</span>
                  <span className='font-mono font-semibold'>
                    {formatFinanceUsd(detailTarget.amount, 2)}
                  </span>
                </div>
                <div className='flex justify-between'>
                  <span className='text-muted-foreground'>{t('Account')}</span>
                  <span>{detailTarget.account_info || '--'}</span>
                </div>
                <div className='flex justify-between'>
                  <span className='text-muted-foreground'>{t('Payment Proof')}</span>
                  <span className='font-mono'>
                    {detailTarget.paid_proof || '--'}
                  </span>
                </div>
                <div className='flex justify-between'>
                  <span className='text-muted-foreground'>{t('Review Note')}</span>
                  <span>{detailTarget.review_note || '--'}</span>
                </div>
              </div>
              <div className='space-y-2 rounded-lg border p-3'>
                <div className='flex items-center gap-2'>
                  <Clock className='text-muted-foreground size-3.5' />
                  <span>
                    {t('Requested')}:{' '}
                    {formatTimestampToDate(detailTarget.created_at)}
                  </span>
                </div>
                {(detailTarget.reviewed_at ?? 0) > 0 && (
                  <div className='flex items-center gap-2'>
                    <ShieldCheck className='text-sky-500 size-3.5' />
                    <span>
                      {t('Reviewed')}:{' '}
                      {formatTimestampToDate(detailTarget.reviewed_at || 0)}
                    </span>
                  </div>
                )}
                {detailTarget.paid_at > 0 && (
                  <div className='flex items-center gap-2'>
                    <CheckCircle2 className='size-3.5 text-emerald-500' />
                    <span>
                      {t('Paid & Settled')}:{' '}
                      {formatTimestampToDate(detailTarget.paid_at)}
                    </span>
                  </div>
                )}
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
