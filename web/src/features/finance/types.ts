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
import type { PageInfo } from '@/features/merchant/types'

export interface FinanceTimeWindow {
  start_timestamp: number
  end_timestamp: number
}

export interface FinanceProviderBreakdown {
  provider: string
  orders: number
  money: number
}

export interface FinanceOverview {
  window: FinanceTimeWindow
  topup_stats: {
    total_orders: number
    success_orders: number
    pending_orders: number
    total_money: number
    total_amount_usd: number
    period_orders: number
    period_money: number
    period_amount_usd: number
    provider_breakdown: FinanceProviderBreakdown[]
  }
  subscription_stats: {
    total_paid_orders: number
    total_revenue: number
    period_revenue: number
    active_users: number
  }
  redemption_stats: {
    total_codes: number
    used_codes: number
    unused_codes: number
    total_usd: number
    used_usd: number
    unused_usd: number
  }
  consume_stats: {
    period_calls: number
    period_spend_usd: number
    all_time_usd: number
  }
  user_wallet_stats: {
    total_users: number
    total_remain_usd: number
    total_used_usd: number
  }
  merchant_stats: {
    available_balance: number
    pending_balance: number
    frozen_balance: number
    total_earned: number
    total_withdrawn: number
    total_commission: number
    period_gross_usd: number
    period_merchant_earning: number
    period_platform_commission: number
    pending_withdrawal_count: number
    pending_withdrawal_amount: number
  }
  daily_series: FinanceDailyPoint[]
}

export interface FinanceDailyPoint {
  day: number
  topup_money: number
  subscription_money: number
  merchant_earning: number
  platform_commission: number
  withdrawn_usd: number
}

export interface FinanceTopUpOrder {
  id: number
  user_id: number
  username: string
  amount: number
  money: number
  trade_no: string
  payment_method: string
  payment_provider: string
  create_time: number
  complete_time: number
  status: string
}

export interface FinanceSubscriptionOrder {
  id: number
  user_id: number
  username: string
  plan_id: number
  plan_title: string
  money: number
  trade_no: string
  payment_method: string
  payment_provider: string
  status: string
  create_time: number
  complete_time: number
  provider_payload: string
}

export interface FinanceRedemptionItem {
  id: number
  name: string
  status: number
  quota: number
  amount_usd: number
  user_id: number
  creator_name: string
  used_user_id: number
  used_username: string
  created_time: number
  redeemed_time: number
  expired_time: number
}

export interface FinanceMerchantEarning {
  id: number
  merchant_user_id: number
  username: string
  site_name: string
  channel_id: number
  log_id: number
  gross_amount: number
  amount: number
  commission_amount: number
  status: number
  settle_at: number
  settled_at: number
  model_name: string
  created_at: number
}

export type FinanceLedgerDomain =
  | 'user_quota'
  | 'platform_revenue'
  | 'merchant_wallet'
  | string

export type FinanceLedgerDirection =
  | 'in'
  | 'out'
  | 'freeze'
  | 'unfreeze'
  | string

export interface FinanceLedgerRecord {
  id: number
  ledger_no: string
  domain: FinanceLedgerDomain
  user_id: number
  username: string
  biz_type: string
  direction: FinanceLedgerDirection
  amount_usd: number
  money: number
  currency: string
  quota_delta: number
  before_balance: number
  after_balance: number
  ref_type: string
  ref_id: string
  operator_id: number
  operator_name: string
  remark: string
  created_at: number
}

export interface FinanceWalletMismatch {
  merchant_user_id: number
  username: string
  wallet_total: number
  earning_total: number
  diff: number
  pending_balance: number
  expected_pending: number
}

export interface FinanceReconciliationReport {
  checked_at: number
  checked_wallets: number
  wallet_mismatch_count: number
  wallet_mismatches: FinanceWalletMismatch[]
  due_unsettled_count: number
  due_unsettled_amount: number
  stale_pending_topups: number
  finance_ledger_records: number
  healthy: boolean
}

export type FinanceTopUpsPage = PageInfo<FinanceTopUpOrder>
export type FinanceSubscriptionsPage = PageInfo<FinanceSubscriptionOrder>
export type FinanceRedemptionsPage = PageInfo<FinanceRedemptionItem>
export type FinanceMerchantEarningsPage = PageInfo<FinanceMerchantEarning>
export type FinanceLedgersPage = PageInfo<FinanceLedgerRecord>
