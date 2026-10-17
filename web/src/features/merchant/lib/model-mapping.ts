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
import type { MerchantModelQuote } from '../types'

/**
 * 解析某个上游模型当前对接到的官方模型名。
 * 对接值缺省时同名自动对接；显式空串表示不上架。
 */
export function resolveModelMapping(
  upstream: string,
  value: Record<string, string>,
  platformModelSet: Set<string>
): string {
  const explicit = value[upstream]
  if (explicit !== undefined) return explicit
  return platformModelSet.has(upstream) ? upstream : ''
}

/** 从对接结果生成上架清单：同名的直接上架，改过名的带 upstream；空条目与重复项丢弃。 */
export function buildModelQuotesFromMapping(
  upstreamModels: string[],
  value: Record<string, string>,
  platformModels: string[]
): MerchantModelQuote[] {
  const platformModelSet = new Set(platformModels)
  const models: MerchantModelQuote[] = []
  const seen = new Set<string>()
  for (const upstream of upstreamModels) {
    const official = resolveModelMapping(upstream, value, platformModelSet)
      .trim()
      .toLowerCase()
    if (!official || seen.has(official)) continue
    const matched = platformModels.find((m) => m.toLowerCase() === official)
    if (!matched) continue
    seen.add(matched)
    models.push(
      matched === upstream ? { model: matched } : { model: matched, upstream }
    )
  }
  return models
}
