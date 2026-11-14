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

/**
 * 申请/扩容时商家声明的上架模型。
 *
 * `model` 是平台官方模型名（用户请求的名字）；`upstream` 是上游实际模型名，
 * 两者不同时审核通过会写入渠道的 model_mapping。计价走倍率制，商家不填价格。
 */
export interface MerchantModelQuote {
  model: string
  pool?: string
  upstream?: string
}

/**
 * 商家上游的网关类型。
 *
 * 空字符串表示不声明（自动判定）：New API 系暴露公开价格接口，可自动同步报价；
 * OpenAI /v1/models 报价协议通过带鉴权的 /v1/models 返回 USD/1M-token 价格；
 * Sub2API / OpenAI 兼容系只有中继端点，报价需手填。
 */
export type UpstreamKind =
  | ''
  | 'newapi'
  | 'sub2api'
  | 'openai'
  | 'openai_models_pricing'

export interface MerchantApplication {
  id: number
  user_id: number
  site_name: string
  site_url: string
  site_key: string
  models: string
  upstream_kind: UpstreamKind
  /** 渠道倍率（相对平台标准价的折扣系数，0.1 = 一折）。 */
  rate_multiplier: number
  status: number // 0 pending 1 approved 2 rejected
  review_note: string
  reviewed_at: number
  created_at: number
}

/** POST /api/merchant_manage/applications/:id/probe 的探测结果（不改变申请状态）。 */
export interface UpstreamProbe {
  kind: UpstreamKind
  channel_type: number
  declared_kind?: UpstreamKind
  new_api_pricing: boolean
  models_endpoint_ok: boolean
  models?: string[]
  model_count: number
  price_sync_capable: boolean
  price_sync_protocol?: 'newapi' | 'openai_models_pricing' | ''
  warnings?: string[]
}

/** 一条从上游实测抓到的样例报价。 */
export interface UpstreamRatioPreview {
  model_name: string
  input_price: number
  output_price: number
}

/**
 * POST /api/merchant/apply/probe：商家在表单里自查上游。
 *
 * 与管理端探测不同，这里会真的从上游价格接口拉一次数据并折算，
 * `price_fetch_ok` 表示「倍率自动获取」实测可用，而不只是接口存在。
 */
export interface MerchantUpstreamProbeResult {
  kind: UpstreamKind
  price_sync_capable: boolean
  price_sync_protocol?: 'newapi' | 'openai_models_pricing' | ''
  models_endpoint_ok: boolean
  model_count: number
  warnings?: string[]
  /** 实测抓价是否成功。 */
  price_fetch_ok: boolean
  price_fetch_message?: string
  ratio_total?: number
  ratio_preview?: UpstreamRatioPreview[]
  /** 用密钥从上游 /v1/models 拉到的模型清单，供模型对接编辑器使用。 */
  models?: string[]
}

/** POST /api/merchant_manage/applications/:id/review 的审核结果。 */
export interface MerchantReviewResult {
  channel_id: number
  channel_type: number
  price_sync: boolean
  upstream_kind: UpstreamKind
  probe: UpstreamProbe | null
}

export interface MerchantProfile {
  id: number
  user_id: number
  site_name: string
  site_url: string
  profit_ratio: number
  status: number
}

export interface MerchantWallet {
  merchant_user_id: number
  balance: number
  total_earned: number
  total_withdrawn: number
  updated_at: number
}

export interface MerchantTraffic24h {
  calls: number
  quota: number
  quota_usd: number
  fails: number
  success_rate: number
}

/** 商家在某个号池（分组）内的近 24h 调用聚合。 */
export interface MerchantGroupTraffic {
  group: string
  calls: number
  quota: number
  quota_usd: number
  fails: number
  success_rate: number
}

/**
 * 商家门户可见的调用记录（已脱敏）。
 *
 * 后端已剔除 user_id / username / token_name / token_id / ip / other，
 * 确保商家看不到终端用户身份信息。
 */
export interface MerchantLog {
  id: number
  created_at: number
  type: number
  content: string
  model_name: string
  quota: number
  prompt_tokens: number
  completion_tokens: number
  use_time: number
  is_stream: boolean
  channel: number
  channel_name: string
  group: string
  request_id?: string
}

export interface PortalSummary {
  profit_ratio: number
  site_name: string
  site_url: string
  channel_count: number
  wallet?: MerchantWallet
  traffic_24h?: MerchantTraffic24h
  groups_24h?: MerchantGroupTraffic[]
  total_quota_usd?: number
}

export interface MerchantEarning {
  id: number
  merchant_user_id: number
  channel_id: number
  log_id: number
  amount: number
  model_name: string
  created_at: number
}

