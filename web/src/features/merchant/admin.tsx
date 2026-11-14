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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  deleteChannelQuote,
  getAdminWithdrawals,
  getChannelQuotes,
  getMerchantApplications,
  getMerchantOverview,
  getMerchants,
  probeMerchantApplication,
  reviewMerchantApplication,
  reviewMerchantWithdrawal,
  syncChannelQuotes,
  updateMerchant,
  upsertChannelQuote,
} from './api'
import { UpstreamProbeDialog } from './components/upstream-probe-dialog'
import type {
  ChannelQuote,
  MerchantApplication,
  MerchantModelQuote,
  MerchantReviewResult,
  MerchantWithdrawal,
  UpstreamProbe,
} from './types'
import {
  MERCHANT_CHANNEL_TYPES,
  UPSTREAM_KIND_AUTO,
  toUpstreamKind,
  upstreamKindLabelKey,
  upstreamKindOptions,
} from './upstream-kinds'

/** 管理端列表每页条数，超出后显示「加载更多」。 */
const PAGE_SIZE = 50

/** 报价单价精度：$/1M token，沿用 6 位小数。 */
const QUOTE_PRICE_FORMAT = { digitsLarge: 6, digitsSmall: 6 } as const

/** 渠道类型下拉框中「跟随探测结果」的哨兵值。 */
const FOLLOW_PROBE = 'auto'

/** 上游类型下拉选项：跟随探测结果 / New API / Sub2API / OpenAI 兼容。 */
const REVIEW_UPSTREAM_KIND_OPTIONS = upstreamKindOptions('Follow probe')

/** 自动报价同步开关选项。 */
const PRICE_SYNC_OPTIONS = [
  { value: 'auto', labelKey: 'Follow upstream capability' },
  { value: 'on', labelKey: 'On' },
  { value: 'off', labelKey: 'Off' },
] as const

type PriceSyncChoice = (typeof PRICE_SYNC_OPTIONS)[number]['value']

export function MerchantAdmin() {
  const { t } = useTranslation()
  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-6'>
      <h1 className='mb-4 text-2xl font-bold'>{t('Merchant Management')}</h1>
      <Tabs defaultValue='overview'>
        <TabsList>
          <TabsTrigger value='overview'>{t('Overview')}</TabsTrigger>
          <TabsTrigger value='applications'>{t('Applications')}</TabsTrigger>
          <TabsTrigger value='merchants'>{t('Merchants')}</TabsTrigger>
          <TabsTrigger value='withdrawals'>{t('Withdrawals')}</TabsTrigger>
          <TabsTrigger value='quotes'>{t('Quote Board')}</TabsTrigger>
        </TabsList>
        <TabsContent value='overview'>
          <OverviewTab />
        </TabsContent>
        <TabsContent value='applications'>
          <ApplicationsTab />
        </TabsContent>
        <TabsContent value='merchants'>
          <MerchantsTab />
        </TabsContent>
        <TabsContent value='withdrawals'>
          <AdminWithdrawalsTab />
        </TabsContent>
        <TabsContent value='quotes'>
          <QuoteBoardTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}

