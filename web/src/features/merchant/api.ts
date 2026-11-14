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
  ChannelQuote,
  MerchantApplication,
  MerchantEarningsPage,
  MerchantLogsPage,
  MerchantModelQuote,
  MerchantOverview,
  MerchantPortalPools,
  MerchantPortalQuote,
  MerchantReviewResult,
  MerchantRow,
  MerchantUpstreamProbeResult,
  MerchantWithdrawal,
  PageInfo,
  PortalSummary,
  PriceSyncSummary,
  UpstreamKind,
  UpstreamProbe,
} from './types'

export type { PageInfo }

// ----------------------------------------------------------------------------
// Merchant APIs（商家入驻 / 门户 / 管理端）
// ----------------------------------------------------------------------------

async function unwrap<T>(res: { data: unknown }): Promise<T> {
  const payload = requireServerSuccess(res.data) as { data?: unknown }
  return payload.data as T
}

// ---------- 用户侧 ----------

/**
 * 提交入驻申请（已是商家时为扩容申请，此时 `models` 不能为空）。
 *
 * `upstream_kind` 留空表示由后端自动判定；`rate_multiplier` 是渠道倍率
 * （相对平台标准价的折扣系数，0.1 = 一折），不填则由后端取默认值。
 */
export async function submitMerchantApplication(body: {
  site_name: string
  site_url: string
  site_key: string
  upstream_kind?: UpstreamKind
  group_name?: string
  group_desc?: string
  join_auto_group?: boolean
  rate_multiplier?: number
  models: MerchantModelQuote[]
}): Promise<MerchantApplication> {
  return unwrap(await api.post('/api/merchant/apply', body))
}

export async function getMyMerchantApplication(): Promise<MerchantApplication | null> {
  return unwrap(await api.get('/api/merchant/application'))
}

/**
 * 表单内自查上游：验证站址与密钥，并实测「倍率自动获取」是否可用。
 *
 * 与管理端探测不同，这里会真的从上游价格接口拉一次数据并折算，
 * 返回样例报价与上游模型清单（供模型对接编辑器使用）；
 * 站址/密钥仅用于本次请求，不在平台落库。
 */
export async function probeMerchantUpstream(body: {
  site_url: string
  site_key: string
  upstream_kind?: UpstreamKind
}): Promise<MerchantUpstreamProbeResult> {
  return unwrap(await api.post('/api/merchant/apply/probe', body))
}

/**
 * 平台在售的官方模型清单（全部分组，不按当前用户可用分组过滤）。
 *
 * 模型对接编辑器的数据源：商家申请加入的号池可能尚未开通，
 * 因此这里必须看到平台全量模型，而不是 /api/pricing 里按分组过滤后的子集。
 */
export async function getPlatformModels(): Promise<string[]> {
  return unwrap(await api.get('/api/merchant/platform/models'))
}

// ---------- 商家门户 ----------

export async function getMerchantSummary(): Promise<PortalSummary> {
  return unwrap(await api.get('/api/merchant/portal/summary'))
}

export async function getMerchantEarnings(
  page = 1,
  pageSize = 20,
  filters: { model_name?: string; start_timestamp?: number; end_timestamp?: number } = {}
): Promise<MerchantEarningsPage> {
  return unwrap(
    await api.get('/api/merchant/portal/earnings', {
      params: { p: page, page_size: pageSize, ...filters },
    })
  )
}

export async function getMerchantQuotes(): Promise<MerchantPortalQuote[]> {
  return unwrap(await api.get('/api/merchant/portal/quotes'))
}

/** 平台全部分组 + 商家已接入的池子（渠道）明细。 */
export async function getMerchantPools(): Promise<MerchantPortalPools> {
  return unwrap(await api.get('/api/merchant/portal/pools'))
}

/**
 * 商家调用记录（仅自己渠道，后端已脱敏，不含终端用户身份信息）。
 */
export async function getMerchantLogs(
  page = 1,
  pageSize = 20,
  filters: {
    type?: number
    group?: string
    model_name?: string
    start_timestamp?: number
    end_timestamp?: number
  } = {}
): Promise<MerchantLogsPage> {
  return unwrap(
    await api.get('/api/merchant/portal/logs', {
      params: { p: page, page_size: pageSize, ...filters },
    })
  )
}

/** 提交商家专属分组倍率 / 描述 / Auto 自动路由变更申请（需管理员审核）。 */
export async function updateMerchantRate(body: {
  group_name?: string
  channel_id?: number
  rate_multiplier: number
  group_desc?: string
  join_auto_group?: boolean
}): Promise<MerchantApplication> {
  return unwrap(await api.put('/api/merchant/portal/rate', body))
}

/** 商家对自己名下渠道触发一轮报价同步；不传 ids 则同步全部自己的渠道。 */
export async function syncMerchantQuotes(
  channelIds: number[] = []
): Promise<PriceSyncSummary> {
  return unwrap(
    await api.post('/api/merchant/portal/sync', { channel_ids: channelIds })
  )
}

/**
 * 探测商家自己渠道的上游：不传站址/密钥时复用渠道已保存的值，
 * 编辑渠道时填入新值即可"先校验再保存"。返回探测结果与上游模型清单。
 */
