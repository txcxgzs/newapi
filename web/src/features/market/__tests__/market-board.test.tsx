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
import {
  useMarketKline,
  useMarketOrderBook,
  useMarketOverview,
} from '../hooks/use-market-data'

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
  useMarketOrderBook: vi.fn(),
}))

const useOverviewMock = vi.mocked(useMarketOverview)
const useKlineMock = vi.mocked(useMarketKline)
const useOrderBookMock = vi.mocked(useMarketOrderBook)

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
  useOrderBookMock.mockReturnValue({
    data: { strategy: 'price_ttft', entries: [], updated_at: 1700000000 },
    isLoading: false,
    isFetching: false,
    refetch: vi.fn(),
  } as unknown as ReturnType<typeof useMarketOrderBook>)
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

it('renders live order-book entries, filters by query, and expands the routing guide', async () => {
  const user = userEvent.setup()
  useOverviewMock.mockReturnValue({
    data: { pools: ['default'], stocks: [] },
    isLoading: false,
    isFetching: false,
    refetch: vi.fn(),
  } as unknown as ReturnType<typeof useMarketOverview>)
  useOrderBookMock.mockReturnValue({
    data: {
      strategy: 'price_ttft',
      updated_at: 1700000000,
      entries: [
        {
          rank: 1,
          pool: 'default',
          model: 'gpt-4o',
          merchant_code: 'm1',
          merchant_name: 'FastCloud',
          input_price: 1.2,
          output_price: 4.8,
          blended_price: 3.0,
          quote_source: 'merchant',
          ttft_ms: 340,
          tps: 68.5,
          success_rate: 0.992,
          sample_count: 15,
          has_live_metric: true,
          score: 3.5,
          updated_at: 1700000000,
        },
        {
          rank: 2,
          pool: 'default',
          model: 'claude-3-7',
          merchant_code: 'm2',
          merchant_name: 'ValuePool',
          input_price: 1.5,
          output_price: 6.0,
          blended_price: 3.75,
          quote_source: 'auto',
          ttft_ms: 1200,
          tps: 0,
          success_rate: 1.0,
          sample_count: 0,
          has_live_metric: false,
          score: 6.0,
          updated_at: 1700000000,
        },
      ],
    },
    isLoading: false,
    isFetching: false,
    refetch: vi.fn(),
  } as unknown as ReturnType<typeof useMarketOrderBook>)

  renderBoard()

  expect(screen.getByText('FastCloud')).toBeInTheDocument()
  expect(screen.getByText('Best')).toBeInTheDocument()
  expect(screen.getByText('340 ms')).toBeInTheDocument()
  expect(screen.getByText('68.5/s')).toBeInTheDocument()
  expect(screen.getByText('ValuePool')).toBeInTheDocument()

  await user.type(
    screen.getByPlaceholderText('Filter model or provider...'),
    'FastCloud'
  )
  expect(screen.getByText('FastCloud')).toBeInTheDocument()
  expect(screen.queryByText('ValuePool')).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Clear search' }))
  expect(screen.getByText('ValuePool')).toBeInTheDocument()

  await user.click(
    screen.getByRole('button', { name: /Order-Book Routing & Bidding Guide/i })
  )
  expect(
    await screen.findByText('Buyer SLA & Header Override Quickstart')
  ).toBeInTheDocument()
  expect(screen.getByRole('button', { name: /Copy cURL/i })).toBeInTheDocument()
})
