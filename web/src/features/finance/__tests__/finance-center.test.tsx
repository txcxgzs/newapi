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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  completeFinanceTopUp,
  getFinanceOverview,
  getFinanceReconciliation,
  getFinanceTopUps,
  getFinanceWithdrawals,
  reviewFinanceWithdrawal,
  settleDueMerchantEarnings,
} from '../api'
import { FinanceCenter } from '../index'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, string | number>) => {
      if (!opts) return key
      return key.replaceAll(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? ''))
    },
  }),
}))

vi.mock('../api', () => ({
  getFinanceOverview: vi.fn(),
  getFinanceReconciliation: vi.fn(),
  getFinanceTopUps: vi.fn(),
  completeFinanceTopUp: vi.fn(),
  getFinanceSubscriptions: vi.fn().mockResolvedValue({
    items: [],
    total: 0,
    page: 1,
    page_size: 20,
  }),
  getFinanceRedemptions: vi.fn().mockResolvedValue({
    items: [],
    total: 0,
    page: 1,
    page_size: 20,
  }),
  getFinanceMerchantEarnings: vi.fn().mockResolvedValue({
    items: [],
    total: 0,
    page: 1,
    page_size: 20,
  }),
  getFinanceWithdrawals: vi.fn(),
  reviewFinanceWithdrawal: vi.fn(),
  settleDueMerchantEarnings: vi.fn(),
  reverseMerchantEarning: vi.fn(),
  getFinanceLedgers: vi.fn().mockResolvedValue({
    items: [],
    total: 0,
    page: 1,
    page_size: 20,
  }),
  downloadFinanceLedgersCSV: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

const overviewMock = vi.mocked(getFinanceOverview)
const reconcileMock = vi.mocked(getFinanceReconciliation)
const topupsMock = vi.mocked(getFinanceTopUps)
const completeTopupMock = vi.mocked(completeFinanceTopUp)
const settleMock = vi.mocked(settleDueMerchantEarnings)
const withdrawalsMock = vi.mocked(getFinanceWithdrawals)
const reviewWithdrawalMock = vi.mocked(reviewFinanceWithdrawal)

function renderFinance() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <FinanceCenter />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  overviewMock.mockResolvedValue({
    window: { start_timestamp: 1000, end_timestamp: 2000 },
    topup_stats: {
      total_orders: 10,
      success_orders: 8,
      pending_orders: 2,
      total_money: 500,
      total_amount_usd: 500,
      period_orders: 4,
      period_money: 200,
      period_amount_usd: 200,
      provider_breakdown: [{ provider: 'alipay', orders: 4, money: 200 }],
    },
    subscription_stats: {
      total_paid_orders: 3,
      total_revenue: 90,
      period_revenue: 30,
      active_users: 2,
    },
    redemption_stats: {
      total_codes: 5,
      used_codes: 3,
      unused_codes: 2,
      total_usd: 100,
      used_usd: 60,
      unused_usd: 40,
    },
    consume_stats: {
      period_calls: 120,
      period_spend_usd: 42.5,
      all_time_usd: 120,
    },
    user_wallet_stats: {
      total_users: 15,
      total_remain_usd: 320,
      total_used_usd: 120,
    },
    merchant_stats: {
      available_balance: 18,
      pending_balance: 5,
      frozen_balance: 4,
      total_earned: 27,
      total_withdrawn: 0,
      total_commission: 1.42,
      period_gross_usd: 28.42,
      period_merchant_earning: 27,
      period_platform_commission: 1.42,
      pending_withdrawal_count: 1,
      pending_withdrawal_amount: 4,
    },
    daily_series: [],
  })
  reconcileMock.mockResolvedValue({
    checked_at: 1700000000,
    checked_wallets: 2,
    wallet_mismatch_count: 0,
    wallet_mismatches: [],
    due_unsettled_count: 2,
    due_unsettled_amount: 5,
    stale_pending_topups: 1,
    finance_ledger_records: 24,
    healthy: true,
  })
  topupsMock.mockResolvedValue({
    items: [
      {
        id: 1,
        user_id: 9,
        username: 'alice',
        amount: 10,
        money: 10,
        trade_no: 'TN-PENDING-001',
        payment_method: 'alipay',
        payment_provider: 'epay',
        create_time: 1700000000,
        complete_time: 0,
        status: 'pending',
      },
    ],
    total: 1,
    page: 1,
    page_size: 20,
  })
  completeTopupMock.mockResolvedValue()
  settleMock.mockResolvedValue({ settled_count: 2, settled_amount: 5 })
  const dummyQr = 'data:image/png;base64,iVBORw0KGgo='
  withdrawalsMock.mockResolvedValue({
    items: [
      {
        id: 7,
        trade_no: 'WD-2026-007',
        merchant_user_id: 21,
        shop_name: 'AcmeRelayShop',
        merchant_code: 'm21',
        merchant_username: 'acme',
        amount: 88.5,
        status: 1,
        payout_method: 'alipay',
        account_info: 'alipay@acme.example',
        payout_qrcode: dummyQr,
        note: 'monthly payout',
        review_note: 'approved first',
        paid_proof: '',
        created_at: 1700000000,
        paid_at: 0,
      },
    ],
    total: 1,
    page: 1,
    page_size: 20,
  })
  reviewWithdrawalMock.mockResolvedValue({
    id: 7,
    trade_no: 'WD-2026-007',
    merchant_user_id: 21,
    amount: 88.5,
    status: 2,
    account_info: 'alipay@acme.example',
    note: '',
    review_note: '',
    paid_proof: 'ALIPAY-999',
    created_at: 1700000000,
    paid_at: 1700001000,
  })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('FinanceCenter', () => {
  it('renders financial overview, reconciliation status, and triggers due T+N settlement', async () => {
    const user = userEvent.setup()
    renderFinance()

    expect(
      await screen.findByText('Ledger Reconciliation Healthy')
    ).toBeInTheDocument()
    const settleBtn = screen.getByRole('button', {
      name: 'Settle Due T+N (2)',
    })
    await user.click(settleBtn)

    await waitFor(() => expect(settleMock).toHaveBeenCalledTimes(1))
  })

  it('allows manually completing a pending top-up order from Top-up Orders tab', async () => {
    const user = userEvent.setup()
    renderFinance()

    await user.click(screen.getByRole('tab', { name: 'Top-up Orders' }))
    expect(await screen.findByText('TN-PENDING-001')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Manual Complete' }))
    await waitFor(() =>
      expect(completeTopupMock).toHaveBeenCalledWith('TN-PENDING-001')
    )
  })

  it('opens the Withdrawals & QR Payouts tab, renders snapshotted QR modal, and marks withdrawal paid', async () => {
    const user = userEvent.setup()
    renderFinance()

    const jumpBtn = await screen.findByRole('button', {
      name: 'Open Withdrawal Audit',
    })
    await user.click(jumpBtn)

    expect(await screen.findByText('WD-2026-007')).toBeInTheDocument()
    expect(screen.getByText('AcmeRelayShop')).toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: 'Scan QR & Confirm Paid' })
    )
    expect(await screen.findByText('Scan QR & Confirm Payout')).toBeInTheDocument()
    expect(
      screen.getAllByAltText('Payout QR Code').length
    ).toBeGreaterThanOrEqual(1)

    await user.type(
      screen.getByLabelText('Payment Proof / Transaction ID'),
      'ALIPAY-999'
    )
    await user.click(screen.getByRole('button', { name: 'Confirm Paid' }))

    await waitFor(() =>
      expect(reviewWithdrawalMock).toHaveBeenCalledWith(7, {
        action: 'paid',
        paid_proof: 'ALIPAY-999',
        review_note: 'approved first',
      })
    )
  })
})
