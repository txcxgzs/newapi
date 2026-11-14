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
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type {
  CandleInterval,
  MarketCandle,
  MarketOrderBookResponse,
  MarketOverview,
} from './types'

// ----------------------------------------------------------------------------
// Market APIs（公开行情）
// ----------------------------------------------------------------------------

export async function getMarketOverview(
  pool?: string
): Promise<MarketOverview> {
  const res = await api.get('/api/market/overview', {
    params: pool ? { pool } : undefined,
  })
  return requireServerSuccess(res.data).data as MarketOverview
}

export async function getMarketKline(
  pool: string,
  model: string,
  interval: CandleInterval,
  limit = 200
): Promise<MarketCandle[]> {
  const res = await api.get('/api/market/kline', {
    params: { pool, model, interval, limit },
  })
  const payload = requireServerSuccess(res.data)
  return (payload?.data?.candles ?? []) as MarketCandle[]
}

export async function getMarketOrderBook(params?: {
  pool?: string
  model?: string
  strategy?: string
}): Promise<MarketOrderBookResponse> {
  const res = await api.get('/api/market/orderbook', {
    params: {
      ...(params?.pool ? { pool: params.pool } : {}),
      ...(params?.model ? { model: params.model } : {}),
      ...(params?.strategy ? { strategy: params.strategy } : {}),
    },
  })
  return requireServerSuccess(res.data).data as MarketOrderBookResponse
}
