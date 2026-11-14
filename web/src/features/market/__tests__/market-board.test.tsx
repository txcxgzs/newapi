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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AnchorHTMLAttributes, ReactNode } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { MarketBoard } from '../components/market-board'
import { StockList } from '../components/stock-list'
import { useMarketKline, useMarketOverview } from '../hooks/use-market-data'

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  Link: ({
    to,
    children,
    ...rest
  }: { to: string; children?: ReactNode } & AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
}))

vi.mock('../components/market-kline-chart', () => ({
  MarketKlineChart: () => <div data-testid='kline-chart' />,
}))

vi.mock('../hooks/use-market-data', () => ({
  useMarketOverview: vi.fn(),
  useMarketKline: vi.fn(),
}))

const useOverviewMock = vi.mocked(useMarketOverview)
const useKlineMock = vi.mocked(useMarketKline)

function renderBoard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <MarketBoard />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  useAuthStore.getState().auth.reset()
  useKlineMock.mockReturnValue({
    data: [],
    isLoading: false,
  } as unknown as ReturnType<typeof useMarketKline>)
})

afterEach(() => {
  useAuthStore.getState().auth.reset()
  vi.clearAllMocks()
})

it('renders a cohesive empty market state instead of split empty boxes when no stocks exist', async () => {
  const user = userEvent.setup()
  const refetch = vi.fn().mockResolvedValue({})
  useOverviewMock.mockReturnValue({
    data: { pools: [], stocks: [] },
    isLoading: false,
    isFetching: false,
    refetch,
  } as unknown as ReturnType<typeof useMarketOverview>)

  renderBoard()

  expect(screen.getByText('No model market data yet')).toBeInTheDocument()
  expect(
    screen.queryByText('Select a model to view its market')
  ).not.toBeInTheDocument()
  expect(screen.queryByText('All Pools')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Model Pricing' })).toHaveAttribute(
    'href',
    '/pricing'
  )

  await user.click(screen.getByRole('button', { name: 'Refresh' }))
  expect(refetch).toHaveBeenCalledTimes(1)
})

it('offers Manage Channels shortcut on empty market for administrators', () => {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'root',
    role: ROLE.SUPER_ADMIN,
    status: 1,
    display_name: 'Root',
  })
  useOverviewMock.mockReturnValue({
    data: { pools: [], stocks: [] },
    isLoading: false,
    isFetching: false,
    refetch: vi.fn(),
  } as unknown as ReturnType<typeof useMarketOverview>)

  renderBoard()

  expect(screen.getByRole('button', { name: 'Manage Channels' })).toHaveAttribute(
    'href',
    '/channels'
  )
})

it('shows pool-specific empty state and resets filter via View All Pools', async () => {
  const user = userEvent.setup()
  useOverviewMock.mockImplementation((pool) => {
    if (pool === 'vip') {
      return {
        data: { pools: ['vip', 'default'], stocks: [] },
        isLoading: false,
        isFetching: false,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useMarketOverview>
    }
    return {
      data: {
        pools: ['vip', 'default'],
        stocks: [
          {
            pool: 'default',
            model: 'gpt-4o',
            last_price: 2.5,
            open_24h: 2.4,
            high_24h: 2.6,
            low_24h: 2.3,
            change_24h_pct: 4.17,
            vol_calls_24h: 100,
            vol_quota_24h: 500000,
            listed_ratio: 1,
            listed_ratio_avg: 1.1,
            listed_input_price: 2.5,
            listed_output_price: 10,
            listed_channels: 2,
          },
        ],
      },
      isLoading: false,
      isFetching: false,
      refetch: vi.fn(),
    } as unknown as ReturnType<typeof useMarketOverview>
  })

  renderBoard()

  expect(screen.getAllByText('gpt-4o').length).toBeGreaterThan(0)
  await user.click(screen.getByRole('button', { name: 'vip' }))
  expect(
    await screen.findByText('No transaction data in pool "vip" yet')
  ).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'View All Pools' }))
  expect((await screen.findAllByText('gpt-4o')).length).toBeGreaterThan(0)
})

it('preserves flex centering classes in empty StockList even with custom className', () => {
  render(
    <StockList
      stocks={[]}
      selectedPool=''
      selectedModel=''
      onSelect={vi.fn()}
      className='max-h-[560px]'
    />
  )

  const emptyEl = screen.getByText('No transaction data yet').parentElement
  expect(emptyEl).toHaveClass('flex', 'items-center', 'justify-center', 'max-h-[560px]')
})
