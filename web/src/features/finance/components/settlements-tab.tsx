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
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { MerchantWithdrawal } from '@/features/merchant/types'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getFinanceMerchantEarnings,
  getFinanceWithdrawals,
  reverseMerchantEarning,
  reviewFinanceWithdrawal,
  settleDueMerchantEarnings,
} from '../api'
import {
  FINANCE_PAGE_SIZE,
  formatFinanceUsd,
  getWithdrawalStatusMeta,
} from '../format'
import type { FinanceMerchantEarning } from '../types'
import { FinancePagination } from './shared'

function getEarningBadgeMeta(status: number): {
  variant: 'default' | 'secondary' | 'destructive'
  labelKey: string
} {
  if (status === 2) return { variant: 'secondary', labelKey: 'Unsettled (T+N)' }
  if (status === 3) return { variant: 'destructive', labelKey: 'Reversed' }
  return { variant: 'default', labelKey: 'Settled' }
}

export function FinanceSettlementsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [earnPage, setEarnPage] = useState(1)
  const [earnStatus, setEarnStatus] = useState('0')
  const [modelFilter, setModelFilter] = useState('')
  const [settling, setSettling] = useState(false)
  const [reversing, setReversing] = useState<FinanceMerchantEarning | null>(null)
  const [reverseReason, setReverseReason] = useState('')
  const [reverseSubmitting, setReverseSubmitting] = useState(false)

  const [wdPage, setWdPage] = useState(1)
  const [wdStatus, setWdStatus] = useState('-1')
  const [paying, setPaying] = useState<MerchantWithdrawal | null>(null)
  const [paidProof, setPaidProof] = useState('')
  const [reviewNote, setReviewNote] = useState('')

  const earningsQuery = useQuery({
    queryKey: ['finance', 'earnings', earnPage, earnStatus, modelFilter],
    queryFn: () =>
      getFinanceMerchantEarnings({
        p: earnPage,
        page_size: FINANCE_PAGE_SIZE,
        status: Number(earnStatus) || undefined,
        model_name: modelFilter.trim() || undefined,
      }),
  })

  const withdrawalsQuery = useQuery({
    queryKey: ['finance', 'withdrawals', wdPage, wdStatus],
    queryFn: () =>
      getFinanceWithdrawals(wdPage, FINANCE_PAGE_SIZE, Number(wdStatus)),
  })

  const earnings = earningsQuery.data?.items ?? []
  const withdrawals = withdrawalsQuery.data?.items ?? []

  async function handleSettleDue() {
    setSettling(true)
    try {
      const res = await settleDueMerchantEarnings()
      toast.success(
        t('Settled {{count}} due records ({{amount}})', {
          count: res.settled_count,
          amount: formatFinanceUsd(res.settled_amount),
        })
      )
      queryClient.invalidateQueries({ queryKey: ['finance'] })
    } catch (error) {
      handleServerError(error, t('Failed to settle due earnings'))
    } finally {
      setSettling(false)
    }
  }

  async function handleReverseConfirm() {
    if (!reversing) return
    setReverseSubmitting(true)
    try {
      await reverseMerchantEarning(reversing.id, reverseReason.trim())
      toast.success(t('Earning reversed and clawed back'))
      setReversing(null)
      setReverseReason('')
      queryClient.invalidateQueries({ queryKey: ['finance'] })
    } catch (error) {
      handleServerError(error, t('Failed to reverse earning'))
    } finally {
      setReverseSubmitting(false)
    }
  }

  async function handleWithdrawalAction(
    id: number,
    action: 'approve' | 'reject' | 'paid',
    note = '',
    proof = ''
  ) {
    try {
      await reviewFinanceWithdrawal(id, {
        action,
        review_note: note,
        paid_proof: proof,
      })
      toast.success(t('Updated'))
      setPaying(null)
      setPaidProof('')
      setReviewNote('')
      queryClient.invalidateQueries({ queryKey: ['finance'] })
      queryClient.invalidateQueries({
        queryKey: ['merchant-admin', 'withdrawals'],
      })
    } catch (error) {
      handleServerError(error, t('Operation failed'))
    }
  }

  return (
    <div className='space-y-6'>
      <section className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <h3 className='text-sm font-semibold'>
            {t('Merchant Earnings & T+N Settlement')}
          </h3>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              value={modelFilter}
              onChange={(e) => {
                setModelFilter(e.target.value)
                setEarnPage(1)
              }}
              placeholder={t('Filter by model')}
              className='w-44'
            />
            <Select
              items={[
                { value: '0', label: t('All statuses') },
                { value: '1', label: t('Settled') },
                { value: '2', label: t('Unsettled (T+N)') },
                { value: '3', label: t('Reversed') },
              ]}
              value={earnStatus}
              onValueChange={(next) => {
                if (next !== null) {
                  setEarnStatus(next)
                  setEarnPage(1)
                }
              }}
            >
              <SelectTrigger className='w-40'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value='0'>{t('All statuses')}</SelectItem>
                <SelectItem value='1'>{t('Settled')}</SelectItem>
                <SelectItem value='2'>{t('Unsettled (T+N)')}</SelectItem>
                <SelectItem value='3'>{t('Reversed')}</SelectItem>
              </SelectContent>
            </Select>
            <Button
              size='sm'
              variant='outline'
              disabled={settling}
              onClick={handleSettleDue}
            >
              {settling ? t('Settling...') : t('Run Due T+N Settlement')}
            </Button>
          </div>
        </div>

        <Card className='overflow-hidden py-0'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>{t('Merchant')}</TableHead>
                <TableHead>{t('Model')}</TableHead>
                <TableHead className='text-right'>{t('Gross')}</TableHead>
                <TableHead className='text-right'>
                  {t('Merchant Share')}
                </TableHead>
                <TableHead className='text-right'>{t('Commission')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Created / Settle At')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {earnings.map((e) => (
                <TableRow key={e.id}>
                  <TableCell className='font-mono text-xs'>#{e.id}</TableCell>
                  <TableCell className='text-xs'>
                    {e.site_name || e.username || `#${e.merchant_user_id}`}
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {e.model_name}
                  </TableCell>
                  <TableCell className='text-right font-mono text-xs'>
                    {formatFinanceUsd(e.gross_amount || e.amount)}
                  </TableCell>
                  <TableCell className='text-right font-mono text-xs'>
                    {formatFinanceUsd(e.amount)}
                  </TableCell>
                  <TableCell className='text-right font-mono text-xs'>
                    {formatFinanceUsd(e.commission_amount)}
                  </TableCell>
                  <TableCell>
                    {(() => {
                      const badge = getEarningBadgeMeta(e.status)
                      return (
                        <Badge variant={badge.variant}>
                          {t(badge.labelKey)}
                        </Badge>
                      )
                    })()}
                  </TableCell>
                  <TableCell className='text-muted-foreground text-xs'>
                    {formatTimestampToDate(e.created_at)}
                    {e.settle_at > 0 && (
                      <span className='block'>
                        → {formatTimestampToDate(e.settle_at)}
                      </span>
                    )}
                  </TableCell>
                  <TableCell className='text-right'>
                    {e.status !== 3 && (
                      <Button
                        size='sm'
                        variant='ghost'
                        className='text-destructive'
                        onClick={() => {
                          setReversing(e)
                          setReverseReason('')
                        }}
                      >
                        {t('Reverse')}
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {!earnings.length && (
                <TableRow>
                  <TableCell
                    colSpan={9}
                    className='text-muted-foreground py-8 text-center'
                  >
                    {t('No earnings records found')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
          <FinancePagination
            page={earnPage}
            total={earningsQuery.data?.total ?? 0}
            onPageChange={setEarnPage}
          />
        </Card>
      </section>

      <section className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <h3 className='text-sm font-semibold'>
            {t('Merchant Withdrawals & Payouts')}
          </h3>
          <Select
            items={[
              { value: '-1', label: t('All statuses') },
              { value: '0', label: t('Pending') },
              { value: '1', label: t('Approved') },
              { value: '2', label: t('Paid') },
              { value: '3', label: t('Rejected') },
            ]}
            value={wdStatus}
            onValueChange={(next) => {
              if (next !== null) {
                setWdStatus(next)
                setWdPage(1)
              }
            }}
          >
            <SelectTrigger className='w-40'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectItem value='-1'>{t('All statuses')}</SelectItem>
              <SelectItem value='0'>{t('Pending')}</SelectItem>
              <SelectItem value='1'>{t('Approved')}</SelectItem>
              <SelectItem value='2'>{t('Paid')}</SelectItem>
              <SelectItem value='3'>{t('Rejected')}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <Card className='overflow-hidden py-0'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Trade No / Merchant')}</TableHead>
                <TableHead className='text-right'>
                  {t('Amount (USD)')}
                </TableHead>
                <TableHead>{t('Payout Channel & Account')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Created Time')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {withdrawals.map((w) => (
                <TableRow key={w.id}>
                  <TableCell>
                    <p className='font-mono text-xs'>
                      {w.trade_no || `#${w.id}`}
                    </p>
                    <p className='text-muted-foreground text-xs'>
                      {w.shop_name || `Merchant #${w.merchant_user_id}`}
                    </p>
                  </TableCell>
                  <TableCell className='text-right font-mono'>
                    {formatFinanceUsd(w.amount, 2)}
                  </TableCell>
                  <TableCell className='max-w-xs text-xs'>
                    {w.payout_method && (
                      <Badge variant='outline' className='mr-1'>
                        {w.payout_method}
                      </Badge>
                    )}
                    <span className='break-all'>{w.account_info}</span>
                    {w.paid_proof && (
                      <p className='text-muted-foreground mt-0.5'>
                        {t('Proof')}: {w.paid_proof}
                      </p>
                    )}
                  </TableCell>
                  <TableCell>
                    {(() => {
                      const meta = getWithdrawalStatusMeta(w.status)
                      return (
                        <Badge variant={meta.variant}>
                          {t(meta.labelKey)}
                        </Badge>
                      )
                    })()}
                  </TableCell>
                  <TableCell className='text-muted-foreground text-xs'>
                    {formatTimestampToDate(w.created_at)}
                  </TableCell>
                  <TableCell className='text-right'>
                    <div className='flex justify-end gap-1.5'>
                      {w.status === 0 && (
                        <>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() =>
                              handleWithdrawalAction(w.id, 'approve')
                            }
                          >
                            {t('Approve')}
                          </Button>
                          <Button
                            size='sm'
                            variant='ghost'
                            className='text-destructive'
                            onClick={() =>
                              handleWithdrawalAction(w.id, 'reject')
                            }
                          >
                            {t('Reject')}
                          </Button>
                        </>
                      )}
                      {(w.status === 0 || w.status === 1) && (
                        <Button size='sm' onClick={() => setPaying(w)}>
                          {t('Mark Paid')}
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
              {!withdrawals.length && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-muted-foreground py-8 text-center'
                  >
                    {t('No withdrawal requests yet')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
          <FinancePagination
            page={wdPage}
            total={withdrawalsQuery.data?.total ?? 0}
            onPageChange={setWdPage}
          />
        </Card>
      </section>

      <Dialog
        open={!!reversing}
        onOpenChange={(open) => !open && setReversing(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Reverse Merchant Earning')} #{reversing?.id}
            </DialogTitle>
          </DialogHeader>
          <div className='space-y-3'>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Claws back merchant share ({{amount}}) from pending or available balance and writes an offsetting ledger entry.',
                { amount: formatFinanceUsd(reversing?.amount) }
              )}
            </p>
            <div className='space-y-1.5'>
              <Label htmlFor='finance-reverse-reason'>
                {t('Reason (refund / dispute / risk control)')}
              </Label>
              <Input
                id='finance-reverse-reason'
                value={reverseReason}
                onChange={(e) => setReverseReason(e.target.value)}
                placeholder={t('e.g. Chargeback or disputed request')}
              />
            </div>
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setReversing(null)}>
                {t('Cancel')}
              </Button>
              <Button
                variant='destructive'
                disabled={reverseSubmitting}
                onClick={handleReverseConfirm}
              >
                {reverseSubmitting ? t('Processing...') : t('Confirm Reverse')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={!!paying} onOpenChange={(open) => !open && setPaying(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Mark Paid')}: {paying?.trade_no || `#${paying?.id}`}
            </DialogTitle>
          </DialogHeader>
          <div className='space-y-3'>
            <div className='space-y-1.5'>
              <Label htmlFor='finance-paid-proof'>{t('Payment Proof')}</Label>
              <Input
                id='finance-paid-proof'
                value={paidProof}
                onChange={(e) => setPaidProof(e.target.value)}
                placeholder={t('Transaction hash / bank reference')}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='finance-paid-note'>{t('Review Note')}</Label>
              <Input
                id='finance-paid-note'
                value={reviewNote}
                onChange={(e) => setReviewNote(e.target.value)}
              />
            </div>
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setPaying(null)}>
                {t('Cancel')}
              </Button>
              <Button
                onClick={() =>
                  paying &&
                  handleWithdrawalAction(paying.id, 'paid', reviewNote, paidProof)
                }
              >
                {t('Confirm')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
