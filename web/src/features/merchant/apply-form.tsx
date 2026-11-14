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
import { RefreshCw } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { useAuthStore } from '@/stores/auth-store'

import {
  getPlatformModels,
  probeMerchantUpstream,
  submitMerchantApplication,
} from './api'
import {
  MerchantModelMappingEditor,
} from './components/model-mapping-editor'
import {
  buildModelQuotesFromMapping,
  type CustomModelPriceDraft,
  validateCustomModelQuotes,
} from './lib/model-mapping'
import type { MerchantModelQuote, MerchantUpstreamProbeResult } from './types'
import {
  UPSTREAM_KIND_AUTO,
  toUpstreamKind,
  upstreamKindOptions,
} from './upstream-kinds'

interface MerchantApplyFormProps {
  defaultGroupName?: string
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
 * 入驻 / 扩容表单。
 *
 * 模型上架走「拉取上游模型 → 与平台官方模型对接」：填好站址与密钥后点
 * 「检测上游并拉取模型」，后端会验证上游、实测倍率自动获取，并带回上游的
 * 模型清单；商家把每个上游模型对接到平台官方模型名，未对接的不上架。
 * 手填清单只作为拉取失败时的兜底。
 */
export function MerchantApplyForm({
  defaultGroupName,
  onSubmitted,
}: MerchantApplyFormProps) {
  const { t } = useTranslation()
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
  // 模型对接：上游模型名 → 平台官方或 m{ID}/... 专属模型名；缺省时同名/近名自动对接
  const merchantUserId = useAuthStore((state) => state.auth.user?.id)
  const [upstreamModels, setUpstreamModels] = useState<string[]>([])
  const [mapping, setMapping] = useState<Record<string, string>>({})
  const [customPrices, setCustomPrices] = useState<
    Record<string, CustomModelPriceDraft>
  >({})
  // 手动清单兜底：拉取失败或上游没有 /v1/models 时展开使用
  const [manualModels, setManualModels] = useState('')
  const [showManualModels, setShowManualModels] = useState(false)

  // 平台在售的官方模型清单（对接下拉的数据源）
  const platformModelsQuery = useQuery({
    queryKey: ['merchant', 'platform-models'],
    queryFn: getPlatformModels,
    staleTime: 5 * 60_000,
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
      toast.error(t('Please fill in Site URL and API Key first'))
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
      // 拉到了模型清单就优先用对接编辑器；什么都没拉到才自动展开手动兜底
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
    if (!siteName || !siteUrl || !siteKey) {
      toast.error(t('Please fill in Site Name, Site URL and API Key'))
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
    const trimmedRate = groupRate.trim()
    if (trimmedRate !== '') {
      const rateNum = Number(trimmedRate)
      if (!Number.isFinite(rateNum) || rateNum <= 0 || rateNum > 100) {
        toast.error(t('分组倍率需在 0（不含）到 100 之间'))
        return
      }
    }

    setSubmitting(true)
    try {
      await submitMerchantApplication({
        site_name: siteName,
        site_url: siteUrl,
        site_key: siteKey,
        upstream_kind: toUpstreamKind(upstreamKind),
        group_name: groupName.trim() || undefined,
        group_desc: groupDesc.trim() || undefined,
        join_auto_group: joinAutoGroup,
        rate_multiplier: trimmedRate !== '' ? Number(trimmedRate) : undefined,
        models: parsed.models,
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

  return (
    <div className='bg-card space-y-4 rounded-xl border p-4'>
      <div className='space-y-1.5'>
        <Label htmlFor='merchant-site-name'>{t('Site Name')}</Label>
        <Input
          id='merchant-site-name'
          value={siteName}
          onChange={(e) => setSiteName(e.target.value)}
          placeholder={t('MyRelay')}
        />
      </div>
      <div className='space-y-1.5'>
        <Label htmlFor='merchant-site-url'>{t('Site URL')}</Label>
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
              {groupName.trim() && siteName.trim() && (
                <span className='text-foreground ml-1 font-mono'>
                  {t('最终分组名：{{name}}', {
                    name: groupName.trim().startsWith(`${siteName.trim()}-`)
                      ? groupName.trim()
                      : `${siteName.trim()}-${groupName.trim()}`,
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
        <div className='flex items-center justify-between gap-3 rounded-md border bg-background px-3 py-2'>
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
                  <div className='text-muted-foreground grid grid-cols-[1.6fr_1fr_1fr] gap-2 bg-muted/40 px-2 py-1 text-xs'>
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

      {/* 模型对接：上游模型 → 平台官方模型；未对接的不上架 */}
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

      {/* 手动清单兜底：上游没拉到模型清单（如 /v1/models 不可用）时使用 */}
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
      <Button onClick={submit} disabled={submitting}>
        {t('Submit Application')}
      </Button>
    </div>
  )
}
