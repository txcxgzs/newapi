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

export interface MarketStock {
  pool: string
  model: string
  last_price: number
  open_24h: number
  change_24h_pct: number
  high_24h: number
  low_24h: number
  vol_calls_24h: number
  vol_quota_24h: number
  /** 商家挂牌：最低挂牌模型倍率（1.0 = $2/1M tokens） */
  listed_ratio: number
  /** 商家挂牌：平均挂牌模型倍率 */
  listed_ratio_avg: number
  /** 商家挂牌：最低挂牌输入价 $/1M tokens */
  listed_input_price: number
  /** 商家挂牌：最低挂牌价对应的输出价 $/1M tokens */
  listed_output_price: number
  /** 商家挂牌：挂牌渠道数 */
  listed_channels: number
  updated_at: number
}

export interface MarketOverview {
  pools: string[]
  stocks: MarketStock[]
  updated_at: number
}

export interface MarketCandle {
  id: number
  pool_group: string
  model_name: string
  interval: string
  bucket_ts: number
  open: number
  high: number
  low: number
  close: number
  volume_calls: number
  volume_quota: number
}

export type CandleInterval = '1m' | '5m' | '1h'

export interface MarketOrderBookEntry {
  rank: number
  pool: string
  model: string
  merchant_code: string
  merchant_name: string
  input_price: number
  output_price: number
  blended_price: number
  quote_source: string
  ttft_ms: number
  tps: number
  success_rate: number
  sample_count: number
  has_live_metric: boolean
  score: number
  updated_at: number
}

export interface MarketOrderBookResponse {
  strategy: string
  entries: MarketOrderBookEntry[]
  updated_at: number
}