export async function probeMerchantChannel(body: {
  channel_id: number
  site_url?: string
  site_key?: string
}): Promise<MerchantUpstreamProbeResult> {
  return unwrap(await api.post('/api/merchant/portal/channel/probe', body))
}

/**
 * 编辑商家自己的渠道：站点名称 / 站址 / 密钥 / 模型对接。
 * 密钥留空表示不修改；站址或密钥变更时后端会先校验上游再落库。
 */
export async function updateMerchantChannel(body: {
  channel_id: number
  site_name?: string
  site_url?: string
  site_key?: string
  models?: MerchantModelQuote[]
}): Promise<{ channel_id: number; updated: boolean }> {
  return unwrap(await api.put('/api/merchant/portal/channel', body))
}

export async function getMerchantWithdrawals(
  page = 1,
  pageSize = 20
): Promise<PageInfo<MerchantWithdrawal>> {
  return unwrap(
    await api.get('/api/merchant/portal/withdrawals', {
      params: { p: page, page_size: pageSize },
    })
  )
}

export async function createMerchantWithdrawal(body: {
  amount: number
  account_info: string
  note?: string
}): Promise<MerchantWithdrawal> {
  return unwrap(await api.post('/api/merchant/portal/withdrawals', body))
}

// ---------- 管理端 ----------

export async function getMerchantApplications(
  page = 1,
  pageSize = 20,
  status = -1
): Promise<PageInfo<MerchantApplication>> {
  return unwrap(
    await api.get('/api/merchant_manage/applications', {
      params: { p: page, page_size: pageSize, status },
    })
  )
}

/**
 * 审核入驻申请。
 *
 * 可选覆盖项只对 approve 生效：`upstream_kind` 覆盖申请人声明的上游类型，
 * `channel_type` 直接指定渠道类型（1/59/60），`price_sync` 覆盖自动报价同步开关，
 * `rate_multiplier` 覆盖商家申请的渠道倍率。
 */
export async function reviewMerchantApplication(
  id: number,
  body: {
    action: 'approve' | 'reject'
    review_note?: string
    pools?: string[]
    group_name?: string
    group_desc?: string
    join_auto_group?: boolean
    profit_ratio?: number
    upstream_kind?: UpstreamKind
    channel_type?: number
    price_sync?: boolean
    rate_multiplier?: number
  }
): Promise<MerchantReviewResult> {
  return unwrap(
    await api.post(`/api/merchant_manage/applications/${id}/review`, body)
  )
}

/** 审核前探测上游（管理员操作，不改变申请状态）；密钥清除后返回 400。 */
export async function probeMerchantApplication(
  id: number
): Promise<UpstreamProbe> {
  return unwrap(await api.post(`/api/merchant_manage/applications/${id}/probe`))
}

export async function getMerchants(
  page = 1,
  pageSize = 20
): Promise<PageInfo<MerchantRow>> {
  return unwrap(
    await api.get('/api/merchant_manage/list', {
      params: { p: page, page_size: pageSize },
    })
  )
}

/** 管理端总览：全部商家（含渠道/分组归属）+ 全部分组及商家索引，一次返回。 */
export async function getMerchantOverview(): Promise<MerchantOverview> {
  return unwrap(await api.get('/api/merchant_manage/overview'))
}

export async function updateMerchant(
  id: number,
  body: { profit_ratio?: number; status?: number }
): Promise<void> {
  await api.put(`/api/merchant_manage/${id}`, body)
}

export async function getAdminWithdrawals(
  page = 1,
  pageSize = 20,
  status = -1
): Promise<PageInfo<MerchantWithdrawal>> {
  return unwrap(
    await api.get('/api/merchant_manage/withdrawals', {
      params: { p: page, page_size: pageSize, status },
    })
  )
}

export async function reviewMerchantWithdrawal(
  id: number,
  body: {
    action: 'approve' | 'reject' | 'paid'
    review_note?: string
    paid_proof?: string
  }
): Promise<MerchantWithdrawal> {
  return unwrap(
    await api.post(`/api/merchant_manage/withdrawals/${id}/review`, body)
  )
}

// ---------- 管理端：盘口报价总表 ----------

export async function getChannelQuotes(
  page = 1,
  pageSize = 20
): Promise<PageInfo<ChannelQuote>> {
  return unwrap(
    await api.get('/api/channel_quotes/', {
      params: { p: page, page_size: pageSize },
    })
  )
}

/** 手动报价：source=manual，后续自动同步不会覆盖。 */
export async function upsertChannelQuote(body: {
  channel_id: number
  pool_group: string
  model_name: string
  input_price: number
  output_price: number
  status?: number
}): Promise<ChannelQuote> {
  return unwrap(await api.put('/api/channel_quotes/', body))
}

export async function deleteChannelQuote(id: number): Promise<void> {
  await api.delete(`/api/channel_quotes/${id}`)
}

/** 触发上游价格同步；不传 channel_ids 时同步全部开启同步的渠道。 */
export async function syncChannelQuotes(
  channelIds: number[] = []
): Promise<PriceSyncSummary> {
  return unwrap(
    await api.post('/api/channel_quotes/sync', { channel_ids: channelIds })
  )
}
