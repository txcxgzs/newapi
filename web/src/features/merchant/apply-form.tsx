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
import {
  AlertTriangle,
  ChevronDown,
  ChevronUp,
  Globe,
  RefreshCw,
  Server,
  ShieldAlert,
  Store,
  Wallet,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  getPlatformModels,
  probeMerchantUpstream,
  submitMerchantApplication,
} from './api'
import { MerchantModelMappingEditor } from './components/model-mapping-editor'
import { PayoutQrcodeUploader } from './components/payout-qrcode-uploader'
import {
  buildModelQuotesFromMapping,
  type CustomModelPriceDraft,
  validateCustomModelQuotes,
} from './lib/model-mapping'
import { OPERATION_TENURE_OPTIONS } from './lib/payout-qrcode'
import { PAYOUT_METHOD_OPTIONS } from './payout-methods'
import type { MerchantModelQuote, MerchantUpstreamProbeResult } from './types'
import {
  UPSTREAM_KIND_AUTO,
  toUpstreamKind,
  upstreamKindOptions,
} from './upstream-kinds'

interface MerchantApplyFormProps {
  defaultGroupName?: string
  hasMerchantProfile?: boolean
  onSubmitted: () => void
}

/** 上游类型下拉选项：自动探测 / New API / Sub2API / OpenAI 兼容。 */
const UPSTREAM_KIND_OPTIONS = upstreamKindOptions('Auto detect')

interface ParsedModels {
  models: MerchantModelQuote[]
  /** 第一条缺少模型名的行，用于提示用户修正。 */
  invalidLine: string | null
}

/**
 * 解析手动填写的模型清单：空行忽略，每行 `模型名` 或 `模型名, 号池`。
 * 仅作为「拉取上游模型」失败时的兜底入口。
 */
function parseModels(raw: string): ParsedModels {
  const models: MerchantModelQuote[] = []
  let invalidLine: string | null = null
  for (const line of raw
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean)) {
    // 每行: 模型名 [, 号池名]
    const parts = line.split(',').map((part) => part.trim())
    const model = parts[0] ?? ''
    if (!model) {
      invalidLine ??= line
      continue
    }
    // 号池为空时不携带该字段，避免提交空字符串污染后端解析
    models.push(parts[1] ? { model, pool: parts[1] } : { model })
  }
  return { models, invalidLine }
}

/**
 * 商家入驻 / 渠道扩容表单。
 *
 * 首次入驻重点核验：商户名称（ShopName）、自有平台网址（仅资质审核与展示）、
 * 运营时长、联系方式、收款方式与收款二维码；首条上游渠道改为可选折叠项，
 * 审核通过进入商家中心后再自由添加多条上游渠道与专属分组。
 */
