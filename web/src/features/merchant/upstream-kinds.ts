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
import type { UpstreamKind } from './types'

/** 下拉框中「自动判定 / 跟随探测结果」的哨兵值；提交时转换为空上游类型。 */
export const UPSTREAM_KIND_AUTO = 'auto'

/** 可以显式声明的上游网关类型。 */
export const UPSTREAM_KIND_VALUES: UpstreamKind[] = [
  'newapi',
  'openai_models_pricing',
  'sub2api',
  'openai',
]

/** 审核时可指定的渠道类型：1=OpenAI 兼容 / 59=Sub2API / 60=New API。 */
export const MERCHANT_CHANNEL_TYPES = [1, 59, 60] as const

/** 把接口返回的上游类型映射为展示文案键；空类型表示自动判定。 */
export function upstreamKindLabelKey(kind: string | undefined): string {
  switch (kind) {
    case 'newapi':
      return 'New API'
    case 'openai_models_pricing':
      return 'OpenAI /v1/models pricing protocol'
    case 'sub2api':
      return 'Sub2API'
    case 'openai':
      return 'OpenAI compatible'
    default:
      return 'Auto detect'
  }
}

/** 把下拉框的哨兵值转换回接口字段：未知值与 auto 都表示不声明类型。 */
export function toUpstreamKind(selection: string): UpstreamKind {
  const kind = UPSTREAM_KIND_VALUES.find((value) => value === selection)
  return kind ?? ''
}

/** 上游类型下拉选项；autoLabelKey 区分「自动探测」与「跟随探测结果」。 */
export function upstreamKindOptions(
  autoLabelKey: string
): { value: string; labelKey: string }[] {
  return [
    { value: UPSTREAM_KIND_AUTO, labelKey: autoLabelKey },
    ...UPSTREAM_KIND_VALUES.map((kind) => ({
      value: kind,
      labelKey: upstreamKindLabelKey(kind),
    })),
  ]
}
