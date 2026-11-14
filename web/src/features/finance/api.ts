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
import type {
  MerchantWithdrawal,
  PageInfo,
} from '@/features/merchant/types'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  FinanceLedgersPage,
  FinanceMerchantEarningsPage,
  FinanceOverview,
  FinanceReconciliationReport,
  FinanceRedemptionsPage,
  FinanceSubscriptionsPage,
  FinanceTopUpsPage,
} from './types'

async function unwrap<T>(res: { data: unknown }): Promise<T> {
  const payload = requireServerSuccess(res.data) as { data?: unknown }
  return payload.data as T
}

export async function getFinanceOverview(params?: {
  start_timestamp?: number
  end_timestamp?: number
}): Promise<FinanceOverview> {
  return unwrap(
    await api.get('/api/finance/overview', {
      params: {
        ...params,
        timezone_offset: -new Date().getTimezoneOffset() * 60,
      },
    })
  )
}

export async function getFinanceTopUps(params: {
  p: number
  page_size: number
  status?: string
  provider?: string
  keyword?: string
  start_timestamp?: number
  end_timestamp?: number
}): Promise<FinanceTopUpsPage> {
  return unwrap(await api.get('/api/finance/topups', { params }))
}

export async function completeFinanceTopUp(tradeNo: string): Promise<void> {
  requireServerSuccess(
    (await api.post('/api/user/topup/complete', { trade_no: tradeNo })).data
  )
}

export async function getFinanceSubscriptions(params: {
  p: number
  page_size: number
  status?: string
  provider?: string
  keyword?: string
  start_timestamp?: number
  end_timestamp?: number
}): Promise<FinanceSubscriptionsPage> {
  return unwrap(await api.get('/api/finance/subscriptions', { params }))
}

export async function getFinanceRedemptions(params: {
  p: number
  page_size: number
  status?: number
  keyword?: string
}): Promise<FinanceRedemptionsPage> {
  return unwrap(await api.get('/api/finance/redemptions', { params }))
}

export async function getFinanceMerchantEarnings(params: {
  p: number
  page_size: number
  merchant_user_id?: number
  status?: number
  model_name?: string
  start_timestamp?: number
  end_timestamp?: number
}): Promise<FinanceMerchantEarningsPage> {
  return unwrap(await api.get('/api/finance/earnings', { params }))
}

export async function settleDueMerchantEarnings(): Promise<{
  settled_count: number
  settled_amount: number
}> {
  return unwrap(await api.post('/api/finance/earnings/settle'))
}

export async function reverseMerchantEarning(
  id: number,
  reason: string
): Promise<void> {
  requireServerSuccess(
    (await api.post(`/api/finance/earnings/${id}/reverse`, { reason })).data
  )
}

export async function getFinanceLedgers(params: {
  p: number
  page_size: number
  domain?: string
  biz_type?: string
  direction?: string
  user_id?: number
  keyword?: string
  start_timestamp?: number
  end_timestamp?: number
}): Promise<FinanceLedgersPage> {
  return unwrap(await api.get('/api/finance/ledger', { params }))
}

export async function downloadFinanceLedgersCSV(params: {
  domain?: string
  biz_type?: string
  direction?: string
  user_id?: number
  keyword?: string
  start_timestamp?: number
  end_timestamp?: number
}): Promise<Blob> {
  const res = await api.get('/api/finance/ledger/export', {
    params,
    responseType: 'blob',
    skipBusinessError: true,
  })
  return res.data as Blob
}

export async function getFinanceReconciliation(): Promise<FinanceReconciliationReport> {
  return unwrap(await api.get('/api/finance/reconcile'))
}

export async function getFinanceWithdrawals(
  page = 1,
  pageSize = 20,
  status = -1
): Promise<PageInfo<MerchantWithdrawal>> {
  return unwrap(
    await api.get('/api/finance/withdrawals', {
      params: { p: page, page_size: pageSize, status },
    })
  )
}

export async function reviewFinanceWithdrawal(
  id: number,
  body: {
    action: 'approve' | 'reject' | 'paid'
    review_note?: string
    paid_proof?: string
  }
): Promise<MerchantWithdrawal> {
  return unwrap(await api.post(`/api/finance/withdrawals/${id}/review`, body))
}