/** 分页响应的通用结构（page/page_size/total/items）。 */
export interface PageInfo<T> {
  page: number
  page_size: number
  total: number
  items: T[]
}

/** 分润接口的汇总卡片数据（USD）。 */
export interface MerchantEarningsSummary {
  today: number
  last_7d: number
  last_30d: number
  total: number
}

/** 近 30 天每日分润趋势的一个数据点（day 为本地日零点的 unix 秒）。 */
export interface MerchantDailyEarning {
  day: number
  amount: number
}

/** GET /api/merchant/portal/earnings 的响应：分页明细 + 本页合计 + 汇总 + 每日趋势。 */
export interface MerchantEarningsPage extends PageInfo<MerchantEarning> {
  page_amount: number
  summary: MerchantEarningsSummary
  daily: MerchantDailyEarning[]
}

/** 调用记录筛选内统计：调用/失败/消耗/Token。 */
export interface MerchantLogStats {
  calls: number
  fails: number
  quota: number
  tokens: number
}

/** GET /api/merchant/portal/logs 的响应：分页明细 + 同口径统计。 */
export interface MerchantLogsPage extends PageInfo<MerchantLog> {
  stats: MerchantLogStats
}

export interface MerchantWithdrawal {
  id: number
  merchant_user_id: number
  amount: number
  status: number // 0 pending 1 approved 2 paid 3 rejected
  account_info: string
  note: string
  review_note: string
  paid_proof: string
  created_at: number
  paid_at: number
}

export interface ChannelQuote {
  id: number
  channel_id: number
  pool_group: string
  model_name: string
  input_price: number
  output_price: number
  /** 该渠道的渠道倍率。 */
  rate_multiplier: number
  currency: string
  source: string
  status: number
  updated_at: number
}

/**
 * 商家门户盘口报价：在盘口记录上补充渠道信息与派生倍率。
 *
 * `rate_multiplier` 为该模型在该渠道上的有效倍率（平台标准倍率 × 渠道倍率）。
 */
export interface MerchantPortalQuote extends ChannelQuote {
  channel_name: string
  channel_rate_multiplier: number
  platform_model_ratio: number
}

/** 商家在某个分组下的单个渠道（池子）。 */
export interface MerchantPoolChannel {
  channel_id: number
  name: string
  status: number
  rate_multiplier: number
  models: string[]
  /** 上游站址（商家自己的，可回显编辑）。 */
  base_url: string
  /** 官方名 → 上游名的对接映射（JSON 字符串；空表示全部同名）。 */
  model_mapping?: string
  price_sync: boolean
  /** 最近一次同步到上游真实报价的时间（unix 秒；0 = 从未同步）。 */
  price_synced_at: number
  calls_24h: number
  fails_24h: number
  quota_usd_24h: number
  success_rate: number
  /** 平均耗时（秒）。 */
  avg_use_time: number
}

/** 商家在某个分组（号池）下的渠道聚合。 */
export interface MerchantPoolGroup {
  pool: string
  channels: MerchantPoolChannel[]
  calls_24h: number
  quota_usd_24h: number
  success_rate: number
}

/** GET /api/merchant/portal/pools：平台全部可对接分组 + 商家已接入的池子。 */
export interface MerchantPortalPools {
  all_pools: string[]
  groups: MerchantPoolGroup[]
}

export interface MerchantRow extends MerchantProfile {
  wallet?: MerchantWallet
  channel_ids: number[]
  username: string
}

/** 管理端总览里的单个商家渠道。 */
export interface AdminOverviewChannel {
  id: number
  name: string
  pools: string[]
  model_count: number
  status: number
  price_sync: boolean
}

/** 管理端总览里的单个商家。 */
export interface AdminOverviewMerchant {
  user_id: number
  username: string
  site_name: string
  site_url: string
  status: number
  profit_ratio: number
  balance: number
  channels: AdminOverviewChannel[]
}

/** 管理端总览里的单个分组（号池）。 */
export interface AdminOverviewPool {
  pool: string
  merchant_count: number
  channel_count: number
  merchant_names: string[]
}

/** GET /api/merchant_manage/overview：全部商家 + 全部分组两份视图。 */
export interface MerchantOverview {
  merchants: AdminOverviewMerchant[]
  pools: AdminOverviewPool[]
}

/** 一次上游价格同步任务的汇总（POST /api/channel_quotes/sync）。 */
export interface PriceSyncSummary {
  total_channels: number
  synced_channels: number
  failed_channels: number
  /** 上游不提供价格接口而被跳过的渠道数。 */
  skipped_channels: number
  written_quotes: number
  failed_details?: string[]
  skipped_details?: string[]
}
