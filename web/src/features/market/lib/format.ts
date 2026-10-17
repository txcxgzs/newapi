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
import { formatCurrencyFromUSD } from '@/lib/currency'

// ----------------------------------------------------------------------------
// 行情展示格式化（成交价 / 成交额）
// ----------------------------------------------------------------------------

/**
 * 盘口成交价（$/1M token）。
 *
 * 低价模型的成交价可达 1e-6 量级，按量级选择精度以免被舍入为 0；
 * 无成交（0、负值或非有限值）时返回占位符。
 */
export function formatMarketPrice(
  price: number | undefined,
  locale?: Intl.LocalesArgument
): string {
  if (!price || !Number.isFinite(price) || price <= 0) return '--'
  return formatCurrencyFromUSD(price, {
    digitsLarge: price >= 100 ? 2 : 4,
    digitsSmall: 6,
    locale,
  })
}

/** 24h 成交额，金额沿用系统货币展示，大额使用紧凑写法。 */
export function formatMarketVolume(
  usd: number | undefined,
  locale?: Intl.LocalesArgument
): string {
  if (!usd || !Number.isFinite(usd) || usd <= 0) return '--'
  return formatCurrencyFromUSD(usd, {
    digitsLarge: 2,
    digitsSmall: 2,
    compact: usd >= 1000,
    locale,
  })
}

/**
 * 商家挂牌模型倍率（1.0 = $2/1M tokens）。
 *
 * 与成交价不同，倍率是无量纲系数，固定保留 4 位小数并带 "x" 后缀，
 * 无挂牌（0、负值或非有限值）时返回占位符。
 */
export function formatMarketRatio(ratio: number | undefined): string {
  if (!ratio || !Number.isFinite(ratio) || ratio <= 0) return '--'
  return `${ratio.toFixed(4)}x`
}
