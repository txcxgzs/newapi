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

export interface CustomModelPriceDraft {
  input: string
  output: string
}

/** 归一化模型名用于容错匹配：统一小写，并将 `.` 与 `_` 折叠为 `-`。 */
export function normalizeModelMatchKey(name: string): string {
  return name.trim().toLowerCase().replaceAll(/[._]/g, '-')
}

/** 为平台模型表建立归一化索引（用于 `.` vs `-` 与大小写差异的智能匹配）。 */
export function buildNormalizedPlatformModelMap(
  platformModels: string[]
): Map<string, string[]> {
  const index = new Map<string, string[]>()
  for (const model of platformModels) {
    const key = normalizeModelMatchKey(model)
    const list = index.get(key)
    if (list) {
      list.push(model)
    } else {
      index.set(key, [model])
    }
  }
  return index
}

export function buildMerchantCustomPrefix(userId?: number): string {
  return userId && userId > 0 ? `m${userId}/` : 'm0/'
}

export function isMerchantCustomTarget(
  target: string,
  userId?: number
): boolean {
  if (!target) return false
  const prefix = buildMerchantCustomPrefix(userId)
  return target.startsWith(prefix) && target.length > prefix.length
}

export function buildDefaultCustomModelName(
  upstream: string,
  userId?: number
): string {
  const prefix = buildMerchantCustomPrefix(userId)
  const tail = upstream.includes('/')
    ? upstream.slice(upstream.lastIndexOf('/') + 1)
    : upstream
  const sanitized = tail.trim().replaceAll(/[,\s/]+/g, '-') || 'custom-model'
  return `${prefix}${sanitized}`
}

export function findSuggestedPlatformModel(
  upstream: string,
  platformModelSet: Set<string>,
  normalizedMap?: Map<string, string[]>
): { target: string; matchedByNormalization: boolean } {
  if (platformModelSet.has(upstream)) {
    return { target: upstream, matchedByNormalization: false }
  }
  if (!normalizedMap) {
    return { target: '', matchedByNormalization: false }
  }
  const candidates = normalizedMap.get(normalizeModelMatchKey(upstream))
  if (candidates && candidates.length === 1) {
    return { target: candidates[0], matchedByNormalization: true }
  }
  return { target: '', matchedByNormalization: false }
}

/**
 * 解析某个上游模型当前对接到的暴露模型名。
 * 缺省时优先同名自动对接，其次按 `.` / `-` 归一化唯一匹配；显式空串表示不上架。
 */
export function resolveModelMapping(
  upstream: string,
  value: Record<string, string>,
  platformModelSet: Set<string>,
  normalizedMap?: Map<string, string[]>
): string {
  const explicit = value[upstream]
  if (explicit !== undefined) return explicit
  return findSuggestedPlatformModel(upstream, platformModelSet, normalizedMap)
    .target
}

export function validateCustomModelQuotes(
  upstreamModels: string[],
  value: Record<string, string>,
  platformModels: string[],
  customPrices?: Record<string, CustomModelPriceDraft>,
  merchantUserId?: number
): string | null {
  const platformModelSet = new Set(platformModels)
  const normalizedMap = buildNormalizedPlatformModelMap(platformModels)
  const prefix = buildMerchantCustomPrefix(merchantUserId)
  for (const upstream of upstreamModels) {
    const rawTarget = resolveModelMapping(
      upstream,
      value,
      platformModelSet,
      normalizedMap
    ).trim()
    if (!rawTarget || !rawTarget.startsWith(prefix)) continue
    const suffix = rawTarget.slice(prefix.length)
    if (!suffix || /[,\s/]/.test(suffix)) {
      return `自定义模型「${rawTarget}」别名不能为空，且不能包含斜杠、逗号或空格`
    }
    const price = customPrices?.[rawTarget]
    const inputNum = Number(price?.input)
    const outputNum = Number(price?.output)
    if (
      !Number.isFinite(inputNum) ||
      inputNum <= 0 ||
      inputNum > 10000 ||
      !Number.isFinite(outputNum) ||
      outputNum <= 0 ||
      outputNum > 10000
    ) {
      return `自定义模型「${rawTarget}」需填写 0（不含）~ 10000 之间的输入与输出基准价`
    }
  }
  return null
}

/** 从对接结果生成上架清单：平台模型支持同/近名匹配，自定义 m{id}/ 模型附带基准单价。 */
export function buildModelQuotesFromMapping(
  upstreamModels: string[],
  value: Record<string, string>,
  platformModels: string[],
  customPrices?: Record<string, CustomModelPriceDraft>,
  merchantUserId?: number
): MerchantModelQuote[] {
  const platformModelSet = new Set(platformModels)
  const normalizedMap = buildNormalizedPlatformModelMap(platformModels)
  const models: MerchantModelQuote[] = []
  const seen = new Set<string>()
  for (const upstream of upstreamModels) {
    const rawTarget = resolveModelMapping(
      upstream,
      value,
      platformModelSet,
      normalizedMap
    ).trim()
    if (!rawTarget) continue

    if (isMerchantCustomTarget(rawTarget, merchantUserId)) {
      if (seen.has(rawTarget)) continue
      seen.add(rawTarget)
      const price = customPrices?.[rawTarget]
      const inputNum = Number(price?.input)
      const outputNum = Number(price?.output)
      const quote: MerchantModelQuote =
        rawTarget === upstream
          ? { model: rawTarget }
          : { model: rawTarget, upstream }
      if (Number.isFinite(inputNum) && inputNum > 0) {
        quote.input_price = inputNum
      }
      if (Number.isFinite(outputNum) && outputNum > 0) {
        quote.output_price = outputNum
      }
      models.push(quote)
      continue
    }

    const officialKey = rawTarget.toLowerCase()
    if (seen.has(officialKey)) continue
    const matched = platformModels.find((m) => m.toLowerCase() === officialKey)
    if (!matched) continue
    seen.add(officialKey)
    models.push(
      matched === upstream ? { model: matched } : { model: matched, upstream }
    )
  }
  return models
}