export function MerchantApplyForm({
  defaultGroupName,
  hasMerchantProfile,
  onSubmitted,
}: MerchantApplyFormProps) {
  const { t } = useTranslation()
  const authUser = useAuthStore((state) => state.auth.user)
  const isExistingMerchant =
    hasMerchantProfile ?? (authUser?.role ?? 0) >= ROLE.MERCHANT

  // 1. 商户资质与联系方式
  const [shopName, setShopName] = useState('')
  const [platformUrl, setPlatformUrl] = useState('')
  const [operationTenure, setOperationTenure] = useState('')
  const [contactName, setContactName] = useState('')
  const [contactInfo, setContactInfo] = useState('')
  const [description, setDescription] = useState('')

  // 2. 结算方式与收款二维码
  const [payoutMethod, setPayoutMethod] = useState('')
  const [payoutAccount, setPayoutAccount] = useState('')
  const [payoutHolder, setPayoutHolder] = useState('')
  const [payoutQrcode, setPayoutQrcode] = useState('')
  const [agreedTerms, setAgreedTerms] = useState(false)

  // 3. 首条上游渠道配置（首次入驻可选；已是商家扩展渠道时必选）
  const [configureChannelNow, setConfigureChannelNow] = useState(
    isExistingMerchant
  )
  const [siteName, setSiteName] = useState('')
  const [siteUrl, setSiteUrl] = useState('')
  const [siteKey, setSiteKey] = useState('')
  const [upstreamKind, setUpstreamKind] = useState<string>(UPSTREAM_KIND_AUTO)
  const [groupName, setGroupName] = useState(defaultGroupName ?? '')
  const [groupDesc, setGroupDesc] = useState('')
  const [groupRate, setGroupRate] = useState('1')
  const [joinAutoGroup, setJoinAutoGroup] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [probing, setProbing] = useState(false)
  const [probeResult, setProbeResult] =
    useState<MerchantUpstreamProbeResult | null>(null)
  const merchantUserId = authUser?.id
  const [upstreamModels, setUpstreamModels] = useState<string[]>([])
  const [mapping, setMapping] = useState<Record<string, string>>({})
  const [customPrices, setCustomPrices] = useState<
    Record<string, CustomModelPriceDraft>
  >({})
  const [manualModels, setManualModels] = useState('')
  const [showManualModels, setShowManualModels] = useState(false)

  const includeChannelFields = isExistingMerchant || configureChannelNow

  const platformModelsQuery = useQuery({
    queryKey: ['merchant', 'platform-models'],
    queryFn: getPlatformModels,
    staleTime: 5 * 60_000,
    enabled: includeChannelFields,
  })
  const platformModels = useMemo(
    () => platformModelsQuery.data ?? [],
    [platformModelsQuery.data]
  )

  /** 站址/密钥/上游类型变化后，旧检测结果即失效。 */
  function invalidateProbe() {
    setProbeResult(null)
    setUpstreamModels([])
    setMapping({})
    setCustomPrices({})
  }

  async function runProbe() {
    if (!siteUrl.trim() || !siteKey.trim()) {
      toast.error(t('Please fill in API Base URL and API Key first'))
      return
    }
    setProbing(true)
    setProbeResult(null)
    setUpstreamModels([])
    setMapping({})
    setCustomPrices({})
    try {
      const result = await probeMerchantUpstream({
        site_url: siteUrl.trim(),
        site_key: siteKey.trim(),
        upstream_kind: toUpstreamKind(upstreamKind),
      })
      setProbeResult(result)
      const models = [...(result.models ?? [])].sort((a, b) =>
        a.localeCompare(b)
      )
      setUpstreamModels(models)
      setShowManualModels(models.length === 0)
    } catch (error) {
      handleServerError(error, t('Probe failed, please check the site URL'))
      setShowManualModels(true)
    } finally {
      setProbing(false)
    }
  }

  /** 从对接结果生成上架清单；没有任何对接时回退到手动清单。 */
  function buildModelQuotes(): ParsedModels {
    const mapped = buildModelQuotesFromMapping(
      upstreamModels,
      mapping,
      platformModels,
      customPrices,
      merchantUserId
    )
    if (mapped.length > 0) {
      return { models: mapped, invalidLine: null }
    }
    return parseModels(manualModels)
  }

  async function submit() {
    const effectiveShopName = shopName.trim()
    const effectiveChannelName = siteName.trim() || effectiveShopName

    if (!isExistingMerchant) {
      if (!effectiveShopName) {
        toast.error(t('Please enter your Shop Name'))
        return
      }
      if (!platformUrl.trim()) {
        toast.error(t('Please enter your Platform Website URL'))
        return
      }
      if (!operationTenure) {
        toast.error(t('Please select how long you have operated your platform'))
        return
      }
      if (!contactInfo.trim()) {
        toast.error(
          t('Please provide Merchant Contact Info (Telegram / WeChat / Email)')
        )
        return
      }
      if (payoutMethod && !payoutAccount.trim()) {
        toast.error(
          t('Please enter the payout account for the selected payout method')
        )
        return
      }
      if (!agreedTerms) {
        toast.error(t('Please read and agree to the merchant settlement terms'))
        return
      }
    }

    let finalModels: MerchantModelQuote[] = []
    if (includeChannelFields) {
      if (!effectiveChannelName || !siteUrl.trim() || !siteKey.trim()) {
        toast.error(t('Please fill in Channel Name, API Base URL and API Key'))
        return
      }
      const customErr = validateCustomModelQuotes(
        upstreamModels,
        mapping,
        platformModels,
        customPrices,
        merchantUserId
      )
      if (customErr) {
        toast.error(t(customErr))
        return
      }
      const parsed = buildModelQuotes()
      if (parsed.invalidLine) {
        toast.error(
          t('Model line is invalid: {{line}}', { line: parsed.invalidLine })
        )
        return
      }
      finalModels = parsed.models
      const trimmedRate = groupRate.trim()
      if (trimmedRate !== '') {
        const rateNum = Number(trimmedRate)
        if (!Number.isFinite(rateNum) || rateNum <= 0 || rateNum > 100) {
          toast.error(t('分组倍率需在 0（不含）到 100 之间'))
          return
        }
      }
    }

    setSubmitting(true)
    try {
      const trimmedRate = groupRate.trim()
      await submitMerchantApplication({
        shop_name: isExistingMerchant ? undefined : effectiveShopName,
        platform_url: isExistingMerchant ? undefined : platformUrl.trim(),
        operation_tenure: isExistingMerchant ? undefined : operationTenure,
        site_name: includeChannelFields
          ? effectiveChannelName
          : effectiveShopName,
        site_url: includeChannelFields ? siteUrl.trim() : undefined,
        site_key: includeChannelFields ? siteKey.trim() : undefined,
        contact_name: contactName.trim() || undefined,
        contact_info: contactInfo.trim() || undefined,
        description: description.trim() || undefined,
        payout_method: payoutMethod || undefined,
        payout_account: payoutAccount.trim() || undefined,
        payout_holder: payoutHolder.trim() || undefined,
        payout_qrcode: payoutQrcode || undefined,
        agreed_terms: isExistingMerchant ? undefined : agreedTerms,
        upstream_kind: includeChannelFields
          ? toUpstreamKind(upstreamKind)
          : undefined,
        group_name:
          includeChannelFields && groupName.trim() ? groupName.trim() : undefined,
        group_desc:
          includeChannelFields && groupDesc.trim() ? groupDesc.trim() : undefined,
        join_auto_group: includeChannelFields ? joinAutoGroup : undefined,
        rate_multiplier:
          includeChannelFields && trimmedRate !== ''
            ? Number(trimmedRate)
            : undefined,
        models: includeChannelFields ? finalModels : undefined,
      })
      toast.success(t('Application submitted, waiting for review'))
      onSubmitted()
    } catch (error) {
      handleServerError(error, t('Failed to submit application'))
    } finally {
      setSubmitting(false)
    }
  }

  const preview = probeResult?.ratio_preview ?? []
  const prefixPreviewBase = shopName.trim() || siteName.trim()

  return (
    <div className='bg-card space-y-5 rounded-2xl border p-5 shadow-xs'>
      {!isExistingMerchant && (
        <>
          {/* Section 1: 商户店铺档案与现有平台资质 */}
          <section className='bg-muted/20 space-y-4 rounded-xl border p-4'>
            <div className='flex items-start justify-between gap-2'>
              <div className='flex items-center gap-2'>
                <div className='bg-primary/10 text-primary flex size-7 items-center justify-center rounded-lg'>
                  <Store className='size-4' />
                </div>
                <div>
                  <h3 className='text-sm font-semibold'>
                    {t('1. Shop Identity & Platform Qualification')}
                  </h3>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Merchants must operate an existing platform before applying. Qualification fields verify track record and brand identity.'
                    )}
                  </p>
                </div>
              </div>
              <Badge variant='secondary' className='shrink-0 text-[11px]'>
                {t('Required')}
              </Badge>
            </div>

            <div className='space-y-1.5'>
              <Label htmlFor='merchant-shop-name'>{t('Shop Name')}</Label>
              <Input
                id='merchant-shop-name'
                value={shopName}
                onChange={(e) => setShopName(e.target.value)}
                placeholder={t('e.g. AcmeAI Relay')}
              />
              <div className='flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-300'>
                <AlertTriangle className='mt-0.5 size-3.5 shrink-0' />
                <span>
                  {t(
                    'Important: Shop Name is used for your Merchant Center header, Finance Center reconciliation statements, and exclusive group prefixes (e.g. ShopName-Group). It cannot be edited freely after approval—renaming requires submitting a review application.'
                  )}
                </span>
              </div>
            </div>

            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label
                  htmlFor='merchant-platform-url'
                  className='flex items-center gap-1.5'
                >
                  <Globe className='text-muted-foreground size-3.5' />
                  {t('Platform Website URL')}
                </Label>
                <Input
                  id='merchant-platform-url'
                  value={platformUrl}
                  onChange={(e) => setPlatformUrl(e.target.value)}
                  placeholder='https://your-platform.example.com'
                />
                <p className='text-muted-foreground text-[11px]'>
                  {t(
                    'Used strictly for merchant qualification review and portal display — never used for API relay or endpoint probing.'
                  )}
                </p>
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-operation-tenure'>
                  {t('Operation Tenure')}
                </Label>
                <Select
                  items={OPERATION_TENURE_OPTIONS.map((option) => ({
                    value: option.value,
                    label: t(option.labelKey),
                  }))}
                  value={operationTenure}
                  onValueChange={(next) => {
                    if (next !== null) setOperationTenure(next)
                  }}
                >
                  <SelectTrigger
                    id='merchant-operation-tenure'
                    className='w-full'
                  >
                    <SelectValue placeholder={t('Select operation duration')} />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    {OPERATION_TENURE_OPTIONS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {t(option.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className='text-muted-foreground text-[11px]'>
                  {t('How long your own site or supply platform has operated.')}
                </p>
              </div>
            </div>

            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-contact-name'>
                  {t('Contact Person')}
                </Label>
                <Input
                  id='merchant-contact-name'
                  value={contactName}
                  onChange={(e) => setContactName(e.target.value)}
                  placeholder={t('e.g. Alice / Ops Lead')}
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-contact-info'>
                  {t('Contact Info')}
                </Label>
                <Input
                  id='merchant-contact-info'
                  value={contactInfo}
                  onChange={(e) => setContactInfo(e.target.value)}
                  placeholder={t('Telegram @handle / WeChat / Email')}
                />
              </div>
            </div>

            <div className='space-y-1.5'>
              <Label htmlFor='merchant-description'>
                {t('Platform & Supply Capacity Notes')}
              </Label>
              <Input
                id='merchant-description'
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t(
                  'e.g. Direct Tier-5 OpenAI/Claude pool, 24/7 failover, average RPM 5000'
                )}
              />
            </div>
          </section>

          {/* Section 2: 结算规则与收款方式 */}
          <section className='bg-muted/20 space-y-4 rounded-xl border p-4'>
            <div className='flex items-center gap-2'>
              <div className='bg-primary/10 text-primary flex size-7 items-center justify-center rounded-lg'>
                <Wallet className='size-4' />
              </div>
              <div>
                <h3 className='text-sm font-semibold'>
                  {t('2. Settlement Rules & Payout Account')}
                </h3>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Default split: 95% merchant / 5% platform fee. Configure your receiving account and optional QR code for payouts.'
                  )}
                </p>
              </div>
            </div>

            <div className='grid gap-3 sm:grid-cols-3'>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-payout-method'>
                  {t('Payout Method')}
                </Label>
                <Select
                  items={PAYOUT_METHOD_OPTIONS.map((option) => ({
                    value: option.value,
                    label: t(option.labelKey),
                  }))}
                  value={payoutMethod}
                  onValueChange={(next) => {
                    if (next !== null) setPayoutMethod(next)
                  }}
                >
                  <SelectTrigger id='merchant-payout-method' className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    {PAYOUT_METHOD_OPTIONS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {t(option.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-payout-account'>
                  {t('Payout Account')}
                </Label>
                <Input
                  id='merchant-payout-account'
                  value={payoutAccount}
                  onChange={(e) => setPayoutAccount(e.target.value)}
                  placeholder={t('Wallet address / Alipay / Bank card')}
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-payout-holder'>
                  {t('Account Holder')}
                </Label>
                <Input
                  id='merchant-payout-holder'
                  value={payoutHolder}
                  onChange={(e) => setPayoutHolder(e.target.value)}
                  placeholder={t('Recipient name or memo')}
                />
              </div>
            </div>

            <div className='space-y-1.5'>
              <Label htmlFor='merchant-payout-qrcode'>
                {t('Payout Collection QR Code')}
              </Label>
              <PayoutQrcodeUploader
                id='merchant-payout-qrcode'
                value={payoutQrcode}
                onChange={setPayoutQrcode}
              />
            </div>
          </section>

          {/* Section 3 Toggle: 可选首条渠道配置 */}
          <div className='flex items-center justify-between gap-3 rounded-xl border bg-muted/15 px-4 py-3'>
            <div className='space-y-0.5'>
              <div className='flex items-center gap-2'>
                <Server className='text-primary size-4' />
                <Label
                  htmlFor='merchant-configure-channel-now'
                  className='text-sm font-semibold'
                >
                  {t('Configure initial upstream channel now (optional)')}
                </Label>
              </div>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Leave off for fast qualification onboarding — you can add channels, API Base URLs, and groups anytime in Merchant Portal after approval.'
                )}
              </p>
            </div>
            <div className='flex items-center gap-2'>
              <Switch
                id='merchant-configure-channel-now'
                checked={configureChannelNow}
                onCheckedChange={setConfigureChannelNow}
              />
              {configureChannelNow ? (
                <ChevronUp className='text-muted-foreground size-4' />
              ) : (
                <ChevronDown className='text-muted-foreground size-4' />
              )}
            </div>
          </div>
        </>
      )}

      {includeChannelFields && (
        <section className='space-y-4 rounded-xl border bg-muted/10 p-4'>
          <div className='space-y-0.5'>
            <h3 className='text-sm font-semibold'>
              {isExistingMerchant
                ? t('Add Upstream Channel & Group')
                : t('3. Initial Upstream Channel & Model Mapping')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'This API Base URL & Key belong exclusively to this relay channel (independent of your display Platform Website URL).'
              )}
            </p>
          </div>

          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label htmlFor='merchant-site-name'>
                {isExistingMerchant ? t('Site Name') : t('Channel Alias')}
              </Label>
              <Input
                id='merchant-site-name'
                value={siteName}
                onChange={(e) => setSiteName(e.target.value)}
                placeholder={shopName.trim() || t('MyRelay')}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='merchant-site-url'>{t('API Base URL')}</Label>
              <Input
                id='merchant-site-url'
                value={siteUrl}
                onChange={(e) => {
                  setSiteUrl(e.target.value)
                  invalidateProbe()
                }}
                placeholder={t('https://api.example.com')}
              />
            </div>
          </div>

          <div className='space-y-1.5'>
            <Label htmlFor='merchant-site-key'>{t('API Key')}</Label>
            <Input
              id='merchant-site-key'
              type='password'
              value={siteKey}
              onChange={(e) => {
                setSiteKey(e.target.value)
                invalidateProbe()
              }}
              placeholder={t('sk-...')}
            />
          </div>

          <div className='bg-muted/30 space-y-3 rounded-lg border p-3'>
            <div className='text-sm font-medium'>
              {t('专属分组与路由设置（提交后需管理员审核）')}
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-group-name'>{t('专属分组名称')}</Label>
                <Input
                  id='merchant-group-name'
                  value={groupName}
                  onChange={(e) => setGroupName(e.target.value)}
                  placeholder='例如 特惠通道 或 vip01'
                />
                <p className='text-muted-foreground text-xs'>
                  {t(
                    '支持中文、字母、数字、连字符（-）与下划线（_），不可使用 default/vip/svip/auto 等系统保留词；提交时会自动加上「商户名-」前缀。'
                  )}
                  {groupName.trim() && prefixPreviewBase && (
                    <span className='text-foreground ml-1 font-mono'>
                      {t('最终分组名：{{name}}', {
                        name: groupName.trim().startsWith(`${prefixPreviewBase}-`)
                          ? groupName.trim()
                          : `${prefixPreviewBase}-${groupName.trim()}`,
                      })}
                    </span>
                  )}
                </p>
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-group-rate'>
                  {t('分组倍率（GroupRatio）')}
                </Label>
                <Input
                  id='merchant-group-rate'
                  type='number'
                  step='0.01'
                  min='0.01'
                  max='100'
                  value={groupRate}
                  onChange={(e) => setGroupRate(e.target.value)}
                  placeholder='1'
                />
                <p className='text-muted-foreground text-xs'>
                  {t(
                    '1.0 = 平台标准价，0.5 = 五折；计费按原生「模型倍率 × 分组倍率」结算。'
                  )}
                </p>
              </div>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='merchant-group-desc'>{t('分组说明')}</Label>
              <Input
                id='merchant-group-desc'
                value={groupDesc}
                onChange={(e) => setGroupDesc(e.target.value)}
                placeholder={t('向终端用户展示在分组选择器中的说明')}
              />
            </div>
            <div className='bg-background flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
              <div className='space-y-0.5'>
                <Label htmlFor='merchant-join-auto-group'>
                  {t('加入平台 Auto 自动路由')}
                </Label>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    '开启后，管理员审核通过时将该专属分组加入平台 auto 自动路由候选链。'
                  )}
                </p>
              </div>
              <Switch
                id='merchant-join-auto-group'
                checked={joinAutoGroup}
                onCheckedChange={setJoinAutoGroup}
              />
            </div>
          </div>

          <div className='space-y-1.5'>
            <Label htmlFor='merchant-upstream-kind'>{t('Upstream type')}</Label>
            <Select
              items={UPSTREAM_KIND_OPTIONS.map((option) => ({
                value: option.value,
                label: t(option.labelKey),
              }))}
              value={upstreamKind}
              onValueChange={(next) => {
                if (next !== null) {
                  setUpstreamKind(next)
                  invalidateProbe()
                }
              }}
            >
              <SelectTrigger
                id='merchant-upstream-kind'
                className='w-full sm:w-64'
                aria-describedby='merchant-upstream-kind-hint'
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {UPSTREAM_KIND_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {t(option.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p
              id='merchant-upstream-kind-hint'
              className='text-muted-foreground text-xs'
            >
              {t(
                'New API and OpenAI /v1/models pricing protocol upstreams expose a pricing API, so rates sync automatically; Sub2API and OpenAI-compatible upstreams have none and list at the platform standard price.'
              )}
            </p>
          </div>

          {/* 上游检测：验证可达性/密钥，实测倍率自动获取，并拉取上游模型清单 */}
          <div className='space-y-2 rounded-lg border border-dashed p-3'>
            <div className='flex items-center justify-between gap-2'>
              <p className='text-sm font-medium'>{t('Ratio auto-fetch')}</p>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={runProbe}
                disabled={probing || !siteUrl.trim() || !siteKey.trim()}
              >
                <RefreshCw
                  className={`mr-1.5 size-3.5 ${probing ? 'animate-spin' : ''}`}
                  aria-hidden
                />
                {probing
                  ? t('Detecting...')
                  : t('Probe upstream & pull models')}
              </Button>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Checks your upstream, tests whether rates can be fetched automatically, and pulls its model list for mapping — no manual model names.'
              )}
            </p>

            {probeResult?.price_fetch_ok && (
              <Alert>
                <AlertTitle>{t('Ratio auto-fetch works')}</AlertTitle>
                <AlertDescription className='space-y-2'>
                  {(probeResult.warnings?.length ?? 0) > 0 && (
                    <div className='space-y-0.5 text-xs text-amber-600 dark:text-amber-400'>
                      {probeResult.warnings?.map((warning) => (
                        <p key={warning}>{warning}</p>
                      ))}
                    </div>
                  )}
                  <p>
                    {t(
                      'Fetched {{total}} model prices from the upstream. After approval the platform re-syncs them on a schedule — no manual upkeep.',
                      { total: probeResult.ratio_total ?? preview.length }
                    )}
                  </p>
                  {preview.length > 0 && (
                    <div className='overflow-hidden rounded-md border'>
                      <div className='text-muted-foreground bg-muted/40 grid grid-cols-[1.6fr_1fr_1fr] gap-2 px-2 py-1 text-xs'>
                        <span>{t('Model')}</span>
                        <span className='text-right'>{t('Input $/1M')}</span>
                        <span className='text-right'>{t('Output $/1M')}</span>
                      </div>
                      <div className='divide-y'>
                        {preview.map((item) => (
                          <div
                            key={item.model_name}
                            className='grid grid-cols-[1.6fr_1fr_1fr] gap-2 px-2 py-1 font-mono text-xs'
                          >
                            <span className='truncate'>{item.model_name}</span>
                            <span className='text-right'>
                              {item.input_price.toFixed(4)}
                            </span>
                            <span className='text-right'>
                              {item.output_price.toFixed(4)}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </AlertDescription>
              </Alert>
            )}

            {probeResult && !probeResult.price_fetch_ok && (
              <Alert variant='destructive'>
                <AlertTitle>{t('Ratio auto-fetch unavailable')}</AlertTitle>
                <AlertDescription className='space-y-2'>
                  {probeResult.price_fetch_message && (
                    <p>{probeResult.price_fetch_message}</p>
                  )}
                  {probeResult.warnings?.map((warning) => (
                    <p key={warning}>{warning}</p>
                  ))}
                  <p>
                    {t(
                      'Quotes will list at the platform standard price (rate 1.0). Optionally set a discount below, or adjust the rate later on the channel card.'
                    )}
                  </p>
                </AlertDescription>
              </Alert>
            )}
          </div>

          {upstreamModels.length > 0 && (
            <div className='rounded-lg border p-3'>
              <MerchantModelMappingEditor
                upstreamModels={upstreamModels}
                platformModels={platformModels}
                value={mapping}
                onChange={setMapping}
                merchantUserId={merchantUserId}
                groupRatio={Number(groupRate) > 0 ? Number(groupRate) : 1}
                customPrices={customPrices}
                onCustomPricesChange={setCustomPrices}
                onRemove={(upstream) => {
                  setUpstreamModels((prev) => prev.filter((n) => n !== upstream))
                  setMapping((prev) => {
                    const next = { ...prev }
                    delete next[upstream]
                    return next
                  })
                }}
              />
            </div>
          )}

          <div className='space-y-1.5'>
            <button
              type='button'
              className='text-muted-foreground text-xs underline-offset-2 hover:underline'
              onClick={() => setShowManualModels((v) => !v)}
            >
              {showManualModels
                ? t('Hide manual model list')
                : t("Couldn't pull the model list? Fill in manually")}
            </button>
            {showManualModels && (
              <>
                <Label htmlFor='merchant-model-list'>{t('Listed Models')}</Label>
                <Textarea
                  id='merchant-model-list'
                  className='min-h-32'
                  value={manualModels}
                  onChange={(e) => setManualModels(e.target.value)}
                  placeholder={t('gpt-5.6, pro\ngpt-6')}
                  aria-describedby='merchant-model-list-hint'
                />
                <p
                  id='merchant-model-list-hint'
                  className='text-muted-foreground text-xs'
                >
                  {t('One model per line: model or model, pool')}
                  <br />
                  {t(
                    'The model list is optional: leave it empty to fill the channel model list from the site key at review time.'
                  )}
                </p>
              </>
            )}
          </div>
        </section>
      )}

      {!isExistingMerchant && (
        <div className='bg-muted/20 flex items-center justify-between gap-3 rounded-xl border px-4 py-3'>
          <div className='space-y-0.5'>
            <div className='flex items-center gap-1.5'>
              <ShieldAlert className='text-primary size-4' />
              <Label htmlFor='merchant-agreed-terms'>
                {t('Merchant Settlement & Commission Agreement')}
              </Label>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Upstream API Key (if provided) is encrypted at rest during review. I confirm authentic platform ownership and accept platform settlement & dispute clawback rules.'
              )}
            </p>
          </div>
          <Switch
            id='merchant-agreed-terms'
            checked={agreedTerms}
            onCheckedChange={setAgreedTerms}
          />
        </div>
      )}

      <div className='flex justify-end'>
        <Button onClick={submit} disabled={submitting} size='lg'>
          {submitting ? t('Submitting...') : t('Submit Application')}
        </Button>
      </div>
    </div>
  )
}
