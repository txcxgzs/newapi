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

import { completeFinanceTopUp, getFinanceTopUps } from '../api'
import {
  FINANCE_PAGE_SIZE,
  formatFinanceMoney,
  formatFinanceUsd,
} from '../format'
import { FinancePagination } from './shared'

export function FinanceTopUpsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState('all')
  const [keyword, setKeyword] = useState('')
  const [completingTradeNo, setCompletingTradeNo] = useState<string | null>(null)

  const query = useQuery({
    queryKey: ['finance', 'topups', page, status, keyword],
    queryFn: () =>
      getFinanceTopUps({
        p: page,
        page_size: FINANCE_PAGE_SIZE,
        status: status === 'all' ? undefined : status,
        keyword: keyword.trim() || undefined,
      }),
  })

  const items = query.data?.items ?? []

  async function handleComplete(tradeNo: string) {
    setCompletingTradeNo(tradeNo)
    try {
      await completeFinanceTopUp(tradeNo)
      toast.success(t('Order completed manually'))
      queryClient.invalidateQueries({ queryKey: ['finance'] })
    } catch (error) {
      handleServerError(error, t('Failed to complete order'))
    } finally {
      setCompletingTradeNo(null)
    }
  }

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <Input
          value={keyword}
          onChange={(e) => {
            setKeyword(e.target.value)
            setPage(1)
          }}
          placeholder={t('Search trade no or payment method')}
          className='w-full sm:w-64'
        />
        <Select
          items={[
            { value: 'all', label: t('All statuses') },
            { value: 'pending', label: t('Pending') },
            { value: 'success', label: t('Completed') },
          ]}
          value={status}
          onValueChange={(next) => {
            if (next !== null) {
              setStatus(next)
              setPage(1)
            }
          }}
        >
          <SelectTrigger className='w-40'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectItem value='all'>{t('All statuses')}</SelectItem>
            <SelectItem value='pending'>{t('Pending')}</SelectItem>
            <SelectItem value='success'>{t('Completed')}</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <Card className='overflow-hidden py-0'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Trade No')}</TableHead>
              <TableHead>{t('User')}</TableHead>
              <TableHead>{t('Method / Provider')}</TableHead>
              <TableHead className='text-right'>{t('Paid Cash')}</TableHead>
              <TableHead className='text-right'>{t('Credit (USD)')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead>{t('Created Time')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='font-mono text-xs'>
                  {item.trade_no}
                </TableCell>
                <TableCell>
                  {item.username || `#${item.user_id}`}
                </TableCell>
                <TableCell className='text-xs'>
                  {item.payment_method || '--'} /{' '}
                  {item.payment_provider || '--'}
                </TableCell>
                <TableCell className='text-right font-mono'>
                  {formatFinanceMoney(item.money)}
                </TableCell>
                <TableCell className='text-right font-mono'>
                  {formatFinanceUsd(item.amount, 2)}
                </TableCell>
                <TableCell>
                  <Badge
                    variant={
                      item.status === 'success' ? 'default' : 'secondary'
                    }
                  >
                    {item.status === 'success' ? t('Completed') : item.status}
                  </Badge>
                </TableCell>
                <TableCell className='text-muted-foreground text-xs'>
                  {formatTimestampToDate(item.create_time)}
                </TableCell>
                <TableCell className='text-right'>
                  {item.status !== 'success' && (
                    <Button
                      size='sm'
                      variant='outline'
                      disabled={completingTradeNo === item.trade_no}
                      onClick={() => handleComplete(item.trade_no)}
                    >
                      {completingTradeNo === item.trade_no
                        ? t('Processing...')
                        : t('Manual Complete')}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
            {!items.length && (
              <TableRow>
                <TableCell
                  colSpan={8}
                  className='text-muted-foreground py-8 text-center'
                >
                  {t('No top-up orders found')}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        <FinancePagination
          page={page}
          total={query.data?.total ?? 0}
          onPageChange={setPage}
        />
      </Card>
    </div>
  )
}
