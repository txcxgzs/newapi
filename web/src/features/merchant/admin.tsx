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
import {
  CheckCircle2,
  ClipboardCheck,
  ExternalLink,
  FileSpreadsheet,
  Layers,
  QrCode,
  ReceiptText,
  RefreshCw,
  Store,
  Users,
  Wallet,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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
import { cn } from '@/lib/utils'

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
import {
  OPERATION_TENURE_OPTIONS,
  isValidPayoutQrDataUrl,
} from './lib/payout-qrcode'
import { getPayoutMethodLabel } from './payout-methods'
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
  const queryClient = useQueryClient()

  const pendingAppsQuery = useQuery({
    queryKey: ['merchant-admin', 'pending-apps-count'],
    queryFn: () => getMerchantApplications(1, 1, 0),
    staleTime: 30_000,
  })
  const pendingWithdrawalsQuery = useQuery({
    queryKey: ['merchant-admin', 'pending-withdrawals-count'],
    queryFn: () => getAdminWithdrawals(1, 1, 0),
    staleTime: 30_000,
  })
  const pendingAppsCount = pendingAppsQuery.data?.total ?? 0
  const pendingWithdrawalsCount = pendingWithdrawalsQuery.data?.total ?? 0

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex items-center gap-2.5'>
          <span className='border-primary/20 bg-primary/10 text-primary flex size-9 items-center justify-center rounded-xl border'>
            <Store className='size-5' />
          </span>
          <span>{t('Merchant Management')}</span>
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          size='sm'
          variant='outline'
          onClick={() =>
            queryClient.invalidateQueries({ queryKey: ['merchant-admin'] })
          }
        >
          <RefreshCw className='mr-1.5 size-3.5' />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <Tabs defaultValue='overview' className='space-y-4'>
          <TabsList className='flex-wrap'>
            <TabsTrigger value='overview' className='gap-1.5'>
              <Layers className='size-3.5' />
              {t('Overview')}
            </TabsTrigger>
            <TabsTrigger value='applications' className='gap-1.5'>
              <ClipboardCheck className='size-3.5' />
              <span>{t('Applications')}</span>
              {pendingAppsCount > 0 && (
                <Badge
                  variant='destructive'
                  className='h-4 px-1.5 text-[10px] leading-none'
                >
                  {pendingAppsCount}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value='merchants' className='gap-1.5'>
              <Users className='size-3.5' />
              {t('Merchant List')}
            </TabsTrigger>
            <TabsTrigger value='withdrawals' className='gap-1.5'>
              <ReceiptText className='size-3.5' />
              <span>{t('Withdrawals')}</span>
              {pendingWithdrawalsCount > 0 && (
                <Badge
                  variant='destructive'
                  className='h-4 px-1.5 text-[10px] leading-none'
                >
                  {pendingWithdrawalsCount}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value='quotes' className='gap-1.5'>
              <FileSpreadsheet className='size-3.5' />
              {t('Quote Board')}
            </TabsTrigger>
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
      </SectionPageLayout.Content>
    </SectionPageLayout>
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
  const activeMerchantsCount = merchants.filter((m) => m.status === 1).length
  const totalWalletBalance = merchants.reduce(
    (sum, m) => sum + (Number.isFinite(m.balance) ? m.balance : 0),
    0
  )

  return (
    <div className='space-y-6'>
      <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
        <Card className='border-sky-500/20 bg-gradient-to-br from-sky-500/5 via-transparent to-transparent'>
          <CardHeader className='flex flex-row items-center justify-between pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {t('Merchant')}
            </CardTitle>
            <Users className='size-4 text-sky-500' />
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-bold tabular-nums'>
              {merchants.length}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Enabled')}: {activeMerchantsCount}
            </p>
          </CardContent>
        </Card>

        <Card className='border-emerald-500/20 bg-gradient-to-br from-emerald-500/5 via-transparent to-transparent'>
          <CardHeader className='flex flex-row items-center justify-between pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {t('Enabled')}
            </CardTitle>
            <CheckCircle2 className='size-4 text-emerald-500' />
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-bold tabular-nums'>
              {activeMerchantsCount}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Suspended')}: {merchants.length - activeMerchantsCount}
            </p>
          </CardContent>
        </Card>

        <Card className='border-violet-500/20 bg-gradient-to-br from-violet-500/5 via-transparent to-transparent'>
          <CardHeader className='flex flex-row items-center justify-between pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {t('Group')}
            </CardTitle>
            <Layers className='size-4 text-violet-500' />
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-bold tabular-nums'>
              {pools.length}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Channel')}:{' '}
              {pools.reduce((sum, p) => sum + (p.channel_count ?? 0), 0)}
            </p>
          </CardContent>
        </Card>

        <Card className='border-amber-500/20 bg-gradient-to-br from-amber-500/5 via-transparent to-transparent'>
          <CardHeader className='flex flex-row items-center justify-between pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-medium'>
              {t('Balance (USD)')}
            </CardTitle>
            <Wallet className='size-4 text-amber-500' />
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-bold tabular-nums'>
              {formatCurrencyFromUSD(totalWalletBalance, {
                digitsLarge: 2,
                digitsSmall: 2,
              })}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Merchant Count')}: {merchants.length}
            </p>
          </CardContent>
        </Card>
      </div>

      {/* 分组 → 商家 */}
      <section className='space-y-2.5'>
        <h3 className='text-sm font-semibold'>
          {t('Group')} → {t('Merchant')}
        </h3>
        <div className='overflow-x-auto rounded-xl border'>
          <div className='min-w-[540px]'>
            <div className='bg-muted/40 text-muted-foreground grid grid-cols-[1fr_0.6fr_0.6fr_2fr] gap-2 border-b px-4 py-2.5 text-xs font-medium'>
              <span>{t('Group')}</span>
              <span>{t('Channel')}</span>
              <span>{t('Merchant Count')}</span>
              <span>{t('Merchant')}</span>
            </div>
            <div className='divide-y'>
              {pools.map((p) => (
                <div
                  key={p.pool}
                  className='hover:bg-muted/35 grid grid-cols-[1fr_0.6fr_0.6fr_2fr] items-center gap-2 px-4 py-2.5 text-sm transition-colors'
                >
                  <span className='truncate font-mono font-medium'>{p.pool}</span>
                  <span className='font-mono tabular-nums'>{p.channel_count}</span>
                  <span className='font-mono tabular-nums'>{p.merchant_count}</span>
                  <span className='text-muted-foreground truncate'>
                    {p.merchant_names.length > 0
                      ? p.merchant_names.join('、')
                      : '--'}
                  </span>
                </div>
              ))}
              {!pools.length && (
                <div className='text-muted-foreground flex flex-col items-center gap-2 px-3 py-10 text-center text-sm'>
                  <Layers className='text-muted-foreground/50 size-7' />
                  <span>{t('No pools available yet')}</span>
                </div>
              )}
            </div>
          </div>
        </div>
      </section>

      {/* 商家 → 渠道 */}
      <section className='space-y-2.5'>
        <h3 className='text-sm font-semibold'>
          {t('Merchant')} → {t('Channel')}
        </h3>
        <div className='grid gap-3 md:grid-cols-2'>
          {merchants.map((m) => (
            <Card key={m.user_id} className='overflow-hidden'>
              <CardContent className='space-y-3 py-4'>
                <div className='flex flex-wrap items-center gap-2'>
                  <div className='bg-primary/10 text-primary flex size-7 shrink-0 items-center justify-center rounded-lg'>
                    <Store className='size-3.5' />
                  </div>
                  <span className='text-sm font-semibold'>
                    {m.shop_name || m.site_name || m.username}
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
                    <span className='text-foreground font-mono font-semibold tabular-nums'>
                      {formatCurrencyFromUSD(m.balance, {
                        digitsLarge: 2,
                        digitsSmall: 2,
                      })}
                    </span>
                  </span>
                </div>
                {m.platform_url && (
                  <a
                    href={m.platform_url}
                    target='_blank'
                    rel='noreferrer'
                    className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs hover:underline'
                  >
                    <ExternalLink className='size-3' />
                    {m.platform_url}
                  </a>
                )}
                <div className='space-y-1.5 border-t pt-2.5'>
                  {m.channels.map((ch) => (
                    <div
                      key={ch.id}
                      className='bg-muted/30 flex flex-wrap items-center gap-2 rounded-lg px-2.5 py-1.5 text-xs'
                    >
                      <span className='font-mono font-medium'>#{ch.id}</span>
                      <span className='font-medium'>{ch.name}</span>
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
                        className='ml-auto'
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
              </CardContent>
            </Card>
          ))}
          {!merchants.length && (
            <div className='text-muted-foreground col-span-full flex flex-col items-center gap-2 rounded-xl border px-3 py-10 text-center text-sm'>
              <Store className='text-muted-foreground/50 size-7' />
              <span>{t('No merchants yet')}</span>
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
  if (result.app_type === 'profile_change') {
    return t('Shop Name updated to {{name}}', { name: result.shop_name ?? '' })
  }
  if (result.qualification_only) {
    return t('Merchant "{{name}}" approved (qualification onboarding complete)', {
      name: result.shop_name ?? '',
    })
  }
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
  const [statusFilter, setStatusFilter] = useState<number>(-1)
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
    queryKey: ['merchant-admin', 'applications', page, statusFilter],
    queryFn: () => getMerchantApplications(page, PAGE_SIZE, statusFilter),
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
    const isProfileChange = reviewing.app_type === 'profile_change'
    const isQualificationOnly =
      !isGroupChange && !isProfileChange && !reviewing.site_url
    try {
      const result = await reviewMerchantApplication(reviewing.id, {
        action,
        review_note: note,
        pools:
          approve && !isGroupChange && !isProfileChange && !isQualificationOnly
            ? pools
                .split(',')
                .map((p) => p.trim())
                .filter(Boolean)
            : undefined,
        group_name:
          approve && !isProfileChange && !isQualificationOnly && groupName.trim() !== ''
            ? groupName.trim()
            : undefined,
        group_desc:
          approve && !isProfileChange && !isQualificationOnly
            ? groupDesc.trim()
            : undefined,
        join_auto_group:
          approve && !isProfileChange && !isQualificationOnly
            ? joinAutoGroup
            : undefined,
        profit_ratio:
          approve && !isGroupChange && !isProfileChange
            ? Number(profitRatio) || 0.95
            : undefined,
        upstream_kind:
          !isGroupChange && !isProfileChange && !isQualificationOnly
            ? toUpstreamKind(upstreamKind)
            : undefined,
        channel_type:
          approve &&
          !isGroupChange &&
          !isProfileChange &&
          !isQualificationOnly &&
          channelType !== FOLLOW_PROBE
            ? Number(channelType)
            : undefined,
        price_sync:
          approve &&
          !isGroupChange &&
          !isProfileChange &&
          !isQualificationOnly &&
          priceSync !== 'auto'
            ? priceSync === 'on'
            : undefined,
        // 倍率留空表示不覆盖商家申请值，交由后端沿用
        rate_multiplier:
          approve &&
          !isProfileChange &&
          !isQualificationOnly &&
          rateMultiplier.trim() !== ''
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
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='bg-muted/30 inline-flex items-center rounded-lg border p-0.5 text-xs'>
          {[
            { value: -1, label: t('All') },
            { value: 0, label: t('Pending Review') },
            { value: 1, label: t('Approved') },
            { value: 2, label: t('Rejected') },
          ].map((opt) => (
            <button
              key={opt.value}
              type='button'
              onClick={() => {
                setStatusFilter(opt.value)
                setPage(1)
              }}
              className={cn(
                'rounded-md px-2.5 py-1 transition-colors',
                statusFilter === opt.value
                  ? 'bg-background text-foreground font-medium shadow-xs'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {opt.label}
            </button>
          ))}
        </div>
      </div>

      {lastReview && (
        <Alert>
          <AlertTitle>{t('Last review result')}</AlertTitle>
          <AlertDescription className='space-y-1'>
            <p>{formatReviewResultSummary(lastReview, t)}</p>
            {lastReview.app_type !== 'group_change' &&
              lastReview.app_type !== 'profile_change' &&
              !lastReview.qualification_only && (
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

      <div className='overflow-x-auto rounded-xl border'>
        <div className='min-w-[740px]'>
          <div className='bg-muted/40 text-muted-foreground grid grid-cols-[1fr_1.1fr_1fr_1fr_auto_auto] gap-2 border-b px-4 py-2.5 text-xs font-medium'>
            <span>{t('Site')}</span>
            <span>{t('类型 / 分组与倍率')}</span>
            <span>{t('Applicant')}</span>
            <span>{t('Time')}</span>
            <span>{t('Status')}</span>
            <span className='w-32 text-right'>{t('Actions')}</span>
          </div>
          <div className='divide-y'>
          {items.map((a) => {
            const isGroupChangeApp = a.app_type === 'group_change'
            const isProfileChangeApp = a.app_type === 'profile_change'
            const isQualificationApp =
              !isGroupChangeApp && !isProfileChangeApp && !a.site_url
            const tenureOpt = OPERATION_TENURE_OPTIONS.find(
              (o) => o.value === a.operation_tenure
            )
            let appTypeLabel = t('新渠道/分组')
            if (isProfileChangeApp) {
              appTypeLabel = t('改名申请')
            } else if (isGroupChangeApp) {
              appTypeLabel = t('分组变更')
            } else if (isQualificationApp) {
              appTypeLabel = t('资质入驻')
            }
            return (
              <div
                key={a.id}
                className='hover:bg-muted/35 grid grid-cols-[1fr_1.1fr_1fr_1fr_auto_auto] items-center gap-2 px-4 py-2.5 text-sm transition-colors'
              >
                <div className='min-w-0'>
                  <span className='block truncate font-medium'>
                    {a.shop_name || a.site_name}
                  </span>
                  {a.platform_url && (
                    <a
                      href={a.platform_url}
                      target='_blank'
                      rel='noreferrer'
                      className='text-muted-foreground hover:text-foreground inline-flex max-w-full items-center gap-1 truncate text-xs hover:underline'
                    >
                      <ExternalLink className='size-3 shrink-0' />
                      <span className='truncate'>{a.platform_url}</span>
                    </a>
                  )}
                  {!a.platform_url && a.site_url && (
                    <a
                      href={a.site_url}
                      target='_blank'
                      rel='noreferrer'
                      className='text-muted-foreground block truncate text-xs hover:underline'
                    >
                      {a.site_url}
                    </a>
                  )}
                </div>
                <div className='flex flex-wrap items-center gap-1 text-xs'>
                  <Badge variant='outline'>{appTypeLabel}</Badge>
                  {isProfileChangeApp && a.requested_shop_name && (
                    <span className='font-medium text-amber-600 dark:text-amber-400'>
                      → {a.requested_shop_name}
                    </span>
                  )}
                  {tenureOpt && (
                    <Badge variant='secondary'>{t(tenureOpt.labelKey)}</Badge>
                  )}
                  {a.group_name && (
                    <span className='font-mono font-medium'>{a.group_name}</span>
                  )}
                  {!isProfileChangeApp && !isQualificationApp && (
                    <Badge variant='secondary'>
                      {`×${(a.rate_multiplier || 1).toFixed(2)}`}
                    </Badge>
                  )}
                  {a.join_auto_group && (
                    <Badge variant='secondary'>Auto</Badge>
                  )}
                  {!isGroupChangeApp &&
                    !isProfileChangeApp &&
                    !isQualificationApp && (
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
                      {!isGroupChangeApp &&
                        !isProfileChangeApp &&
                        Boolean(a.site_url) && (
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
            )
          })}
            {!items.length && (
              <div className='text-muted-foreground flex flex-col items-center gap-2 px-3 py-10 text-center text-sm'>
                <ClipboardCheck className='text-muted-foreground/50 size-7' />
                <span>{t('No applications yet')}</span>
              </div>
            )}
          </div>
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
        <DialogContent className='max-h-[90vh] max-w-lg overflow-y-auto'>
          <DialogHeader>
            <DialogTitle>
              {t('Review')}: {reviewing?.shop_name || reviewing?.site_name}
            </DialogTitle>
          </DialogHeader>
          {(() => {
            const isGroupChange = reviewing?.app_type === 'group_change'
            const isProfileChange = reviewing?.app_type === 'profile_change'
            const isQualificationOnly =
              !isGroupChange && !isProfileChange && !reviewing?.site_url
            const tenureOpt = OPERATION_TENURE_OPTIONS.find(
              (o) => o.value === reviewing?.operation_tenure
            )
            let reviewSummaryTitle = t('新渠道与分组入驻申请')
            if (isProfileChange) {
              reviewSummaryTitle = t('商户名称变更申请')
            } else if (isGroupChange) {
              reviewSummaryTitle = t('专属分组倍率 / Auto 路由变更申请')
            } else if (isQualificationOnly) {
              reviewSummaryTitle = t('纯资质入驻审核（过审后由商家配置渠道）')
            }
            return (
              <div className='space-y-3'>
                <div className='rounded-md border p-2.5 text-xs'>
                  <p className='font-medium'>{reviewSummaryTitle}</p>
                  {isProfileChange && (
                    <p className='text-muted-foreground mt-1.5'>
                      {t('原商户名称')}:{' '}
                      <strong className='text-foreground'>
                        {reviewing?.shop_name || reviewing?.site_name}
                      </strong>{' '}
                      → {t('申请新名称')}:{' '}
                      <strong className='text-amber-600 dark:text-amber-400'>
                        {reviewing?.requested_shop_name}
                      </strong>
                    </p>
                  )}
                  {!isProfileChange && !isQualificationOnly && (
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
                  )}

                  {(reviewing?.platform_url ||
                    reviewing?.operation_tenure ||
                    reviewing?.contact_name ||
                    reviewing?.contact_info ||
                    reviewing?.payout_method ||
                    reviewing?.payout_account) && (
                    <div className='text-muted-foreground mt-2 space-y-1 border-t pt-2'>
                      {reviewing?.platform_url && (
                        <p>
                          {t('Platform Website URL')}:{' '}
                          <a
                            href={reviewing.platform_url}
                            target='_blank'
                            rel='noreferrer'
                            className='text-foreground hover:underline'
                          >
                            {reviewing.platform_url}
                          </a>
                          {tenureOpt
                            ? ` · ${t('Tenure')}: ${t(tenureOpt.labelKey)}`
                            : ''}
                        </p>
                      )}
                      <p>
                        {t('Contact')}: {reviewing?.contact_name || '--'} (
                        {reviewing?.contact_info || '--'})
                      </p>
                      {reviewing?.description && (
                        <p>
                          {t('Notes')}: {reviewing.description}
                        </p>
                      )}
                      <p>
                        {t('Payout')}:{' '}
                        {reviewing?.payout_method
                          ? t(getPayoutMethodLabel(reviewing.payout_method))
                          : '--'}{' '}
                        · {reviewing?.payout_account || '--'}
                        {reviewing?.payout_holder
                          ? ` (${reviewing.payout_holder})`
                          : ''}
                      </p>
                    </div>
                  )}

                  {isValidPayoutQrDataUrl(reviewing?.payout_qrcode) && (
                    <div className='mt-2 flex items-center gap-3 border-t pt-2'>
                      <div className='rounded border bg-white p-1'>
                        <img
                          src={reviewing?.payout_qrcode}
                          alt={t('Payout QR Code')}
                          className='size-24 object-contain'
                        />
                      </div>
                      <span className='text-muted-foreground text-xs'>
                        {t('Attached payout QR code')}
                      </span>
                    </div>
                  )}
                </div>

                {!isGroupChange && !isProfileChange && isQualificationOnly && (
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
                )}

                {!isGroupChange && !isProfileChange && !isQualificationOnly && (
                  <>
                    <div className='rounded-md border p-2 text-xs'>
                      <p className='mb-1 font-medium'>{t('Requested models')}</p>
                      {quotes.map((q) => (
                        <p key={q.model} className='font-mono'>
                          {q.model}
                          {q.upstream && q.upstream !== q.model
                            ? ` ← ${q.upstream}`
                            : ''}
                          {typeof q.input_price === 'number' &&
                          typeof q.output_price === 'number' &&
                          q.input_price > 0 &&
                          q.output_price > 0
                            ? ` (基准价: $${q.input_price} / $${q.output_price} per 1M)`
                            : ''}
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

                {!isProfileChange && !isQualificationOnly && (
                  <>
                    <div className='grid gap-3 sm:grid-cols-2'>
                      <div className='space-y-1.5'>
                        <Label htmlFor='merchant-review-group-name'>
                          {t('专属分组名称')}
                        </Label>
                        <Input
                          id='merchant-review-group-name'
                          value={groupName}
                          readOnly={isGroupChange}
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
                      <Label htmlFor='merchant-review-group-desc'>
                        {t('分组说明')}
                      </Label>
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
                  </>
                )}

                <div className='space-y-1.5'>
                  <Label htmlFor='merchant-review-note'>{t('Review Note')}</Label>
                  <Input
                    id='merchant-review-note'
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                  />
                </div>
                <div className='flex gap-2'>
                  <Button onClick={() => review('approve')}>
                    {t('Approve')}
                  </Button>
                  <Button
                    variant='destructive'
                    onClick={() => review('reject')}
                  >
                    {t('Reject')}
                  </Button>
                </div>
              </div>
            )
          })()}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function MerchantsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<MerchantRow | null>(null)
  const [profitRatio, setProfitRatio] = useState('0.95')
  const [settleHoldDays, setSettleHoldDays] = useState('0')
  const [minWithdraw, setMinWithdraw] = useState('0')
  const [contactName, setContactName] = useState('')
  const [contactInfo, setContactInfo] = useState('')
  const [adminNote, setAdminNote] = useState('')
  const [saving, setSaving] = useState(false)

  const merchantsQuery = useQuery({
    queryKey: ['merchant-admin', 'merchants', page],
    queryFn: () => getMerchants(page, PAGE_SIZE),
  })
  const items = merchantsQuery.data?.items ?? []

  function openEdit(m: MerchantRow) {
    setProfitRatio(String(m.profit_ratio ?? 0.95))
    setSettleHoldDays(String(m.settle_hold_days ?? 0))
    setMinWithdraw(String(m.min_withdraw ?? 0))
    setContactName(m.contact_name ?? '')
    setContactInfo(m.contact_info ?? '')
    setAdminNote(m.admin_note ?? '')
    setEditing(m)
  }

  async function saveEdit() {
    if (!editing) return
    setSaving(true)
    try {
      await updateMerchant(editing.id, {
        profit_ratio: Number(profitRatio),
        settle_hold_days: Number(settleHoldDays) || 0,
        min_withdraw: Number(minWithdraw) || 0,
        contact_name: contactName.trim(),
        contact_info: contactInfo.trim(),
        admin_note: adminNote.trim(),
      })
      toast.success(t('Merchant updated'))
      setEditing(null)
      queryClient.invalidateQueries({
        queryKey: ['merchant-admin', 'merchants'],
      })
    } catch (error) {
      handleServerError(error, t('Failed to update merchant'))
    } finally {
      setSaving(false)
    }
  }

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
    <div className='space-y-3'>
      <div className='overflow-x-auto rounded-xl border'>
        <div className='min-w-[700px]'>
          <div className='bg-muted/40 text-muted-foreground grid grid-cols-[1.2fr_1fr_1.3fr_1.1fr_auto] gap-2 border-b px-4 py-2.5 text-xs font-medium'>
            <span>{t('Site / Code')}</span>
            <span>{t('Contact / Username')}</span>
            <span>{t('Wallet (Avail / T+N / Frozen)')}</span>
            <span>{t('Terms')}</span>
            <span className='w-36 text-right'>{t('Actions')}</span>
          </div>
          <div className='divide-y'>
            {items.map((m) => (
              <div
                key={m.id}
                className='hover:bg-muted/35 grid grid-cols-[1.2fr_1fr_1.3fr_1.1fr_auto] items-center gap-2 px-4 py-2.5 text-sm transition-colors'
              >
              <div className='min-w-0'>
                <a
                  href={m.site_url}
                  target='_blank'
                  rel='noreferrer'
                  className='block truncate hover:underline'
                >
                  {m.site_name}
                </a>
                <div className='flex flex-wrap items-center gap-1 text-xs'>
                  {m.merchant_code && (
                    <Badge variant='outline' className='font-mono'>
                      {m.merchant_code}
                    </Badge>
                  )}
                  {m.status !== 1 && (
                    <Badge variant='destructive'>{t('Suspended')}</Badge>
                  )}
                </div>
              </div>
              <div className='min-w-0 text-xs'>
                <p className='truncate font-medium'>
                  {m.username || `#${m.user_id}`}
                </p>
                {(m.contact_name || m.contact_info) && (
                  <p className='text-muted-foreground truncate'>
                    {m.contact_name} {m.contact_info}
                  </p>
                )}
              </div>
              <div className='font-mono text-xs'>
                <p>
                  {formatCurrencyFromUSD(m.wallet?.balance, {
                    digitsLarge: 4,
                    digitsSmall: 4,
                  })}
                </p>
                {((m.wallet?.pending_balance ?? 0) > 0 ||
                  (m.wallet?.frozen_balance ?? 0) > 0) && (
                  <p className='text-muted-foreground'>
                    {t('Pending')}:{' '}
                    {formatCurrencyFromUSD(m.wallet?.pending_balance, {
                      digitsLarge: 2,
                      digitsSmall: 2,
                    })}{' '}
                    / {t('Frozen')}:{' '}
                    {formatCurrencyFromUSD(m.wallet?.frozen_balance, {
                      digitsLarge: 2,
                      digitsSmall: 2,
                    })}
                  </p>
                )}
              </div>
              <div className='text-xs'>
                <p>
                  {(m.profit_ratio * 100).toFixed(0)}% · T+
                  {m.settle_hold_days ?? 0}
                </p>
                {(m.min_withdraw ?? 0) > 0 && (
                  <p className='text-muted-foreground'>
                    {t('Min')}: ${Number(m.min_withdraw).toFixed(2)}
                  </p>
                )}
              </div>
              <span className='flex w-36 justify-end gap-1'>
                <Button
                  size='sm'
                  variant='outline'
                  className='h-7'
                  onClick={() => openEdit(m)}
                >
                  {t('Edit')}
                </Button>
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
              <div className='text-muted-foreground flex flex-col items-center gap-2 px-3 py-10 text-center text-sm'>
                <Users className='text-muted-foreground/50 size-7' />
                <span>{t('No merchants yet')}</span>
              </div>
            )}
          </div>
        </div>
        {(merchantsQuery.data?.total ?? 0) > page * PAGE_SIZE && (
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

      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Edit Merchant')}: {editing?.site_name}{' '}
              {editing?.merchant_code ? `(${editing.merchant_code})` : ''}
            </DialogTitle>
          </DialogHeader>
          <div className='space-y-3'>
            <div className='grid grid-cols-3 gap-3'>
              <div className='space-y-1.5'>
                <Label htmlFor='admin-merchant-profit'>
                  {t('Profit ratio (0~1)')}
                </Label>
                <Input
                  id='admin-merchant-profit'
                  type='number'
                  step='0.01'
                  min='0'
                  max='1'
                  value={profitRatio}
                  onChange={(e) => setProfitRatio(e.target.value)}
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='admin-merchant-hold'>
                  {t('Settle Hold (Days)')}
                </Label>
                <Input
                  id='admin-merchant-hold'
                  type='number'
                  min='0'
                  max='90'
                  value={settleHoldDays}
                  onChange={(e) => setSettleHoldDays(e.target.value)}
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='admin-merchant-min-wd'>
                  {t('Min Withdraw (USD)')}
                </Label>
                <Input
                  id='admin-merchant-min-wd'
                  type='number'
                  min='0'
                  step='1'
                  value={minWithdraw}
                  onChange={(e) => setMinWithdraw(e.target.value)}
                />
              </div>
            </div>
            <div className='grid grid-cols-2 gap-3'>
              <div className='space-y-1.5'>
                <Label htmlFor='admin-merchant-contact-name'>
                  {t('Contact Person')}
                </Label>
                <Input
                  id='admin-merchant-contact-name'
                  value={contactName}
                  onChange={(e) => setContactName(e.target.value)}
                />
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='admin-merchant-contact-info'>
                  {t('Contact Info')}
                </Label>
                <Input
                  id='admin-merchant-contact-info'
                  value={contactInfo}
                  onChange={(e) => setContactInfo(e.target.value)}
                />
              </div>
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='admin-merchant-note'>{t('Admin Note')}</Label>
              <Input
                id='admin-merchant-note'
                value={adminNote}
                onChange={(e) => setAdminNote(e.target.value)}
              />
            </div>
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setEditing(null)}>
                {t('Cancel')}
              </Button>
              <Button disabled={saving} onClick={saveEdit}>
                {saving ? t('Saving...') : t('Save')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function AdminWithdrawalsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [statusFilter, setStatusFilter] = useState<number>(-1)
  const [proof, setProof] = useState('')
  const [qrModal, setQrModal] = useState<MerchantWithdrawal | null>(null)

  const withdrawalsQuery = useQuery({
    queryKey: ['merchant-admin', 'withdrawals', page, statusFilter],
    queryFn: () => getAdminWithdrawals(page, PAGE_SIZE, statusFilter),
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
    <div className='space-y-3'>
      <Card className='bg-muted/20'>
        <CardContent className='flex flex-wrap items-center justify-between gap-3 py-3'>
          <div className='bg-background inline-flex items-center rounded-lg border p-0.5 text-xs'>
            {[
              { value: -1, label: t('All') },
              { value: 0, label: t('Pending') },
              { value: 1, label: t('Approved') },
              { value: 2, label: t('Paid') },
              { value: 3, label: t('Rejected') },
            ].map((opt) => (
              <button
                key={opt.value}
                type='button'
                onClick={() => {
                  setStatusFilter(opt.value)
                  setPage(1)
                }}
                className={cn(
                  'rounded-md px-2.5 py-1 transition-colors',
                  statusFilter === opt.value
                    ? 'bg-primary text-primary-foreground font-medium'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {opt.label}
              </button>
            ))}
          </div>
          <div className='flex flex-1 flex-wrap items-center justify-end gap-2'>
            <Label
              htmlFor='merchant-withdrawal-proof'
              className='text-xs font-medium whitespace-nowrap'
            >
              {t('Payment proof (for mark-as-paid)')}
            </Label>
            <Input
              id='merchant-withdrawal-proof'
              value={proof}
              onChange={(e) => setProof(e.target.value)}
              placeholder={t('Transaction ID / screenshot link')}
              className='max-w-sm bg-background'
            />
          </div>
        </CardContent>
      </Card>
      <div className='overflow-x-auto rounded-xl border'>
        <div className='min-w-[680px]'>
          <div className='bg-muted/40 text-muted-foreground grid grid-cols-[1fr_1.2fr_0.9fr_0.8fr_auto] gap-2 border-b px-4 py-2.5 text-xs font-medium'>
            <span>{t('Time')}</span>
            <span>{t('Shop / Account')}</span>
            <span>{t('Amount (USD)')}</span>
            <span>{t('Status')}</span>
            <span className='w-52 text-right'>{t('Actions')}</span>
          </div>
          <div className='divide-y'>
            {items.map((w: MerchantWithdrawal) => (
              <div
                key={w.id}
                className='hover:bg-muted/35 grid grid-cols-[1fr_1.2fr_0.9fr_0.8fr_auto] items-center gap-2 px-4 py-2.5 text-sm transition-colors'
              >
            <span className='text-muted-foreground'>
              {formatTimestampToDate(w.created_at)}
              {w.trade_no && (
                <span className='block font-mono text-xs'>{w.trade_no}</span>
              )}
            </span>
            <div className='min-w-0'>
              <span className='block truncate font-medium'>
                {w.shop_name || `#${w.merchant_user_id}`}
                {w.payout_method && (
                  <Badge variant='outline' className='ml-1 text-xs'>
                    {t(getPayoutMethodLabel(w.payout_method))}
                  </Badge>
                )}
              </span>
              <span className='text-muted-foreground block truncate text-xs'>
                {w.account_info || '--'}
              </span>
            </div>
            <span className='font-mono'>
              {formatCurrencyFromUSD(w.amount, {
                digitsLarge: 2,
                digitsSmall: 2,
              })}
            </span>
            <span>{statusBadge(w.status)}</span>
            <span className='flex w-52 justify-end gap-1'>
              {isValidPayoutQrDataUrl(w.payout_qrcode) && (
                <Button
                  size='sm'
                  variant='outline'
                  className='h-7 px-2'
                  onClick={() => setQrModal(w)}
                >
                  <QrCode className='mr-1 size-3.5' />
                  {t('QR')}
                </Button>
              )}
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
              <div className='text-muted-foreground flex flex-col items-center gap-2 px-3 py-10 text-center text-sm'>
                <ReceiptText className='text-muted-foreground/50 size-7' />
                <span>{t('No withdrawals yet')}</span>
              </div>
            )}
          </div>
        </div>
        {(withdrawalsQuery.data?.total ?? 0) > page * PAGE_SIZE && (
          <div className='border-t p-2 text-center'>
            <Button variant='outline' size='sm' onClick={() => setPage(page + 1)}>
              {t('Load more')}
            </Button>
          </div>
        )}
      </div>

      <Dialog open={Boolean(qrModal)} onOpenChange={(o) => !o && setQrModal(null)}>
        <DialogContent className='max-w-sm'>
          <DialogHeader>
            <DialogTitle>
              {t('Scan QR to Pay')} ·{' '}
              {qrModal?.shop_name || `#${qrModal?.merchant_user_id}`}
            </DialogTitle>
          </DialogHeader>
          {qrModal && (
            <div className='space-y-3'>
              <div className='bg-muted/20 flex items-center justify-between rounded-lg border px-3 py-2 text-xs'>
                <span>{qrModal.account_info || '--'}</span>
                <span className='font-mono text-sm font-bold'>
                  {formatCurrencyFromUSD(qrModal.amount, {
                    digitsLarge: 2,
                    digitsSmall: 2,
                  })}
                </span>
              </div>
              {isValidPayoutQrDataUrl(qrModal.payout_qrcode) && (
                <div className='flex justify-center rounded-xl border bg-white p-3'>
                  <img
                    src={qrModal.payout_qrcode}
                    alt={t('Payout QR Code')}
                    className='max-h-64 max-w-64 object-contain'
                  />
                </div>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>
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
      <div className='overflow-x-auto rounded-xl border'>
        <div className='bg-muted/20 flex items-center justify-between gap-2 border-b px-4 py-2.5'>
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
        <div className='min-w-[700px]'>
          <div className='bg-muted/40 text-muted-foreground grid grid-cols-[1fr_1fr_auto_1fr_1fr_auto_auto] gap-2 border-b px-4 py-2.5 text-xs font-medium'>
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
                className='hover:bg-muted/35 grid grid-cols-[1fr_1fr_auto_1fr_1fr_auto_auto] items-center gap-2 px-4 py-2.5 text-sm transition-colors'
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
              <div className='text-muted-foreground flex flex-col items-center gap-2 px-3 py-10 text-center text-sm'>
                <FileSpreadsheet className='text-muted-foreground/50 size-7' />
                <span>{t('No quotes yet')}</span>
              </div>
            )}
          </div>
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
