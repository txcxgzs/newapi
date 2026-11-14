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

import { Badge } from '@/components/ui/badge'
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

import { getFinanceRedemptions, getFinanceSubscriptions } from '../api'
import {
  FINANCE_PAGE_SIZE,
  formatFinanceMoney,
  formatFinanceUsd,
} from '../format'
import { FinancePagination } from './shared'

function getRedemptionBadgeMeta(status: number): {
  variant: 'default' | 'secondary' | 'destructive'
  labelKey: string
} {
  if (status === 3) return { variant: 'default', labelKey: 'Used' }
  if (status === 1) return { variant: 'secondary', labelKey: 'Unused' }
  return { variant: 'destructive', labelKey: 'Disabled' }
}

export function FinanceSubscriptionsRedemptionsTab() {
  const { t } = useTranslation()
  const [subPage, setSubPage] = useState(1)
  const [subStatus, setSubStatus] = useState('all')
  const [subKeyword, setSubKeyword] = useState('')

  const [redPage, setRedPage] = useState(1)
  const [redStatus, setRedStatus] = useState('0')
  const [redKeyword, setRedKeyword] = useState('')

  const subsQuery = useQuery({
    queryKey: ['finance', 'subscriptions', subPage, subStatus, subKeyword],
    queryFn: () =>
      getFinanceSubscriptions({
        p: subPage,
        page_size: FINANCE_PAGE_SIZE,
        status: subStatus === 'all' ? undefined : subStatus,
        keyword: subKeyword.trim() || undefined,
      }),
  })

  const redQuery = useQuery({
    queryKey: ['finance', 'redemptions', redPage, redStatus, redKeyword],
    queryFn: () =>
      getFinanceRedemptions({
        p: redPage,
        page_size: FINANCE_PAGE_SIZE,
        status: Number(redStatus) || undefined,
        keyword: redKeyword.trim() || undefined,
      }),
  })

  const subItems = subsQuery.data?.items ?? []
  const redItems = redQuery.data?.items ?? []

  return (
    <div className='space-y-6'>
      <section className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <h3 className='text-sm font-semibold'>{t('Subscription Orders')}</h3>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              value={subKeyword}
              onChange={(e) => {
                setSubKeyword(e.target.value)
                setSubPage(1)
              }}
              placeholder={t('Search trade no or payment method')}
              className='w-56'
            />
            <Select
              items={[
                { value: 'all', label: t('All statuses') },
                { value: 'success', label: t('Completed') },
                { value: 'pending', label: t('Pending') },
              ]}
              value={subStatus}
              onValueChange={(next) => {
                if (next !== null) {
                  setSubStatus(next)
                  setSubPage(1)
                }
              }}
            >
              <SelectTrigger className='w-36'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value='all'>{t('All statuses')}</SelectItem>
                <SelectItem value='success'>{t('Completed')}</SelectItem>
                <SelectItem value='pending'>{t('Pending')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        <Card className='overflow-hidden py-0'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Trade No')}</TableHead>
                <TableHead>{t('User')}</TableHead>
                <TableHead>{t('Plan')}</TableHead>
                <TableHead>{t('Method / Provider')}</TableHead>
                <TableHead className='text-right'>{t('Amount')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Created Time')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {subItems.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className='font-mono text-xs'>
                    {item.trade_no}
                  </TableCell>
                  <TableCell>
                    {item.username || `#${item.user_id}`}
                  </TableCell>
                  <TableCell>
                    {item.plan_title || `#${item.plan_id}`}
                  </TableCell>
                  <TableCell className='text-xs'>
                    {item.payment_method || '--'} /{' '}
                    {item.payment_provider || '--'}
                  </TableCell>
                  <TableCell className='text-right font-mono'>
                    {formatFinanceMoney(item.money)}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        item.status === 'success' ? 'default' : 'secondary'
                      }
                    >
                      {item.status}
                    </Badge>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-xs'>
                    {formatTimestampToDate(item.create_time)}
                  </TableCell>
                </TableRow>
              ))}
              {!subItems.length && (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className='text-muted-foreground py-8 text-center'
                  >
                    {t('No subscription orders found')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
          <FinancePagination
            page={subPage}
            total={subsQuery.data?.total ?? 0}
            onPageChange={setSubPage}
          />
        </Card>
      </section>

      <section className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <h3 className='text-sm font-semibold'>
            {t('Redemption Issuance & Usage')}
          </h3>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              value={redKeyword}
              onChange={(e) => {
                setRedKeyword(e.target.value)
                setRedPage(1)
              }}
              placeholder={t('Search code name')}
              className='w-56'
            />
            <Select
              items={[
                { value: '0', label: t('All statuses') },
                { value: '1', label: t('Unused') },
                { value: '3', label: t('Used') },
                { value: '2', label: t('Disabled') },
              ]}
              value={redStatus}
              onValueChange={(next) => {
                if (next !== null) {
                  setRedStatus(next)
                  setRedPage(1)
                }
              }}
            >
              <SelectTrigger className='w-36'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value='0'>{t('All statuses')}</SelectItem>
                <SelectItem value='1'>{t('Unused')}</SelectItem>
                <SelectItem value='3'>{t('Used')}</SelectItem>
                <SelectItem value='2'>{t('Disabled')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>

        <Card className='overflow-hidden py-0'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Code Name')}</TableHead>
                <TableHead>{t('Creator')}</TableHead>
                <TableHead className='text-right'>{t('Value (USD)')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Redeemed By')}</TableHead>
                <TableHead>{t('Redeemed Time')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {redItems.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className='font-medium'>{item.name}</TableCell>
                  <TableCell className='text-xs'>
                    {item.creator_name || `#${item.user_id}`}
                  </TableCell>
                  <TableCell className='text-right font-mono'>
                    {formatFinanceUsd(item.amount_usd, 2)}
                  </TableCell>
                  <TableCell>
                    {(() => {
                      const badge = getRedemptionBadgeMeta(item.status)
                      return (
                        <Badge variant={badge.variant}>
                          {t(badge.labelKey)}
                        </Badge>
                      )
                    })()}
                  </TableCell>
                  <TableCell className='text-xs'>
                    {item.used_user_id > 0
                      ? item.used_username || `#${item.used_user_id}`
                      : '--'}
                  </TableCell>
                  <TableCell className='text-muted-foreground text-xs'>
                    {item.redeemed_time > 0
                      ? formatTimestampToDate(item.redeemed_time)
                      : '--'}
                  </TableCell>
                </TableRow>
              ))}
              {!redItems.length && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-muted-foreground py-8 text-center'
                  >
                    {t('No redemption codes found')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
          <FinancePagination
            page={redPage}
            total={redQuery.data?.total ?? 0}
            onPageChange={setRedPage}
          />
        </Card>
      </section>
    </div>
  )
}
