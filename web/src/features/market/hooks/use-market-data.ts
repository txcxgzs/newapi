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
import { useQuery } from '@tanstack/react-query'

import { requireServerSuccess } from '@/lib/server-error-message'

import { getMarketKline, getMarketOverview } from '../api'
import type { CandleInterval } from '../types'

// ----------------------------------------------------------------------------
// Market data hooks（行情页轮询刷新）
// ----------------------------------------------------------------------------

export function useMarketOverview(pool?: string, refetchMs = 10000) {
  return useQuery({
    queryKey: ['market', 'overview', pool ?? 'all'],
    queryFn: async () => requireServerSuccess(await getMarketOverview(pool)),
    refetchInterval: refetchMs,
    staleTime: 5000,
  })
}

export function useMarketKline(
  pool: string,
  model: string,
  interval: CandleInterval,
  limit = 200
) {
  // K线由后台每分钟聚合一次，1m 周期 30s 轮询，其他周期 60s 足够
  const refetchMs = interval === '1m' ? 30000 : 60000
  return useQuery({
    queryKey: ['market', 'kline', pool, model, interval, limit],
    queryFn: async () =>
      requireServerSuccess(await getMarketKline(pool, model, interval, limit)),
    enabled: Boolean(pool && model),
    refetchInterval: refetchMs,
    staleTime: 10000,
  })
}