/** 总览：全部商家、全部分组、分组对应的商家，两个视角一次看清。 */
function OverviewTab() {
  const { t } = useTranslation()
  const overviewQuery = useQuery({
    queryKey: ['merchant-admin', 'overview'],
    queryFn: getMerchantOverview,
  })
  const merchants = overviewQuery.data?.merchants ?? []
  const pools = overviewQuery.data?.pools ?? []

  return (
    <div className='space-y-6'>
      {/* 分组 → 商家 */}
      <section>
        <h3 className='mb-2 text-sm font-medium'>
          {t('Pool')} → {t('Merchants')}
        </h3>
        <div className='rounded-xl border'>
          <div className='text-muted-foreground grid grid-cols-[1fr_0.6fr_0.6fr_2fr] gap-2 border-b px-3 py-2 text-xs'>
            <span>{t('Pool')}</span>
            <span>{t('Channel')}</span>
            <span>{t('Merchants')}</span>
            <span>{t('Merchant')}</span>
          </div>
          <div className='divide-y'>
            {pools.map((p) => (
              <div
                key={p.pool}
                className='grid grid-cols-[1fr_0.6fr_0.6fr_2fr] items-center gap-2 px-3 py-2 text-sm'
              >
                <span className='truncate font-mono font-medium'>{p.pool}</span>
                <span className='font-mono'>{p.channel_count}</span>
                <span className='font-mono'>{p.merchant_count}</span>
                <span className='text-muted-foreground truncate'>
                  {p.merchant_names.length > 0
                    ? p.merchant_names.join('、')
                    : '--'}
                </span>
              </div>
            ))}
            {!pools.length && (
              <div className='text-muted-foreground px-3 py-8 text-center'>
                {t('No pools available yet')}
              </div>
            )}
          </div>
        </div>
      </section>

      {/* 商家 → 渠道 */}
      <section>
        <h3 className='mb-2 text-sm font-medium'>
          {t('Merchants')} → {t('Channel')}
        </h3>
        <div className='space-y-3'>
          {merchants.map((m) => (
            <div key={m.user_id} className='rounded-xl border p-3'>
              <div className='flex flex-wrap items-center gap-2'>
                <span className='text-sm font-medium'>
                  {m.site_name || m.username}
                </span>
                <span className='text-muted-foreground text-xs'>
                  @{m.username}
                </span>
                <Badge
                  variant={m.status === 1 ? 'default' : 'destructive'}
                >
                  {m.status === 1 ? t('Enabled') : t('Suspended')}
                </Badge>
                <span className='text-muted-foreground ml-auto text-xs'>
                  {t('Balance (USD)')}:{' '}
                  <span className='text-foreground font-mono'>
                    {formatCurrencyFromUSD(m.balance, {
                      digitsLarge: 2,
                      digitsSmall: 2,
                    })}
                  </span>
                </span>
              </div>
              <div className='mt-2 space-y-1.5'>
                {m.channels.map((ch) => (
                  <div
                    key={ch.id}
                    className='flex flex-wrap items-center gap-2 text-xs'
                  >
                    <span className='font-mono'>#{ch.id}</span>
                    <span>{ch.name}</span>
                    {ch.pools.map((pool) => (
                      <Badge key={pool} variant='secondary'>
                        {pool}
                      </Badge>
                    ))}
                    <span className='text-muted-foreground'>
                      {t('Model count')}: {ch.model_count}
                    </span>
                    {ch.price_sync && (
                      <Badge variant='outline'>{t('Price sync on')}</Badge>
                    )}
                    <Badge
                      variant={ch.status === 1 ? 'secondary' : 'destructive'}
                    >
                      {ch.status === 1 ? t('Enabled') : t('Disabled')}
                    </Badge>
                  </div>
                ))}
                {!m.channels.length && (
                  <p className='text-muted-foreground text-xs'>
                    {t('No channels in this pool yet')}
                  </p>
                )}
              </div>
            </div>
          ))}
          {!merchants.length && (
            <div className='text-muted-foreground rounded-xl border px-3 py-8 text-center'>
              {t('No merchants yet')}
            </div>
          )}
        </div>
      </section>
    </div>
  )
}

function formatReviewResultSummary(
  result: MerchantReviewResult,
  t: (key: string, opts?: Record<string, string>) => string
): string {
  if (result.app_type === 'group_change') {
    const rateText =
      typeof result.rate_multiplier === 'number'
        ? result.rate_multiplier.toFixed(2)
        : '1.00'
    const autoText = result.join_auto_group
      ? '，已纳入 Auto 路由'
      : '，未纳入 Auto 路由'
    return `已更新分组 ${result.group_name ?? ''}（倍率 ×${rateText}${autoText}）`
  }
  return t('Channel {{id}} created as {{type}}', {
    id: String(result.channel_id),
    type: t(getChannelTypeLabel(result.channel_type)),
  })
}

function ApplicationsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [reviewing, setReviewing] = useState<MerchantApplication | null>(null)
  const [pools, setPools] = useState('default')
  const [groupName, setGroupName] = useState('')
  const [groupDesc, setGroupDesc] = useState('')
  const [joinAutoGroup, setJoinAutoGroup] = useState(false)
  const [profitRatio, setProfitRatio] = useState('0.95')
  const [note, setNote] = useState('')
  const [upstreamKind, setUpstreamKind] = useState<string>(UPSTREAM_KIND_AUTO)
  const [channelType, setChannelType] = useState<string>(FOLLOW_PROBE)
  const [priceSync, setPriceSync] = useState<PriceSyncChoice>('auto')
  const [rateMultiplier, setRateMultiplier] = useState('')
  const [probing, setProbing] = useState<MerchantApplication | null>(null)
  const [probe, setProbe] = useState<UpstreamProbe | null>(null)
  const [probeLoading, setProbeLoading] = useState(false)
  const [lastReview, setLastReview] = useState<MerchantReviewResult | null>(
    null
  )

  const appsQuery = useQuery({
    queryKey: ['merchant-admin', 'applications', page],
    queryFn: () => getMerchantApplications(page, PAGE_SIZE, -1),
  })
  const items = appsQuery.data?.items ?? []

  const channelTypeItems = [
    { value: FOLLOW_PROBE, label: t('Follow probe') },
    ...MERCHANT_CHANNEL_TYPES.map((type) => ({
      value: String(type),
      label: `${t(getChannelTypeLabel(type))} (${type})`,
    })),
  ]

  /** 每次打开审核弹窗都把上游覆盖项复位，避免沿用上一份申请的覆盖值。 */
  function openReview(application: MerchantApplication) {
    setUpstreamKind(UPSTREAM_KIND_AUTO)
    setChannelType(FOLLOW_PROBE)
    setPriceSync('auto')
    setGroupName(application.group_name ?? '')
    setGroupDesc(application.group_desc ?? '')
    setJoinAutoGroup(Boolean(application.join_auto_group))
    setPools(application.group_name ? application.group_name : 'default')
    // 倍率默认回填申请值，管理员可直接沿用或覆盖
    setRateMultiplier(
      typeof application.rate_multiplier === 'number'
        ? String(application.rate_multiplier)
        : ''
    )
    setReviewing(application)
  }

  async function review(action: 'approve' | 'reject') {
    if (!reviewing) return
    const approve = action === 'approve'
    const isGroupChange = reviewing.app_type === 'group_change'
    try {
      const result = await reviewMerchantApplication(reviewing.id, {
        action,
        review_note: note,
        pools:
          approve && !isGroupChange
            ? pools
                .split(',')
                .map((p) => p.trim())
                .filter(Boolean)
            : undefined,
        group_name:
          approve && groupName.trim() !== '' ? groupName.trim() : undefined,
        group_desc: approve ? groupDesc.trim() : undefined,
        join_auto_group: approve ? joinAutoGroup : undefined,
        profit_ratio:
          approve && !isGroupChange ? Number(profitRatio) || 0.95 : undefined,
        upstream_kind: !isGroupChange
          ? toUpstreamKind(upstreamKind)
          : undefined,
        channel_type:
          approve && !isGroupChange && channelType !== FOLLOW_PROBE
            ? Number(channelType)
            : undefined,
        price_sync:
          approve && !isGroupChange && priceSync !== 'auto'
            ? priceSync === 'on'
            : undefined,
        // 倍率留空表示不覆盖商家申请值，交由后端沿用
        rate_multiplier:
          approve && rateMultiplier.trim() !== ''
            ? Number(rateMultiplier)
            : undefined,
      })
      toast.success(approve ? t('Approved') : t('Rejected'))
      setReviewing(null)
      setLastReview(approve ? result : null)
      queryClient.invalidateQueries({
        queryKey: ['merchant-admin', 'applications'],
      })
      queryClient.invalidateQueries({
        queryKey: ['merchant-admin', 'overview'],
      })
    } catch (error) {
      handleServerError(error, t('Failed to review application'))
    }
  }

  async function runProbe(application: MerchantApplication) {
    setProbing(application)
    setProbe(null)
    setProbeLoading(true)
    try {
      setProbe(await probeMerchantApplication(application.id))
    } catch (error) {
      handleServerError(error, t('Failed to probe upstream'))
      setProbing(null)
    } finally {
      setProbeLoading(false)
    }
  }

  let quotes: MerchantModelQuote[] = []
  if (reviewing) {
    try {
      quotes = JSON.parse(reviewing.models) as MerchantModelQuote[]
    } catch {
      quotes = []
    }
  }

  return (
    <div className='space-y-3'>
      {lastReview && (
        <Alert>
          <AlertTitle>{t('Last review result')}</AlertTitle>
          <AlertDescription className='space-y-1'>
            <p>{formatReviewResultSummary(lastReview, t)}</p>
            {lastReview.app_type !== 'group_change' && (
              <p>
                {lastReview.price_sync
                  ? t('Automatic price sync is enabled for this channel.')
                  : t(
                      'Automatic price sync is disabled: this upstream exposes no price API, so keep the channel quotes updated manually.'
                    )}
              </p>
            )}
            {(lastReview.probe?.warnings ?? []).length > 0 && (
              <ul className='list-disc space-y-1 pl-4'>
                {(lastReview.probe?.warnings ?? []).map((warning) => (
                  <li key={warning}>{warning}</li>
                ))}
              </ul>
            )}
          </AlertDescription>
        </Alert>
      )}

      <div className='rounded-xl border'>
        <div className='text-muted-foreground grid grid-cols-[1fr_1.1fr_1fr_1fr_auto_auto] gap-2 border-b px-3 py-2 text-xs'>
          <span>{t('Site')}</span>
          <span>{t('类型 / 分组与倍率')}</span>
          <span>{t('Applicant')}</span>
          <span>{t('Time')}</span>
          <span>{t('Status')}</span>
          <span className='w-32 text-right'>{t('Actions')}</span>
        </div>
        <div className='divide-y'>
          {items.map((a) => (
            <div
              key={a.id}
              className='grid grid-cols-[1fr_1.1fr_1fr_1fr_auto_auto] items-center gap-2 px-3 py-2 text-sm'
            >
              <a
                href={a.site_url}
                target='_blank'
                rel='noreferrer'
                className='truncate hover:underline'
              >
                {a.site_name}
              </a>
              <div className='flex flex-wrap items-center gap-1 text-xs'>
                <Badge variant='outline'>
                  {a.app_type === 'group_change'
                    ? t('分组变更')
                    : t('新渠道/分组')}
                </Badge>
                {a.group_name && (
                  <span className='font-mono font-medium'>{a.group_name}</span>
                )}
                <Badge variant='secondary'>
                  {`×${(a.rate_multiplier || 1).toFixed(2)}`}
                </Badge>
                {a.join_auto_group && (
                  <Badge variant='secondary'>Auto</Badge>
                )}
                {a.app_type !== 'group_change' && (
                  <span className='text-muted-foreground truncate'>
                    {t(upstreamKindLabelKey(a.upstream_kind))}
                  </span>
                )}
              </div>
              <span>#{a.user_id}</span>
              <span className='text-muted-foreground'>
                {formatTimestampToDate(a.created_at)}
              </span>
              <span>
                {a.status === 0 && (
                  <Badge variant='secondary'>{t('Pending Review')}</Badge>
                )}
                {a.status === 1 && (
                  <Badge variant='default'>{t('Approved')}</Badge>
                )}
                {a.status === 2 && (
                  <Badge variant='destructive'>{t('Rejected')}</Badge>
                )}
              </span>
              <span className='flex w-32 justify-end gap-1'>
                {a.status === 0 && (
                  <>
                    {a.app_type !== 'group_change' && (
                      <Button
                        size='sm'
                        variant='outline'
                        className='h-7'
                        disabled={probeLoading}
                        onClick={() => runProbe(a)}
                      >
                        {t('Probe')}
                      </Button>
                    )}
                    <Button
                      size='sm'
                      variant='outline'
                      className='h-7'
                      onClick={() => openReview(a)}
                    >
                      {t('Review')}
                    </Button>
                  </>
                )}
              </span>
            </div>
          ))}
          {!items.length && (
            <div className='text-muted-foreground px-3 py-8 text-center'>
              {t('No applications yet')}
            </div>
          )}
        </div>
        {(appsQuery.data?.total ?? 0) > page * PAGE_SIZE && (
          <div className='border-t p-2 text-center'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => setPage(page + 1)}
            >
              {t('Load more')}
            </Button>
          </div>
        )}
      </div>

      <UpstreamProbeDialog
        application={probing}
        probe={probe}
        loading={probeLoading}
        onOpenChange={(open) => {
          if (!open) {
            setProbing(null)
            setProbe(null)
          }
        }}
      />

      <Dialog open={!!reviewing} onOpenChange={(o) => !o && setReviewing(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Review')}: {reviewing?.site_name}
            </DialogTitle>
          </DialogHeader>
          <div className='space-y-3'>
            <div className='rounded-md border p-2 text-xs'>
              <p className='font-medium'>
                {reviewing?.app_type === 'group_change'
                  ? t('专属分组倍率 / Auto 路由变更申请')
                  : t('新渠道与分组入驻申请')}
              </p>
              <p className='text-muted-foreground mt-1'>
                {t('目标专属分组')}：{' '}
                <span className='font-mono'>
                  {reviewing?.group_name || '--'}
                </span>
                {' · '}
                {t('申请倍率')}：{' '}
                {typeof reviewing?.rate_multiplier === 'number'
                  ? `${reviewing.rate_multiplier.toFixed(4)}x`
                  : '--'}
                {' · '}
                Auto：{' '}
                {reviewing?.join_auto_group ? t('申请加入') : t('不加入')}
              </p>
            </div>

            {reviewing?.app_type !== 'group_change' && (
              <>
                <div className='rounded-md border p-2 text-xs'>
                  <p className='mb-1 font-medium'>{t('Requested models')}</p>
                  {quotes.map((q) => (
                    <p key={q.model} className='font-mono'>
                      {q.model}
                      {q.pool ? `, ${q.pool}` : ''}
                    </p>
                  ))}
                  {!quotes.length && (
                    <p className='text-muted-foreground'>
                      {t(
                        'No models declared: the channel model list is filled from the site key at review time.'
                      )}
                    </p>
                  )}
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor='merchant-review-upstream-kind'>
                    {t('Upstream type')}
                  </Label>
                  <Select
                    items={REVIEW_UPSTREAM_KIND_OPTIONS.map((option) => ({
                      value: option.value,
                      label: t(option.labelKey),
                    }))}
                    value={upstreamKind}
                    onValueChange={(next) => {
                      if (next !== null) setUpstreamKind(next)
                    }}
                  >
                    <SelectTrigger
                      id='merchant-review-upstream-kind'
                      className='w-full'
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      {REVIEW_UPSTREAM_KIND_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {t(option.labelKey)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor='merchant-review-channel-type'>
                    {t('Channel type')}
                  </Label>
                  <Select
                    items={channelTypeItems}
                    value={channelType}
                    onValueChange={(next) => {
                      if (next !== null) setChannelType(next)
                    }}
                  >
                    <SelectTrigger
                      id='merchant-review-channel-type'
                      className='w-full'
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      {channelTypeItems.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className='space-y-1.5'>
                  <Label>{t('Price sync')}</Label>
                  <ToggleGroup
                    value={[priceSync]}
                    onValueChange={(values) => {
                      const choice = PRICE_SYNC_OPTIONS.find(
                        (option) => option.value === values[0]
                      )
                      if (choice) setPriceSync(choice.value)
                    }}
                    variant='outline'
                    size='sm'
                    aria-label={t('Price sync')}
                  >
                    {PRICE_SYNC_OPTIONS.map((option) => (
                      <ToggleGroupItem key={option.value} value={option.value}>
                        {t(option.labelKey)}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor='merchant-review-pools'>
                    {t('Pools (comma separated)')}
                  </Label>
                  <Input
                    id='merchant-review-pools'
                    value={pools}
                    onChange={(e) => setPools(e.target.value)}
                    placeholder={t('gpt-pro, gpt-plus')}
                  />
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor='merchant-review-profit-ratio'>
                    {t('Profit ratio (0~1)')}
                  </Label>
                  <Input
                    id='merchant-review-profit-ratio'
                    value={profitRatio}
                    onChange={(e) => setProfitRatio(e.target.value)}
                  />
                </div>
              </>
            )}

            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-review-group-name'>
                  {t('专属分组名称')}
                </Label>
                <Input
                  id='merchant-review-group-name'
                  value={groupName}
                  readOnly={reviewing?.app_type === 'group_change'}
                  onChange={(e) => setGroupName(e.target.value)}
                  placeholder='my-shop-vip'
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='merchant-review-rate-multiplier'>
                  {t('分组倍率（GroupRatio）')}
                </Label>
                <Input
                  id='merchant-review-rate-multiplier'
                  type='number'
                  step='0.01'
                  value={rateMultiplier}
                  onChange={(e) => setRateMultiplier(e.target.value)}
                  placeholder={t('Leave empty to keep the requested value')}
                />
              </div>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='merchant-review-group-desc'>{t('分组说明')}</Label>
              <Input
                id='merchant-review-group-desc'
                value={groupDesc}
                onChange={(e) => setGroupDesc(e.target.value)}
                placeholder={t('向终端用户展示的分组说明')}
              />
            </div>
            <div className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
              <Label htmlFor='merchant-review-join-auto'>
                {t('加入 Auto 自动路由组（AutoGroups）')}
              </Label>
              <Switch
                id='merchant-review-join-auto'
                checked={joinAutoGroup}
                onCheckedChange={setJoinAutoGroup}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='merchant-review-note'>{t('Review Note')}</Label>
              <Input
                id='merchant-review-note'
                value={note}
                onChange={(e) => setNote(e.target.value)}
              />
            </div>
            <div className='flex gap-2'>
              <Button onClick={() => review('approve')}>{t('Approve')}</Button>
              <Button variant='destructive' onClick={() => review('reject')}>
                {t('Reject')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function MerchantsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const merchantsQuery = useQuery({
    queryKey: ['merchant-admin', 'merchants', page],
    queryFn: () => getMerchants(page, PAGE_SIZE),
  })
  const items = merchantsQuery.data?.items ?? []

  async function toggleStatus(id: number, status: number) {
    try {
      await updateMerchant(id, { status })
      queryClient.invalidateQueries({
        queryKey: ['merchant-admin', 'merchants'],
      })
    } catch (error) {
      handleServerError(error, t('Failed to update merchant'))
    }
  }

  return (
    <div className='rounded-xl border'>
      <div className='text-muted-foreground grid grid-cols-[1fr_1fr_1fr_1fr_auto] gap-2 border-b px-3 py-2 text-xs'>
        <span>{t('Site')}</span>
        <span>{t('Username')}</span>
        <span>{t('Balance (USD)')}</span>
        <span>{t('Profit ratio')}</span>
        <span className='w-24 text-right'>{t('Actions')}</span>
      </div>
      <div className='divide-y'>
        {items.map((m) => (
          <div
            key={m.id}
            className='grid grid-cols-[1fr_1fr_1fr_1fr_auto] items-center gap-2 px-3 py-2 text-sm'
          >
            <a
              href={m.site_url}
              target='_blank'
              rel='noreferrer'
              className='truncate hover:underline'
            >
              {m.site_name}
              {m.status !== 1 && (
                <Badge variant='destructive' className='ml-2'>
                  {t('Suspended')}
                </Badge>
              )}
            </a>
            <span>{m.username || `#${m.user_id}`}</span>
            <span className='font-mono'>
              {formatCurrencyFromUSD(m.wallet?.balance, {
                digitsLarge: 4,
                digitsSmall: 4,
              })}
            </span>
            <span>{(m.profit_ratio * 100).toFixed(0)}%</span>
            <span className='flex w-24 justify-end'>
              <Button
                size='sm'
                variant='outline'
                className='h-7'
                onClick={() => toggleStatus(m.id, m.status === 1 ? 2 : 1)}
              >
                {m.status === 1 ? t('Suspend') : t('Enable')}
              </Button>
            </span>
          </div>
        ))}
        {!items.length && (
          <div className='text-muted-foreground px-3 py-8 text-center'>
            {t('No merchants yet')}
          </div>
        )}
      </div>
      {(merchantsQuery.data?.total ?? 0) > page * PAGE_SIZE && (
        <div className='border-t p-2 text-center'>
          <Button variant='outline' size='sm' onClick={() => setPage(page + 1)}>
            {t('Load more')}
          </Button>
        </div>
      )}
    </div>
  )
}

function AdminWithdrawalsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [proof, setProof] = useState('')

  const withdrawalsQuery = useQuery({
    queryKey: ['merchant-admin', 'withdrawals', page],
    queryFn: () => getAdminWithdrawals(page, PAGE_SIZE, -1),
  })
  const items = withdrawalsQuery.data?.items ?? []

  async function review(id: number, action: 'approve' | 'reject' | 'paid') {
    try {
      await reviewMerchantWithdrawal(id, {
        action,
        paid_proof: action === 'paid' ? proof : undefined,
      })
      toast.success(t('Done'))
      queryClient.invalidateQueries({
        queryKey: ['merchant-admin', 'withdrawals'],
      })
    } catch (error) {
      handleServerError(error, t('Failed to review withdrawal'))
    }
  }

  const statusBadge = (s: number) => {
    if (s === 0) return <Badge variant='secondary'>{t('Pending')}</Badge>
    if (s === 1) return <Badge variant='default'>{t('Approved')}</Badge>
    if (s === 2) return <Badge variant='default'>{t('Paid')}</Badge>
    return <Badge variant='destructive'>{t('Rejected')}</Badge>
  }

  return (
    <div className='rounded-xl border'>
      <div className='border-b px-3 py-2'>
        <Label
          htmlFor='merchant-withdrawal-proof'
          className='mb-1 block text-xs'
        >
          {t('Payment proof (for mark-as-paid)')}
        </Label>
        <Input
          id='merchant-withdrawal-proof'
          value={proof}
          onChange={(e) => setProof(e.target.value)}
          placeholder={t('Transaction ID / screenshot link')}
        />
      </div>
      <div className='text-muted-foreground grid grid-cols-[1fr_1fr_1fr_1fr_auto] gap-2 border-b px-3 py-2 text-xs'>
        <span>{t('Time')}</span>
        <span>{t('Merchant')}</span>
        <span>{t('Amount (USD)')}</span>
        <span>{t('Status')}</span>
        <span className='w-44 text-right'>{t('Actions')}</span>
      </div>
      <div className='divide-y'>
        {items.map((w: MerchantWithdrawal) => (
          <div
            key={w.id}
            className='grid grid-cols-[1fr_1fr_1fr_1fr_auto] items-center gap-2 px-3 py-2 text-sm'
          >
            <span className='text-muted-foreground'>
              {formatTimestampToDate(w.created_at)}
            </span>
            <span>#{w.merchant_user_id}</span>
            <span className='font-mono'>
              {formatCurrencyFromUSD(w.amount, {
                digitsLarge: 2,
                digitsSmall: 2,
              })}
            </span>
            <span>{statusBadge(w.status)}</span>
            <span className='flex w-44 justify-end gap-1'>
              {w.status === 0 && (
                <>
                  <Button
                    size='sm'
                    variant='outline'
                    className='h-7'
                    onClick={() => review(w.id, 'approve')}
                  >
                    {t('Approve')}
                  </Button>
                  <Button
                    size='sm'
                    variant='outline'
                    className='h-7'
                    onClick={() => review(w.id, 'reject')}
                  >
                    {t('Reject')}
                  </Button>
                </>
              )}
              {w.status === 1 && (
                <Button
                  size='sm'
                  className='h-7'
                  onClick={() => review(w.id, 'paid')}
                >
                  {t('Mark Paid')}
                </Button>
              )}
            </span>
          </div>
        ))}
        {!items.length && (
          <div className='text-muted-foreground px-3 py-8 text-center'>
            {t('No withdrawals yet')}
          </div>
        )}
      </div>
      {(withdrawalsQuery.data?.total ?? 0) > page * PAGE_SIZE && (
        <div className='border-t p-2 text-center'>
          <Button variant='outline' size='sm' onClick={() => setPage(page + 1)}>
            {t('Load more')}
          </Button>
        </div>
      )}
    </div>
  )
}

/** 盘口报价总表：列表 + 手动报价 + 删除 + 触发同步。 */
function QuoteBoardTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<ChannelQuote | null>(null)
  const [creating, setCreating] = useState(false)
  const [deleting, setDeleting] = useState<ChannelQuote | null>(null)
  const [syncing, setSyncing] = useState(false)

  const quotesQuery = useQuery({
    queryKey: ['merchant-admin', 'quotes', page],
    queryFn: () => getChannelQuotes(page, PAGE_SIZE),
  })
  const items = quotesQuery.data?.items ?? []

  function refreshQuotes() {
    queryClient.invalidateQueries({ queryKey: ['merchant-admin', 'quotes'] })
  }

  async function sync() {
    setSyncing(true)
    try {
      await syncChannelQuotes()
      toast.success(t('Sync completed'))
      refreshQuotes()
    } catch (error) {
      handleServerError(error, t('Failed to sync quotes'))
    } finally {
      setSyncing(false)
    }
  }

  async function remove() {
    if (!deleting) return
    try {
      await deleteChannelQuote(deleting.id)
      toast.success(t('Quote deleted'))
      setDeleting(null)
      refreshQuotes()
    } catch (error) {
      handleServerError(error, t('Failed to delete quote'))
    }
  }

  return (
    <div className='rounded-xl border'>
      <div className='flex items-center justify-between gap-2 border-b px-3 py-2'>
        <p className='text-sm font-medium'>{t('Quote Board')}</p>
        <div className='flex gap-2'>
          <Button size='sm' variant='outline' onClick={() => setCreating(true)}>
            {t('New Quote')}
          </Button>
          <Button size='sm' variant='outline' onClick={sync} disabled={syncing}>
            {t('Sync now')}
          </Button>
        </div>
      </div>
      <div className='text-muted-foreground grid grid-cols-[1fr_1fr_auto_1fr_1fr_auto_auto] gap-2 border-b px-3 py-2 text-xs'>
        <span>{t('Pool Group')}</span>
        <span>{t('Model')}</span>
        <span>{t('Channel')}</span>
        <span>{t('Input $/1M')}</span>
        <span>{t('Output $/1M')}</span>
        <span>{t('Source')}</span>
        <span className='w-28 text-right'>{t('Actions')}</span>
      </div>
      <div className='divide-y'>
        {items.map((q) => (
          <div
            key={q.id}
            className='grid grid-cols-[1fr_1fr_auto_1fr_1fr_auto_auto] items-center gap-2 px-3 py-2 text-sm'
          >
            <span className='truncate font-mono'>{q.pool_group}</span>
            <span className='truncate font-mono'>{q.model_name}</span>
            <span className='font-mono'>#{q.channel_id}</span>
            <span className='font-mono'>
              {formatCurrencyFromUSD(q.input_price, QUOTE_PRICE_FORMAT)}
            </span>
            <span className='font-mono'>
              {formatCurrencyFromUSD(q.output_price, QUOTE_PRICE_FORMAT)}
            </span>
            <span>
              {(() => {
                if (q.source === 'derived') return t('Derived')
                if (q.source === 'manual') return t('Manual')
                return t('Auto')
              })()}
            </span>
            <span className='flex w-28 justify-end gap-1'>
              <Button
                size='sm'
                variant='outline'
                className='h-7'
                onClick={() => setEditing(q)}
              >
                {t('Edit')}
              </Button>
              <Button
                size='sm'
                variant='outline'
                className='text-destructive h-7'
                onClick={() => setDeleting(q)}
              >
                {t('Delete')}
              </Button>
            </span>
          </div>
        ))}
        {!items.length && (
          <div className='text-muted-foreground px-3 py-8 text-center'>
            {t('No quotes yet')}
          </div>
        )}
      </div>
      {(quotesQuery.data?.total ?? 0) > page * PAGE_SIZE && (
        <div className='border-t p-2 text-center'>
          <Button variant='outline' size='sm' onClick={() => setPage(page + 1)}>
            {t('Load more')}
          </Button>
        </div>
      )}

      <Dialog
        open={creating || !!editing}
        onOpenChange={(open) => {
          if (!open) {
            setCreating(false)
            setEditing(null)
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editing
                ? `${t('Edit Quote')}: ${editing.model_name}`
                : t('New Quote')}
            </DialogTitle>
          </DialogHeader>
          <AdminQuoteForm
            quote={editing}
            onSaved={() => {
              setCreating(false)
              setEditing(null)
              refreshQuotes()
            }}
          />
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t('Delete quote')}
        desc={t('Are you sure you want to delete this quote?')}
        confirmText={t('Delete')}
        destructive
        handleConfirm={remove}
        className='sm:max-w-sm'
      />
    </div>
  )
}

/** 手动报价表单：source=manual，不会被后续自动同步覆盖。 */
function AdminQuoteForm({
  quote,
  onSaved,
}: {
  quote: ChannelQuote | null
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [channelId, setChannelId] = useState(String(quote?.channel_id ?? ''))
  const [poolGroup, setPoolGroup] = useState(quote?.pool_group ?? '')
  const [modelName, setModelName] = useState(quote?.model_name ?? '')
  const [inputPrice, setInputPrice] = useState(
    quote ? String(quote.input_price) : ''
  )
  const [outputPrice, setOutputPrice] = useState(
    quote ? String(quote.output_price) : ''
  )
  const [saving, setSaving] = useState(false)

  const complete =
    Boolean(channelId && poolGroup && modelName) &&
    Boolean(inputPrice || outputPrice)

  async function save() {
    setSaving(true)
    try {
      await upsertChannelQuote({
        channel_id: Number(channelId) || 0,
        pool_group: poolGroup,
        model_name: modelName,
        input_price: Number(inputPrice) || 0,
        output_price: Number(outputPrice) || 0,
      })
      toast.success(t('Quote saved'))
      onSaved()
    } catch (error) {
      handleServerError(error, t('Failed to save quote'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className='space-y-3'>
      <div className='space-y-1.5'>
        <Label htmlFor='channel-quote-channel-id'>{t('Channel ID')}</Label>
        <Input
          id='channel-quote-channel-id'
          value={channelId}
          onChange={(e) => setChannelId(e.target.value)}
        />
      </div>
      <div className='space-y-1.5'>
        <Label htmlFor='channel-quote-pool-group'>{t('Pool Group')}</Label>
        <Input
          id='channel-quote-pool-group'
          value={poolGroup}
          onChange={(e) => setPoolGroup(e.target.value)}
        />
      </div>
      <div className='space-y-1.5'>
        <Label htmlFor='channel-quote-model-name'>{t('Model Name')}</Label>
        <Input
          id='channel-quote-model-name'
          value={modelName}
          onChange={(e) => setModelName(e.target.value)}
        />
      </div>
      <div className='grid grid-cols-2 gap-3'>
        <div className='space-y-1.5'>
          <Label htmlFor='channel-quote-input-price'>{t('Input $/1M')}</Label>
          <Input
            id='channel-quote-input-price'
            value={inputPrice}
            onChange={(e) => setInputPrice(e.target.value)}
          />
        </div>
        <div className='space-y-1.5'>
          <Label htmlFor='channel-quote-output-price'>{t('Output $/1M')}</Label>
          <Input
            id='channel-quote-output-price'
            value={outputPrice}
            onChange={(e) => setOutputPrice(e.target.value)}
          />
        </div>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t('Manual quotes override auto-synced prices for this model.')}
      </p>
      <Button onClick={save} disabled={saving || !complete}>
        {t('Save')}
      </Button>
    </div>
  )
}
