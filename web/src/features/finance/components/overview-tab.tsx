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
import {
  Activity,
  ArrowRight,
  Coins,
  CreditCard,
  Landmark,
  QrCode,
  TrendingUp,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from '@/components/ui/chart'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { handleServerError } from '@/lib/handle-server-error'
import { formatDate } from '@/lib/time'

import {
  getFinanceOverview,
  getFinanceReconciliation,
  settleDueMerchantEarnings,
} from '../api'
import { formatFinanceMoney, formatFinanceUsd } from '../format'

const WINDOW_PRESETS = [
  { value: '7', days: 7, labelKey: '7 Days' },
  { value: '30', days: 30, labelKey: '30 Days' },
  { value: '90', days: 90, labelKey: '90 Days' },
] as const

export function FinanceOverviewTab({
  onOpenWithdrawals,
}: {
  onOpenWithdrawals?: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [windowDays, setWindowDays] = useState('30')
  const [settling, setSettling] = useState(false)

  const range = useMemo(() => {
    const days = Number(windowDays) || 30
    const end = Math.floor(Date.now() / 1000)
    const start = end - days * 86400
    return { start_timestamp: start, end_timestamp: end }
  }, [windowDays])

  const overviewQuery = useQuery({
    queryKey: ['finance', 'overview', windowDays],
    queryFn: () => getFinanceOverview(range),
  })

  const reconcileQuery = useQuery({
    queryKey: ['finance', 'reconcile'],
    queryFn: getFinanceReconciliation,
  })

  const overview = overviewQuery.data
  const reconcile = reconcileQuery.data

  const chartData = useMemo(
    () =>
      (overview?.daily_series ?? []).map((item) => ({
        date: formatDate(item.day).slice(5),
        topup_money: Number(item.topup_money.toFixed(2)),
        subscription_money: Number(item.subscription_money.toFixed(2)),
        platform_commission: Number(item.platform_commission.toFixed(4)),
        merchant_earning: Number(item.merchant_earning.toFixed(4)),
      })),
    [overview?.daily_series]
  )

  async function handleRunSettlement() {
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

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <ToggleGroup
          value={[windowDays]}
          onValueChange={(vals) => {
            if (vals[0]) setWindowDays(vals[0])
          }}
          variant='outline'
          size='sm'
        >
          {WINDOW_PRESETS.map((preset) => (
            <ToggleGroupItem key={preset.value} value={preset.value}>
              {t(preset.labelKey)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>

        <div className='flex items-center gap-2'>
          {(reconcile?.due_unsettled_count ?? 0) > 0 && (
            <Button
              size='sm'
              variant='outline'
              disabled={settling}
              onClick={handleRunSettlement}
            >
              {settling
                ? t('Settling...')
                : t('Settle Due T+N ({{count}})', {
                    count: reconcile?.due_unsettled_count ?? 0,
                  })}
            </Button>
          )}
        </div>
      </div>

      {reconcile && (
        <Alert variant={reconcile.healthy ? 'default' : 'destructive'}>
          <AlertTitle>
            {reconcile.healthy
              ? t('Ledger Reconciliation Healthy')
              : t('Reconciliation Discrepancy Detected')}
          </AlertTitle>
          <AlertDescription className='flex flex-wrap items-center gap-3 text-xs'>
            <span>
              {t('Checked wallets')}: {reconcile.checked_wallets}
            </span>
            <span>
              {t('Mismatches')}: {reconcile.wallet_mismatch_count}
            </span>
            <span>
              {t('Due T+N pending')}: {reconcile.due_unsettled_count} (
              {formatFinanceUsd(reconcile.due_unsettled_amount)})
            </span>
            <span>
              {t('Stale pending top-ups (>24h)')}:{' '}
              {reconcile.stale_pending_topups}
            </span>
            <span>
              {t('Immutable ledger entries')}:{' '}
              {reconcile.finance_ledger_records}
            </span>
          </AlertDescription>
        </Alert>
      )}

      {(overview?.merchant_stats.pending_withdrawal_count ?? 0) > 0 && (
        <div className='flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm'>
          <div className='flex items-center gap-2.5'>
            <div className='flex size-8 items-center justify-center rounded-lg bg-amber-500/20 text-amber-700 dark:text-amber-300'>
              <QrCode className='size-4' />
            </div>
            <div>
              <p className='font-medium text-amber-900 dark:text-amber-200'>
                {t('{{count}} withdrawal request(s) awaiting review or QR payout', {
                  count: overview?.merchant_stats.pending_withdrawal_count ?? 0,
                })}
              </p>
              <p className='text-xs text-amber-800/80 dark:text-amber-300/80'>
                {t('Pending total amount')}:{' '}
                <strong className='font-mono'>
                  {formatFinanceUsd(
                    overview?.merchant_stats.pending_withdrawal_amount,
                    2
                  )}
                </strong>
              </p>
            </div>
          </div>
          {onOpenWithdrawals && (
            <Button size='sm' onClick={onOpenWithdrawals}>
              {t('Open Withdrawal Audit')}
              <ArrowRight className='ml-1.5 size-3.5' />
            </Button>
          )}
        </div>
      )}

      <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-4'>
        <Card className='border-emerald-500/20 bg-gradient-to-br from-emerald-500/5 via-transparent to-transparent'>
          <CardHeader className='flex flex-row items-center justify-between pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {t('Period Top-up Revenue')}
            </CardTitle>
            <TrendingUp className='size-4 text-emerald-500' />
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-bold tabular-nums'>
              {formatFinanceMoney(overview?.topup_stats.period_money)}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('All-time')}:{' '}
              {formatFinanceMoney(overview?.topup_stats.total_money)} ·{' '}
              {t('Orders')}: {overview?.topup_stats.period_orders ?? 0} (
              {t('Pending')}: {overview?.topup_stats.pending_orders ?? 0})
            </p>
          </CardContent>
        </Card>

        <Card className='border-sky-500/20 bg-gradient-to-br from-sky-500/5 via-transparent to-transparent'>
          <CardHeader className='flex flex-row items-center justify-between pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {t('Subscription Revenue')}
            </CardTitle>
            <CreditCard className='size-4 text-sky-500' />
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-bold tabular-nums'>
              {formatFinanceMoney(overview?.subscription_stats.period_revenue)}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('All-time')}:{' '}
              {formatFinanceMoney(overview?.subscription_stats.total_revenue)} ·{' '}
              {t('Active subscribers')}:{' '}
              {overview?.subscription_stats.active_users ?? 0}
            </p>
          </CardContent>
        </Card>

        <Card className='border-violet-500/20 bg-gradient-to-br from-violet-500/5 via-transparent to-transparent'>
          <CardHeader className='flex flex-row items-center justify-between pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {t('API Consumption & User Float')}
            </CardTitle>
            <Activity className='size-4 text-violet-500' />
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-bold tabular-nums'>
              {formatFinanceUsd(overview?.consume_stats.period_spend_usd)}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Calls')}: {overview?.consume_stats.period_calls ?? 0} ·{' '}
              {t('User wallet float')}:{' '}
              {formatFinanceUsd(overview?.user_wallet_stats.total_remain_usd)}
            </p>
          </CardContent>
        </Card>

        <Card className='border-amber-500/20 bg-gradient-to-br from-amber-500/5 via-transparent to-transparent'>
          <CardHeader className='flex flex-row items-center justify-between pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {t('Platform Commission & Merchant Custody')}
            </CardTitle>
            <Landmark className='size-4 text-amber-500' />
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-bold tabular-nums'>
              {formatFinanceUsd(overview?.merchant_stats.total_commission)}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Merchant available')}:{' '}
              {formatFinanceUsd(overview?.merchant_stats.available_balance)} ·{' '}
              {t('T+N pending')}:{' '}
              {formatFinanceUsd(overview?.merchant_stats.pending_balance)} ·{' '}
              {t('Frozen')}:{' '}
              {formatFinanceUsd(overview?.merchant_stats.frozen_balance)}
            </p>
          </CardContent>
        </Card>
      </div>

      <div className='grid gap-4 lg:grid-cols-3'>
        <Card className='lg:col-span-2'>
          <CardHeader className='flex flex-row items-center justify-between'>
            <CardTitle className='flex items-center gap-2 text-base'>
              <Coins className='text-primary size-4' />
              {t('Daily Cashflow, Merchant Share & Commission Trend')}
            </CardTitle>
            <Badge variant='outline' className='text-xs'>
              {t('{{days}} Days', { days: windowDays })}
            </Badge>
          </CardHeader>
          <CardContent>
            {chartData.length > 0 ? (
              <ChartContainer
                className='h-64 w-full'
                config={{
                  topup_money: {
                    label: t('Top-up Cash'),
                    color: 'hsl(var(--chart-1))',
                  },
                  subscription_money: {
                    label: t('Subscription Cash'),
                    color: 'hsl(var(--chart-2))',
                  },
                  merchant_earning: {
                    label: t('Merchant Share (USD)'),
                    color: 'hsl(var(--chart-4))',
                  },
                  platform_commission: {
                    label: t('Platform Commission'),
                    color: 'hsl(var(--chart-3))',
                  },
                }}
              >
                <BarChart data={chartData}>
                  <CartesianGrid vertical={false} />
                  <XAxis dataKey='date' tickLine={false} axisLine={false} />
                  <YAxis tickLine={false} axisLine={false} width={56} />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <Bar
                    dataKey='topup_money'
                    fill='var(--color-topup_money)'
                    radius={[4, 4, 0, 0]}
                  />
                  <Bar
                    dataKey='subscription_money'
                    fill='var(--color-subscription_money)'
                    radius={[4, 4, 0, 0]}
                  />
                  <Bar
                    dataKey='merchant_earning'
                    fill='var(--color-merchant_earning)'
                    radius={[4, 4, 0, 0]}
                  />
                  <Bar
                    dataKey='platform_commission'
                    fill='var(--color-platform_commission)'
                    radius={[4, 4, 0, 0]}
                  />
                </BarChart>
              </ChartContainer>
            ) : (
              <p className='text-muted-foreground py-12 text-center text-sm'>
                {t('No financial activity in this period')}
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className='text-base'>
              {t('Channel Breakdown & Redemptions')}
            </CardTitle>
          </CardHeader>
          <CardContent className='space-y-4'>
            <div className='space-y-2'>
              <p className='text-muted-foreground text-xs font-medium'>
                {t('Top-up Providers')}
              </p>
              {(overview?.topup_stats.provider_breakdown ?? []).map((item) => (
                <div
                  key={item.provider || 'unknown'}
                  className='flex items-center justify-between text-sm'
                >
                  <Badge variant='secondary'>
                    {item.provider || t('Default')}
                  </Badge>
                  <span className='font-mono text-xs'>
                    {item.orders} {t('orders')} ·{' '}
                    {formatFinanceMoney(item.money)}
                  </span>
                </div>
              ))}
              {!overview?.topup_stats.provider_breakdown?.length && (
                <p className='text-muted-foreground text-xs'>
                  {t('No paid top-up orders')}
                </p>
              )}
            </div>

            <div className='space-y-1.5 border-t pt-3 text-xs'>
              <p className='text-muted-foreground font-medium'>
                {t('Redemption Codes Summary')}
              </p>
              <div className='flex justify-between'>
                <span>{t('Used / Total codes')}</span>
                <span className='font-mono'>
                  {overview?.redemption_stats.used_codes ?? 0} /{' '}
                  {overview?.redemption_stats.total_codes ?? 0}
                </span>
              </div>
              <div className='flex justify-between'>
                <span>{t('Redeemed value')}</span>
                <span className='font-mono'>
                  {formatFinanceUsd(overview?.redemption_stats.used_usd)}
                </span>
              </div>
              <div className='flex justify-between'>
                <span>{t('Unused liability')}</span>
                <span className='font-mono'>
                  {formatFinanceUsd(overview?.redemption_stats.unused_usd)}
                </span>
              </div>
              <div className='flex justify-between'>
                <span>{t('Pending merchant withdrawals')}</span>
                <span className='font-mono'>
                  {overview?.merchant_stats.pending_withdrawal_count ?? 0} (
                  {formatFinanceUsd(
                    overview?.merchant_stats.pending_withdrawal_amount
                  )}
                  )
                </span>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
