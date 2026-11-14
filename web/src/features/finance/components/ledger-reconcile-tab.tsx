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
import { useQuery } from '@tanstack/react-query'
import { Download, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
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
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  downloadFinanceLedgersCSV,
  getFinanceLedgers,
  getFinanceReconciliation,
} from '../api'
import {
  FINANCE_PAGE_SIZE,
  formatFinanceMoney,
  formatFinanceUsd,
} from '../format'
import { FinancePagination } from './shared'

function getDirectionVariant(
  direction: string
): 'default' | 'destructive' | 'secondary' {
  if (direction === 'in' || direction === 'unfreeze') return 'default'
  if (direction === 'out') return 'destructive'
  return 'secondary'
}

export function FinanceLedgerReconcileTab() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [domain, setDomain] = useState('all')
  const [direction, setDirection] = useState('all')
  const [keyword, setKeyword] = useState('')
  const [exporting, setExporting] = useState(false)

  const ledgerQuery = useQuery({
    queryKey: ['finance', 'ledger', page, domain, direction, keyword],
    queryFn: () =>
      getFinanceLedgers({
        p: page,
        page_size: FINANCE_PAGE_SIZE,
        domain: domain === 'all' ? undefined : domain,
        direction: direction === 'all' ? undefined : direction,
        keyword: keyword.trim() || undefined,
      }),
  })

  const reconcileQuery = useQuery({
    queryKey: ['finance', 'reconcile'],
    queryFn: getFinanceReconciliation,
  })

  const items = ledgerQuery.data?.items ?? []
  const reconcile = reconcileQuery.data

  async function handleExportCSV() {
    setExporting(true)
    try {
      const blob = await downloadFinanceLedgersCSV({
        domain: domain === 'all' ? undefined : domain,
        direction: direction === 'all' ? undefined : direction,
        keyword: keyword.trim() || undefined,
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `finance-ledger-${new Date().toISOString().slice(0, 10)}.csv`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      toast.success(t('CSV exported'))
    } catch (error) {
      handleServerError(error, t('Failed to export CSV'))
    } finally {
      setExporting(false)
    }
  }

  return (
    <div className='space-y-5'>
      <Card>
        <CardHeader className='flex flex-row items-center justify-between space-y-0 pb-3'>
          <CardTitle className='flex items-center gap-2 text-base'>
            <ShieldCheck className='size-4' />
            {t('Automated Wallet & Order Reconciliation')}
          </CardTitle>
          <Button
            size='sm'
            variant='outline'
            disabled={reconcileQuery.isFetching}
            onClick={() => reconcileQuery.refetch()}
          >
            {reconcileQuery.isFetching ? t('Checking...') : t('Run Reconcile')}
          </Button>
        </CardHeader>
        <CardContent className='space-y-3'>
          {reconcile && (
            <>
              <Alert variant={reconcile.healthy ? 'default' : 'destructive'}>
                <AlertTitle>
                  {reconcile.healthy
                    ? t('All merchant wallets match earning ledger totals')
                    : t('Found {{count}} wallet balance mismatches', {
                        count: reconcile.wallet_mismatch_count,
                      })}
                </AlertTitle>
                <AlertDescription className='text-xs'>
                  {t('Last check')}: {formatTimestampToDate(reconcile.checked_at)}{' '}
                  · {t('Wallets checked')}: {reconcile.checked_wallets} ·{' '}
                  {t('Due unsettled')}: {reconcile.due_unsettled_count} (
                  {formatFinanceUsd(reconcile.due_unsettled_amount)}) ·{' '}
                  {t('Stale pending top-ups')}:{' '}
                  {reconcile.stale_pending_topups}
                </AlertDescription>
              </Alert>

              {reconcile.wallet_mismatches.length > 0 && (
                <div className='overflow-hidden rounded-lg border'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Merchant')}</TableHead>
                        <TableHead className='text-right'>
                          {t('Wallet Total')}
                        </TableHead>
                        <TableHead className='text-right'>
                          {t('Earning Ledger Total')}
                        </TableHead>
                        <TableHead className='text-right'>{t('Diff')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {reconcile.wallet_mismatches.map((m) => (
                        <TableRow key={m.merchant_user_id}>
                          <TableCell>
                            {m.username || `#${m.merchant_user_id}`}
                          </TableCell>
                          <TableCell className='text-right font-mono'>
                            {formatFinanceUsd(m.wallet_total)}
                          </TableCell>
                          <TableCell className='text-right font-mono'>
                            {formatFinanceUsd(m.earning_total)}
                          </TableCell>
                          <TableCell className='text-destructive text-right font-mono'>
                            {formatFinanceUsd(m.diff)}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>

      <section className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              value={keyword}
              onChange={(e) => {
                setKeyword(e.target.value)
                setPage(1)
              }}
              placeholder={t('Search ledger no, ref ID, or remark')}
              className='w-60'
            />
            <Select
              items={[
                { value: 'all', label: t('All domains') },
                { value: 'user_quota', label: t('User Quota') },
                { value: 'platform_revenue', label: t('Platform Revenue') },
                { value: 'merchant_wallet', label: t('Merchant Wallet') },
              ]}
              value={domain}
              onValueChange={(next) => {
                if (next !== null) {
                  setDomain(next)
                  setPage(1)
                }
              }}
            >
              <SelectTrigger className='w-44'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value='all'>{t('All domains')}</SelectItem>
                <SelectItem value='user_quota'>{t('User Quota')}</SelectItem>
                <SelectItem value='platform_revenue'>
                  {t('Platform Revenue')}
                </SelectItem>
                <SelectItem value='merchant_wallet'>
                  {t('Merchant Wallet')}
                </SelectItem>
              </SelectContent>
            </Select>
            <Select
              items={[
                { value: 'all', label: t('All directions') },
                { value: 'in', label: t('In') },
                { value: 'out', label: t('Out') },
                { value: 'freeze', label: t('Freeze') },
                { value: 'unfreeze', label: t('Unfreeze') },
              ]}
              value={direction}
              onValueChange={(next) => {
                if (next !== null) {
                  setDirection(next)
                  setPage(1)
                }
              }}
            >
              <SelectTrigger className='w-36'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value='all'>{t('All directions')}</SelectItem>
                <SelectItem value='in'>{t('In')}</SelectItem>
                <SelectItem value='out'>{t('Out')}</SelectItem>
                <SelectItem value='freeze'>{t('Freeze')}</SelectItem>
                <SelectItem value='unfreeze'>{t('Unfreeze')}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <Button
            size='sm'
            variant='outline'
            disabled={exporting}
            onClick={handleExportCSV}
          >
            <Download className='mr-1.5 size-3.5' />
            {exporting ? t('Exporting...') : t('Export CSV')}
          </Button>
        </div>

        <Card className='overflow-hidden py-0'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Ledger No / Time')}</TableHead>
                <TableHead>{t('Domain / Biz Type')}</TableHead>
                <TableHead>{t('Direction')}</TableHead>
                <TableHead>{t('User')}</TableHead>
                <TableHead className='text-right'>
                  {t('USD / Cash')}
                </TableHead>
                <TableHead className='text-right'>
                  {t('Before → After')}
                </TableHead>
                <TableHead>{t('Ref / Remark')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((row) => (
                <TableRow key={row.id}>
                  <TableCell className='text-xs'>
                    <p className='font-mono'>{row.ledger_no}</p>
                    <p className='text-muted-foreground'>
                      {formatTimestampToDate(row.created_at)}
                    </p>
                  </TableCell>
                  <TableCell className='text-xs'>
                    <Badge variant='outline' className='mr-1'>
                      {row.domain}
                    </Badge>
                    <span className='font-mono'>{row.biz_type}</span>
                  </TableCell>
                  <TableCell>
                    <Badge variant={getDirectionVariant(row.direction)}>
                      {row.direction}
                    </Badge>
                  </TableCell>
                  <TableCell className='text-xs'>
                    {row.username || (row.user_id ? `#${row.user_id}` : '--')}
                  </TableCell>
                  <TableCell className='text-right font-mono text-xs'>
                    <p>{formatFinanceUsd(row.amount_usd)}</p>
                    {row.money > 0 && (
                      <p className='text-muted-foreground'>
                        {formatFinanceMoney(row.money)} {row.currency}
                      </p>
                    )}
                  </TableCell>
                  <TableCell className='text-right font-mono text-xs'>
                    {row.before_balance.toFixed(4)} →{' '}
                    {row.after_balance.toFixed(4)}
                  </TableCell>
                  <TableCell className='max-w-xs text-xs'>
                    <p className='truncate font-mono'>
                      {row.ref_type} {row.ref_id}
                    </p>
                    {row.remark && (
                      <p className='text-muted-foreground truncate'>
                        {row.remark}
                      </p>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {!items.length && (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className='text-muted-foreground py-8 text-center'
                  >
                    {t('No ledger records found')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
          <FinancePagination
            page={page}
            total={ledgerQuery.data?.total ?? 0}
            onPageChange={setPage}
          />
        </Card>
      </section>
    </div>
  )
}
