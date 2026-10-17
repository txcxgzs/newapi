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
import { ChevronRight, RefreshCw } from 'lucide-react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Bar,
  BarChart,
  CartesianGrid,
  XAxis,
  YAxis,
} from 'recharts'

import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { formatCurrencyFromUSD, formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import {
  createMerchantWithdrawal,
  getMerchantEarnings,
  getMerchantLogs,
  getMerchantPools,
  getMerchantQuotes,
  getMerchantSummary,
  getMerchantWithdrawals,
  getMyMerchantApplication,
  syncMerchantQuotes,
  updateMerchantRate,
} from './api'
import { MerchantApplyForm } from './apply-form'
import { ChannelEditDialog } from './components/channel-edit-dialog'
import type {
  MerchantPoolChannel,
  MerchantPoolGroup,
} from './types'

/** 门户金额按系统货币展示，保留 4 位小数。 */
function formatUsd(v: number | undefined): string {
  return formatCurrencyFromUSD(v, { digitsLarge: 4, digitsSmall: 4 })
}

/** 倍率展示为 `0.1000x` 形式；缺失或非法值回退占位符。 */
function formatRate(v: number | undefined): string {
  return typeof v === 'number' && Number.isFinite(v)
    ? `${v.toFixed(4)}x`
    : '--'
}

/** 成功率（0..1）转百分比；无调用数据时展示占位符，避免出现 NaN。 */
function formatSuccessRate(v: number | undefined, hasCalls: boolean): string {
  if (!hasCalls) return '--'
  return typeof v === 'number' && Number.isFinite(v)
    ? `${(v * 100).toFixed(2)}%`
    : '--'
}

/** 平均耗时展示为秒；无数据时占位符。 */
function formatLatency(v: number | undefined, hasCalls: boolean): string {
  if (!hasCalls) return '--'
  return typeof v === 'number' && Number.isFinite(v) ? `${v.toFixed(2)}s` : '--'
}

export function Merchant() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const selfQuery = useQuery({
    queryKey: ['merchant', 'self'],
    queryFn: async () => {
      const res = await api.get('/api/user/self')
      return requireServerSuccess(res.data).data as { role?: number }
    },
    staleTime: 60000,
  })
  const applicationQuery = useQuery({
    queryKey: ['merchant', 'application'],
    queryFn: getMyMerchantApplication,
  })

  const role = selfQuery.data?.role ?? 1
  const isMerchant = role >= ROLE.MERCHANT
  const status = applicationQuery.data?.status

  if (selfQuery.isLoading || applicationQuery.isLoading) {
    return (
      <div className='text-muted-foreground flex min-h-[60vh] items-center justify-center'>
        {t('Loading...')}
      </div>
    )
  }

  if (!isMerchant) {
    // 未入驻：没有申请记录或被拒后可再次申请
    if (status === undefined || status === 2) {
      return (
        <div className='mx-auto w-full max-w-3xl px-4 py-8'>
          <h1 className='mb-4 text-2xl font-bold'>{t('Become a Merchant')}</h1>
          <p className='text-muted-foreground mb-6'>
            {t(
              'Submit your site, API key and model quotes. After approval, your channel joins the pools and starts competing.'
            )}
          </p>
          {status === 2 && applicationQuery.data?.review_note && (
            <p className='text-destructive mb-4 text-sm'>
              {t('Review Note')}: {applicationQuery.data.review_note}
            </p>
          )}
          <MerchantApplyForm
            onSubmitted={() => {
              queryClient.invalidateQueries({
                queryKey: ['merchant', 'application'],
              })
            }}
          />
        </div>
      )
    }
    const app = applicationQuery.data
    return (
      <div className='mx-auto w-full max-w-3xl px-4 py-8'>
        <h1 className='mb-4 text-2xl font-bold'>{t('Merchant Application')}</h1>
        <Card>
          <CardContent className='space-y-2 py-6'>
            <div className='flex items-center gap-2'>
              {app?.status === 0 && (
                <Badge variant='secondary'>{t('Pending Review')}</Badge>
              )}
              {app?.status === 1 && (
                <Badge variant='default'>{t('Approved')}</Badge>
              )}
              {app?.status === 2 && (
                <Badge variant='destructive'>{t('Rejected')}</Badge>
              )}
              <span>{app?.site_name}</span>
            </div>
            {app?.review_note && (
              <p className='text-muted-foreground text-sm'>
                {t('Review Note')}: {app.review_note}
              </p>
            )}
            {app?.status === 1 && (
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Your channel has been created. You now have access to the merchant portal.'
                )}
              </p>
            )}
          </CardContent>
        </Card>
      </div>
    )
  }

  return <MerchantPortal />
}

function MerchantPortal() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  const summaryQuery = useQuery({
    queryKey: ['merchant', 'summary'],
    queryFn: getMerchantSummary,
    refetchInterval: 30000,
  })

  const traffic = summaryQuery.data?.traffic_24h
  const wallet = summaryQuery.data?.wallet
  const groups = summaryQuery.data?.groups_24h ?? []
  const totalGroupCalls = groups.reduce((sum, g) => sum + g.calls, 0)

  return (
    <div className='mx-auto w-full max-w-6xl px-4 py-6'>
      <h1 className='mb-1 text-2xl font-bold'>{t('Merchant Portal')}</h1>
      <p className='text-muted-foreground mb-4 text-sm'>
        {summaryQuery.data?.site_name} · {summaryQuery.data?.site_url}
      </p>
      <div className='mb-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <Card>
          <CardContent className='py-4'>
            <p className='text-muted-foreground text-xs'>{t('24h Calls')}</p>
            <p className='text-2xl font-bold'>
              {formatNumber(traffic?.calls ?? 0, locale)}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-4'>
            <p className='text-muted-foreground text-xs'>{t('24h Revenue')}</p>
            <p className='text-2xl font-bold'>
              {formatUsd(traffic?.quota_usd)}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-4'>
            <p className='text-muted-foreground text-xs'>
              {t('24h Success Rate')}
            </p>
            <p className='text-2xl font-bold'>
              {(traffic?.success_rate ?? 0).toFixed(2)}%
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-4'>
            <p className='text-muted-foreground text-xs'>
              {t('Wallet Balance')}
            </p>
            <p className='text-2xl font-bold'>{formatUsd(wallet?.balance)}</p>
            <p className='text-muted-foreground text-xs'>
              {t('Profit ratio')}:{' '}
              {((summaryQuery.data?.profit_ratio ?? 0) * 100).toFixed(0)}%
            </p>
          </CardContent>
        </Card>
      </div>

      {!!groups.length && (
        <div className='mb-6 rounded-xl border'>
          <div className='text-muted-foreground grid grid-cols-[1.3fr_1fr_1fr_1fr_0.8fr] gap-2 border-b px-3 py-2 text-xs'>
            <span>{t('Pool')}</span>
            <span>{t('24h Calls')}</span>
            <span>{t('24h Revenue')}</span>
            <span>{t('24h Success Rate')}</span>
            <span className='text-right'>{t('Share')}</span>
          </div>
          <div className='divide-y'>
            {groups.map((g) => (
              <div
                key={g.group || '__default__'}
                className='grid grid-cols-[1.3fr_1fr_1fr_1fr_0.8fr] items-center gap-2 px-3 py-2 text-sm'
              >
                <span className='truncate font-mono'>
                  {g.group || t('Default')}
                </span>
                <span>{formatNumber(g.calls, locale)}</span>
                <span>{formatUsd(g.quota_usd)}</span>
                <span>{g.success_rate.toFixed(2)}%</span>
                <span className='text-muted-foreground text-right'>
                  {totalGroupCalls > 0
                    ? `${((g.calls / totalGroupCalls) * 100).toFixed(1)}%`
                    : '--'}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      <Tabs defaultValue='pools'>
        <TabsList>
          <TabsTrigger value='pools'>{t('Pool Market')}</TabsTrigger>
          <TabsTrigger value='quotes'>{t('Quote Board')}</TabsTrigger>
          <TabsTrigger value='usage'>{t('Usage Records')}</TabsTrigger>
          <TabsTrigger value='earnings'>{t('Earnings Ledger')}</TabsTrigger>
          <TabsTrigger value='withdrawals'>{t('Withdrawals')}</TabsTrigger>
        </TabsList>
        <TabsContent value='pools'>
          <MerchantPoolsTab
            onChanged={() =>
              queryClient.invalidateQueries({ queryKey: ['merchant'] })
            }
          />
        </TabsContent>
        <TabsContent value='quotes'>
          <MerchantQuotesTab />
        </TabsContent>
        <TabsContent value='usage'>
          <MerchantLogsTab
            groups={groups.map((g) => g.group).filter(Boolean)}
          />
        </TabsContent>
        <TabsContent value='earnings'>
          <MerchantEarningsTab />
        </TabsContent>
        <TabsContent value='withdrawals'>
          <MerchantWithdrawalsTab
            balance={wallet?.balance ?? 0}
            onChanged={() =>
              queryClient.invalidateQueries({ queryKey: ['merchant'] })
            }
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}

/**
 * 分组市场：三层结构的入口。
 *
 * 第一层用卡片网格展示平台全部分组（含尚未接入的）；点击进入第二层查看该分组
 * 下自己的池子（渠道），第二层顶部可新增渠道。倍率调整在池子卡片内完成。
 */
function MerchantPoolsTab({ onChanged }: { onChanged: () => void }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const poolsQuery = useQuery({
    queryKey: ['merchant', 'pools'],
    queryFn: getMerchantPools,
  })
  // 第二层选中的分组；null 表示停留在第一层分组市场
  const [selectedPool, setSelectedPool] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)

  const allPools = poolsQuery.data?.all_pools ?? []
  const groups = poolsQuery.data?.groups ?? []
  const groupByPool = new Map(groups.map((group) => [group.pool, group]))

  /** 扩容申请提交成功后关闭表单并刷新门户数据。 */
  function handleAdded() {
    setAdding(false)
    onChanged()
  }

  if (selectedPool) {
    return (
      <PoolGroupDetail
        pool={selectedPool}
        group={groupByPool.get(selectedPool)}
        locale={locale}
        onBack={() => setSelectedPool(null)}
        onAddChannel={() => setAdding(true)}
        adding={adding}
        onCloseAdd={() => setAdding(false)}
        onAdded={handleAdded}
        onChanged={onChanged}
      />
    )
  }

  if (poolsQuery.isLoading) {
    return (
      <div className='text-muted-foreground py-12 text-center'>
        {t('Loading...')}
      </div>
    )
  }

  return (
    <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
      {allPools.map((pool) => {
        const group = groupByPool.get(pool)
        const joined = group?.channels.length ?? 0
        const hasCalls = (group?.calls_24h ?? 0) > 0
        return (
          // 分组卡片是进入第二层的唯一入口：用真实按钮语义 + 明显的视觉暗示
          // （描边高亮、chevron、底部「点击查看」提示），并保持键盘可达。
          <Card
            key={pool}
            role='button'
            tabIndex={0}
            aria-label={t('View my channels in {{pool}}', { pool })}
            className={cn(
              'group cursor-pointer outline-none transition-all',
              'hover:border-primary/60 hover:shadow-md focus-visible:ring-2 focus-visible:ring-ring',
              joined > 0 ? 'border-primary/40' : 'border-border'
            )}
            onClick={() => setSelectedPool(pool)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                setSelectedPool(pool)
              }
            }}
          >
            <CardContent className='space-y-3 py-4'>
              <div className='flex items-center justify-between gap-2'>
                <span className='truncate font-mono font-medium'>{pool}</span>
                <div className='flex shrink-0 items-center gap-1.5'>
                  <Badge variant={joined > 0 ? 'default' : 'secondary'}>
                    {t('{{count}} channels', { count: joined })}
                  </Badge>
                  <ChevronRight
                    aria-hidden
                    className='text-muted-foreground size-4 transition-all group-hover:translate-x-0.5 group-hover:text-primary'
                  />
                </div>
              </div>
              <div className='text-muted-foreground grid grid-cols-3 gap-2 text-xs'>
                <div>
                  <p>{t('24h Calls')}</p>
                  <p className='text-foreground font-mono'>
                    {formatNumber(group?.calls_24h ?? 0, locale)}
                  </p>
                </div>
                <div>
                  <p>{t('24h Revenue')}</p>
                  <p className='text-foreground font-mono'>
                    {formatUsd(group?.quota_usd_24h ?? 0)}
                  </p>
                </div>
                <div>
                  <p>{t('24h Success Rate')}</p>
                  <p className='text-foreground font-mono'>
                    {formatSuccessRate(group?.success_rate, hasCalls)}
                  </p>
                </div>
              </div>
              <div className='text-primary/70 group-hover:text-primary flex items-center gap-1 border-t pt-2 text-xs font-medium'>
                {joined > 0
                  ? t('Click to view my channels')
                  : t('Click to join this pool')}
              </div>
            </CardContent>
          </Card>
        )
      })}
      {!allPools.length && (
        <div className='text-muted-foreground col-span-full rounded-xl border px-3 py-8 text-center'>
          {t('No pools available yet')}
        </div>
      )}
    </div>
  )
}

/** 第二层：某个分组下商家的池子列表 + 新增渠道入口。 */
function PoolGroupDetail({
  pool,
  group,
  locale,
  onBack,
  onAddChannel,
  adding,
  onCloseAdd,
  onAdded,
  onChanged,
}: {
  pool: string
  group: MerchantPoolGroup | undefined
  locale: string | undefined
  onBack: () => void
  onAddChannel: () => void
  adding: boolean
  onCloseAdd: () => void
  onAdded: () => void
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const channels = group?.channels ?? []

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <Button variant='outline' size='sm' onClick={onBack}>
            {t('Back')}
          </Button>
          <span className='font-mono text-lg font-medium'>{pool}</span>
          <Badge variant='secondary'>
            {t('{{count}} channels', { count: channels.length })}
          </Badge>
        </div>
        <Button size='sm' onClick={onAddChannel}>
          {t('Add Channel')}
        </Button>
      </div>

      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
        {channels.map((channel) => (
          <ChannelCard
            key={channel.channel_id}
            channel={channel}
            locale={locale}
            onChanged={onChanged}
          />
        ))}
        {!channels.length && (
          <div className='text-muted-foreground col-span-full rounded-xl border px-3 py-8 text-center'>
            {t('No channels in this pool yet')}
          </div>
        )}
      </div>

      <Dialog open={adding} onOpenChange={(open) => !open && onCloseAdd()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Add Channel')}</DialogTitle>
            <DialogDescription>
              {t(
                'Adding a channel submits an expansion application; it goes live after admin approval.'
              )}
            </DialogDescription>
          </DialogHeader>
          {/* 复用入驻表单：后端把商家的再次提交当作扩容申请走审核 */}
          <MerchantApplyForm onSubmitted={onAdded} />
        </DialogContent>
      </Dialog>
    </div>
  )
}

/** 第三层：单个渠道卡片，展示倍率与近 24h 调用数据。 */
function ChannelCard({
  channel,
  locale,
  onChanged,
}: {
  channel: MerchantPoolChannel
  locale: string | undefined
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [editChannel, setEditChannel] = useState<MerchantPoolChannel | null>(null)
  const [syncing, setSyncing] = useState(false)
  const hasCalls = channel.calls_24h > 0

  /** 触发一次自己渠道的报价同步：改完上游价格后立即可见，不必等定时任务。 */
  async function syncNow() {
    setSyncing(true)
    try {
      const summary = await syncMerchantQuotes([channel.channel_id])
      if (summary.synced_channels > 0) {
        toast.success(
          t('Synced {{count}} quotes', { count: summary.written_quotes })
        )
      } else if (summary.skipped_channels > 0) {
        toast.warning(t('Upstream has no price API to sync from'))
      } else {
        toast.error(t('Sync failed, check the upstream availability'))
      }
      queryClient.invalidateQueries({ queryKey: ['merchant', 'pools'] })
      onChanged()
    } catch (error) {
      handleServerError(error, t('Failed to sync quotes'))
    } finally {
      setSyncing(false)
    }
  }

  return (
    <Card>
      <CardContent className='space-y-3 py-4'>
        <div className='flex items-center justify-between gap-2'>
          <span className='truncate font-medium'>
            {channel.name || `#${channel.channel_id}`}
          </span>
          <Badge variant={channel.status === 1 ? 'default' : 'destructive'}>
            {channel.status === 1 ? t('Enabled') : t('Disabled')}
          </Badge>
        </div>
        <div className='text-muted-foreground grid grid-cols-2 gap-x-3 gap-y-2 text-xs'>
          <div>
            <p>{t('Rate Multiplier')}</p>
            <p className='text-foreground font-mono'>
              {formatRate(channel.rate_multiplier)}
            </p>
          </div>
          <div>
            <p>{t('Avg Latency')}</p>
            <p className='text-foreground font-mono'>
              {formatLatency(channel.avg_use_time, hasCalls)}
            </p>
          </div>
          <div>
            <p>{t('24h Success Rate')}</p>
            <p className='text-foreground font-mono'>
              {formatSuccessRate(channel.success_rate, hasCalls)}
            </p>
          </div>
          <div>
            <p>{t('24h Calls')}</p>
            <p className='text-foreground font-mono'>
              {formatNumber(channel.calls_24h, locale)}
            </p>
          </div>
          <div>
            <p>{t('24h Revenue')}</p>
            <p className='text-foreground font-mono'>
              {formatUsd(channel.quota_usd_24h)}
            </p>
          </div>
          <div>
            <p>{t('Models')}</p>
            <p className='text-foreground font-mono'>{channel.models.length}</p>
          </div>
        </div>
        {channel.price_sync ? (
          // 自动同步渠道：报价由上游定期同步，展示同步时间并提供立即同步入口
          <div className='space-y-2 border-t pt-2'>
            <div className='flex items-center justify-between gap-2'>
              <span className='text-muted-foreground text-xs'>
                {t('Price sync on')}
                {' · '}
                {channel.price_synced_at
                  ? formatTimestampToDate(channel.price_synced_at)
                  : t('Never synced')}
              </span>
              <div className='flex items-center gap-1.5'>
                <Button
                  variant='outline'
                  size='sm'
                  className='h-7'
                  onClick={() => setEditChannel(channel)}
                >
                  {t('Edit Channel')}
                </Button>
                <Button
                  variant='outline'
                  size='sm'
                  className='h-7'
                  disabled={syncing}
                  onClick={syncNow}
                >
                  <RefreshCw
                    aria-hidden
                    className={`mr-1 size-3 ${syncing ? 'animate-spin' : ''}`}
                  />
                  {syncing ? t('Syncing...') : t('Sync now')}
                </Button>
              </div>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Rates are fetched from your upstream automatically and re-synced on a schedule — no manual upkeep needed.'
              )}
            </p>
          </div>
        ) : (
          // 未开启同步的渠道：保留手动改倍率入口
          <div className='flex items-center justify-between gap-2 border-t pt-2'>
            <span className='text-muted-foreground text-xs'>
              {t('Price sync off')}
            </span>
            <div className='flex items-center gap-1.5'>
              <Button
                variant='outline'
                size='sm'
                className='h-7'
                onClick={() => setEditChannel(channel)}
              >
                {t('Edit Channel')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                className='h-7'
                onClick={() => setEditing(true)}
              >
                {t('Modify Rate')}
              </Button>
            </div>
          </div>
        )}
      </CardContent>

      {editChannel && (
        <ChannelEditDialog
          key={editChannel.channel_id}
          channel={editChannel}
          onClose={() => setEditChannel(null)}
          onSaved={onChanged}
        />
      )}

      <Dialog open={editing} onOpenChange={setEditing}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Modify Rate')}:{' '}
              {channel.name || `#${channel.channel_id}`}
            </DialogTitle>
          </DialogHeader>
          <RateEditForm
            channel={channel}
            onSaved={() => {
              setEditing(false)
              queryClient.invalidateQueries({
                queryKey: ['merchant', 'pools'],
              })
            }}
          />
        </DialogContent>
      </Dialog>
    </Card>
  )
}

/** 修改渠道倍率：只影响自己渠道，价格由后端按倍率派生。 */
function RateEditForm({
  channel,
  onSaved,
}: {
  channel: MerchantPoolChannel
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [rate, setRate] = useState(String(channel.rate_multiplier ?? ''))
  const [saving, setSaving] = useState(false)

  async function save() {
    const value = Number(rate)
    if (!Number.isFinite(value) || value <= 0) {
      toast.error(t('Rate multiplier must be a positive number'))
      return
    }
    setSaving(true)
    try {
      await updateMerchantRate({
        channel_id: channel.channel_id,
        rate_multiplier: value,
      })
      toast.success(t('Rate updated'))
      onSaved()
    } catch (error) {
      handleServerError(error, t('Failed to update rate'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className='space-y-4'>
      <div className='space-y-1.5'>
        <Label htmlFor='merchant-channel-rate'>
          {t('Channel Rate Multiplier')}
        </Label>
        <Input
          id='merchant-channel-rate'
          type='number'
          step='0.0001'
          value={rate}
          onChange={(e) => setRate(e.target.value)}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Discount factor relative to the platform standard price: 0.1 means 10% of it.'
          )}
        </p>
      </div>
      <Button onClick={save} disabled={saving}>
        {t('Save')}
      </Button>
    </div>
  )
}

/** 「报价面板」：只读展示渠道的模型报价与倍率，价格不可在商家侧编辑。 */
function MerchantQuotesTab() {
  const { t } = useTranslation()
  const quotesQuery = useQuery({
    queryKey: ['merchant', 'quotes'],
    queryFn: getMerchantQuotes,
  })
  const quotes = quotesQuery.data

  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [source, setSource] = useState('all')
  const [pool, setPool] = useState('all')

  const pools = useMemo(
    () => [...new Set((quotes ?? []).map((q) => q.pool_group))].sort(),
    [quotes]
  )
  const filtered = (quotes ?? []).filter((q) => {
    if (source !== 'all' && q.source !== source) return false
    if (pool !== 'all' && q.pool_group !== pool) return false
    if (search) {
      const kw = search.toLowerCase()
      if (
        !q.model_name.toLowerCase().includes(kw) &&
        !(q.channel_name || '').toLowerCase().includes(kw)
      ) {
        return false
      }
    }
    return true
  })

  /** 报价来源徽标：auto=上游自动同步 / derived=平台标准价×渠道倍率 / manual=管理员锁定。 */
  const sourceBadge = (s: string) => {
    if (s === 'auto') {
      return <Badge variant='default'>{t('Auto')}</Badge>
    }
    if (s === 'manual') {
      return <Badge variant='outline'>{t('Manual')}</Badge>
    }
    return <Badge variant='secondary'>{t('Derived')}</Badge>
  }

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-end gap-2'>
        <div className='w-48 space-y-1.5'>
          <Label htmlFor='merchant-quote-search'>{t('Search')}</Label>
          <Input
            id='merchant-quote-search'
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={(e) =>
              e.key === 'Enter' && setSearch(searchInput.trim())
            }
            placeholder={t('Search models or channels')}
          />
        </div>
        <div className='w-32 space-y-1.5'>
          <Label>{t('Source')}</Label>
          <Select
            value={source}
            onValueChange={(v) => setSource(v ?? 'all')}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All')}</SelectItem>
              <SelectItem value='auto'>{t('Auto')}</SelectItem>
              <SelectItem value='derived'>{t('Derived')}</SelectItem>
              <SelectItem value='manual'>{t('Manual')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='w-32 space-y-1.5'>
          <Label>{t('Pool')}</Label>
          <Select value={pool} onValueChange={(v) => setPool(v ?? 'all')}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All')}</SelectItem>
              {pools.map((p) => (
                <SelectItem key={p} value={p}>
                  {p}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          variant='outline'
          onClick={() => setSearch(searchInput.trim())}
        >
          {t('Search')}
        </Button>
        <span className='text-muted-foreground ml-auto text-xs'>
          {t('{{count}} quotes', { count: filtered.length })}
        </span>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Auto-synced quotes follow your upstream on a schedule; derived quotes follow your rate; manual quotes are locked by admins.'
        )}
      </p>

      <div className='rounded-xl border'>
        <div className='text-muted-foreground grid grid-cols-[1fr_0.9fr_1.2fr_0.8fr_0.8fr_0.7fr_1fr_1fr_1fr] gap-2 border-b px-3 py-2 text-xs'>
          <span>{t('Channel')}</span>
          <span>{t('Pool')}</span>
          <span>{t('Model')}</span>
          <span>{t('Channel Rate')}</span>
          <span>{t('Effective Rate')}</span>
          <span>{t('Source')}</span>
          <span>{t('Input $/1M')}</span>
          <span>{t('Output $/1M')}</span>
          <span className='text-right'>{t('Updated')}</span>
        </div>
        <div className='divide-y'>
          {filtered.map((q) => (
            <div
              key={q.id}
              title={`${t('Last updated')}: ${formatTimestampToDate(q.updated_at)}`}
              className='grid grid-cols-[1fr_0.9fr_1.2fr_0.8fr_0.8fr_0.7fr_1fr_1fr_1fr] items-center gap-2 px-3 py-2 text-sm'
            >
              <span className='truncate'>
                {q.channel_name || `#${q.channel_id}`}
              </span>
              <span className='truncate font-mono'>{q.pool_group}</span>
              <span className='truncate font-mono'>{q.model_name}</span>
              <span className='font-mono'>
                {formatRate(q.channel_rate_multiplier)}
              </span>
              <span className='font-mono'>{formatRate(q.rate_multiplier)}</span>
              <span>{sourceBadge(q.source)}</span>
              <span className='font-mono'>{q.input_price.toFixed(6)}</span>
              <span className='font-mono'>{q.output_price.toFixed(6)}</span>
              <span className='text-muted-foreground text-right text-xs'>
                {q.updated_at
                  ? formatTimestampToDate(q.updated_at)
                  : '--'}
              </span>
            </div>
          ))}
          {!filtered.length && (
            <div className='text-muted-foreground px-3 py-8 text-center'>
              {t('No quotes yet')}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function MerchantEarningsTab() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [page, setPage] = useState(1)
  const [modelInput, setModelInput] = useState('')
  const [modelName, setModelName] = useState('')
  const earningsQuery = useQuery({
    queryKey: ['merchant', 'earnings', page, modelName],
    queryFn: () =>
      getMerchantEarnings(page, 20, { model_name: modelName || undefined }),
  })
  const items = earningsQuery.data?.items ?? []
  const summary = earningsQuery.data?.summary
  const daily = earningsQuery.data?.daily
  const total = earningsQuery.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / 20))

  const chartData = useMemo(
    () =>
      (daily ?? []).map((d) => ({
        day: new Date(d.day * 1000).toLocaleDateString(locale, {
          month: 'short',
          day: 'numeric',
        }),
        amount: Number(d.amount.toFixed(4)),
      })),
    [daily, locale]
  )

  function applyFilter() {
    setPage(1)
    setModelName(modelInput.trim())
  }

  return (
    <div className='space-y-3'>
      {/* 汇总卡片 */}
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <Card>
          <CardContent className='py-4'>
            <p className='text-muted-foreground text-xs'>{t('Today')}</p>
            <p className='text-2xl font-bold'>{formatUsd(summary?.today)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-4'>
            <p className='text-muted-foreground text-xs'>
              {t('Last 7 days')}
            </p>
            <p className='text-2xl font-bold'>{formatUsd(summary?.last_7d)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-4'>
            <p className='text-muted-foreground text-xs'>
              {t('Last 30 days')}
            </p>
            <p className='text-2xl font-bold'>{formatUsd(summary?.last_30d)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-4'>
            <p className='text-muted-foreground text-xs'>
              {t('Total earnings')}
            </p>
            <p className='text-2xl font-bold'>{formatUsd(summary?.total)}</p>
          </CardContent>
        </Card>
      </div>

      {/* 近 30 天每日分润趋势 */}
      <Card>
        <CardContent className='py-4'>
          <p className='mb-2 text-sm font-medium'>
            {t('Daily earnings, last 30 days')}
          </p>
          {(chartData?.length ?? 0) > 0 ? (
            <ChartContainer
              config={{
                amount: { label: t('Earnings'), color: 'var(--primary)' },
              }}
              className='h-44 w-full'
            >
              <BarChart data={chartData} margin={{ left: 4, right: 8 }}>
                <CartesianGrid vertical={false} />
                <XAxis
                  dataKey='day'
                  tickLine={false}
                  axisLine={false}
                  tickMargin={8}
                  minTickGap={28}
                />
                <YAxis
                  width={64}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={(v: number) => `$${v}`}
                />
                <ChartTooltip
                  cursor={{ opacity: 0.15 }}
                  content={
                    <ChartTooltipContent
                      formatter={(value) => (
                        <span className='font-mono font-bold'>
                          {formatUsd(Number(value))}
                        </span>
                      )}
                    />
                  }
                />
                <Bar
                  dataKey='amount'
                  fill='var(--color-amount)'
                  radius={3}
                  maxBarSize={22}
                />
              </BarChart>
            </ChartContainer>
          ) : (
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('No earnings yet')}
            </p>
          )}
        </CardContent>
      </Card>

      {/* 分润明细 */}
      <div className='flex flex-wrap items-end gap-2'>
        <div className='w-48 space-y-1.5'>
          <Label htmlFor='merchant-earning-model'>{t('Model')}</Label>
          <Input
            id='merchant-earning-model'
            value={modelInput}
            onChange={(e) => setModelInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && applyFilter()}
            placeholder={t('Filter by model')}
          />
        </div>
        <Button variant='outline' onClick={applyFilter}>
          {t('Search')}
        </Button>
        <span className='text-muted-foreground ml-auto text-xs'>
          {t('{{count}} records', { count: total })}
        </span>
      </div>

      <div className='rounded-xl border'>
        <div className='text-muted-foreground grid grid-cols-[1.2fr_1.4fr_0.8fr_1fr] gap-2 border-b px-3 py-2 text-xs'>
          <span>{t('Time')}</span>
          <span>{t('Model')}</span>
          <span>{t('Channel')}</span>
          <span className='text-right'>{t('Amount (USD)')}</span>
        </div>
        <div className='divide-y'>
          {items.map((e) => (
            <div
              key={e.id}
              className='grid grid-cols-[1.2fr_1.4fr_0.8fr_1fr] items-center gap-2 px-3 py-2 text-sm'
            >
              <span className='text-muted-foreground'>
                {formatTimestampToDate(e.created_at)}
              </span>
              <span className='truncate font-mono'>{e.model_name}</span>
              <span className='font-mono'>#{e.channel_id}</span>
              <span className='text-right font-mono'>
                +
                {formatCurrencyFromUSD(e.amount, {
                  digitsLarge: 6,
                  digitsSmall: 6,
                })}
              </span>
            </div>
          ))}
          {!items.length && (
            <div className='text-muted-foreground px-3 py-8 text-center'>
              {t('No earnings yet')}
            </div>
          )}
        </div>
        {items.length > 0 && (
          <div className='text-muted-foreground flex items-center justify-between gap-2 border-t px-3 py-2 text-xs'>
            <span>
              {t('Page total')}:{' '}
              <span className='text-foreground font-mono'>
                +
                {formatCurrencyFromUSD(earningsQuery.data?.page_amount ?? 0, {
                  digitsLarge: 6,
                  digitsSmall: 6,
                })}
              </span>
            </span>
            <span className='flex items-center gap-2'>
              {t('Page {{page}} of {{total}}', {
                page,
                total: totalPages,
              })}
              <Button
                variant='outline'
                size='sm'
                className='h-7'
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
              >
                {t('Prev')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                className='h-7'
                disabled={page >= totalPages}
                onClick={() => setPage(page + 1)}
              >
                {t('Next')}
              </Button>
            </span>
          </div>
        )}
      </div>
    </div>
  )
}

function MerchantWithdrawalsTab({
  balance,
  onChanged,
}: {
  balance: number
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [open, setOpen] = useState(false)
  const [amount, setAmount] = useState('')
  const [account, setAccount] = useState('')
  const [saving, setSaving] = useState(false)

  const withdrawalsQuery = useQuery({
    queryKey: ['merchant', 'withdrawals', page],
    queryFn: () => getMerchantWithdrawals(page, 20),
  })
  const items = withdrawalsQuery.data?.items ?? []

  async function submit() {
    setSaving(true)
    try {
      await createMerchantWithdrawal({
        amount: Number(amount),
        account_info: account,
      })
      toast.success(t('Withdrawal request submitted'))
      setOpen(false)
      setAmount('')
      setAccount('')
      onChanged()
    } catch (error) {
      handleServerError(error, t('Failed to submit withdrawal request'))
    } finally {
      setSaving(false)
    }
  }

  const statusBadge = (s: number) => {
    if (s === 0) return <Badge variant='secondary'>{t('Pending')}</Badge>
    if (s === 1) return <Badge variant='default'>{t('Approved')}</Badge>
    if (s === 2) return <Badge variant='default'>{t('Paid')}</Badge>
    return <Badge variant='destructive'>{t('Rejected')}</Badge>
  }

  return (
    <div>
      <div className='mb-3 flex items-center justify-between'>
        <p className='text-muted-foreground text-sm'>
          {t('Available balance')}: {formatUsd(balance)}
        </p>
        <Button onClick={() => setOpen(true)}>{t('Request Withdrawal')}</Button>
      </div>
      <div className='rounded-xl border'>
        <div className='text-muted-foreground grid grid-cols-[1fr_1fr_1fr_1fr_1fr] gap-2 border-b px-3 py-2 text-xs'>
          <span>{t('Time')}</span>
          <span>{t('Amount (USD)')}</span>
          <span>{t('Account')}</span>
          <span>{t('Status')}</span>
          <span>{t('Review Note')}</span>
        </div>
        <div className='divide-y'>
          {items.map((w) => (
            <div
              key={w.id}
              className='grid grid-cols-[1fr_1fr_1fr_1fr_1fr] items-center gap-2 px-3 py-2 text-sm'
            >
              <span className='text-muted-foreground'>
                {formatTimestampToDate(w.created_at)}
              </span>
              <span className='font-mono'>
                {formatCurrencyFromUSD(w.amount, {
                  digitsLarge: 2,
                  digitsSmall: 2,
                })}
              </span>
              <span className='truncate'>{w.account_info}</span>
              <span>{statusBadge(w.status)}</span>
              <span className='text-muted-foreground truncate'>
                {w.review_note || '--'}
              </span>
            </div>
          ))}
          {!items.length && (
            <div className='text-muted-foreground px-3 py-8 text-center'>
              {t('No withdrawals yet')}
            </div>
          )}
        </div>
        {(withdrawalsQuery.data?.total ?? 0) > page * 20 && (
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

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Request Withdrawal')}</DialogTitle>
          </DialogHeader>
          <div className='space-y-4'>
            <div className='space-y-1.5'>
              <Label htmlFor='merchant-withdrawal-amount'>
                {t('Amount (USD)')}
              </Label>
              <Input
                id='merchant-withdrawal-amount'
                type='number'
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                placeholder={balance.toFixed(2)}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='merchant-withdrawal-account'>
                {t('Payout Account')}
              </Label>
              <Input
                id='merchant-withdrawal-account'
                value={account}
                onChange={(e) => setAccount(e.target.value)}
                placeholder={t('Alipay / USDT / Bank card...')}
              />
            </div>
            <Button onClick={submit} disabled={saving || !amount || !account}>
              {t('Submit')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

/** 时间范围快捷选项对应的起始时间戳（在 queryFn 内取当前时间，避免渲染期取随机值）。 */
function rangeStartTimestamp(range: string): number | undefined {
  const now = Date.now()
  if (range === 'today') {
    const d = new Date()
    d.setHours(0, 0, 0, 0)
    return Math.floor(d.getTime() / 1000)
  }
  const days: Record<string, number> = { '3d': 3, '7d': 7, '30d': 30 }
  if (range in days) {
    return Math.floor((now - days[range] * 86400_000) / 1000)
  }
  return undefined
}

/**
 * 商家调用记录：只展示自己渠道的调用日志（后端已脱敏，无终端用户身份信息）。
 * 支持按分组 / 模型 / 类型 / 时间范围过滤，顶部展示与当前筛选同口径的统计，
 * 底部为常规分页。
 */
function MerchantLogsTab({ groups }: { groups: string[] }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const PAGE_SIZE = 20
  const [page, setPage] = useState(1)
  const [group, setGroup] = useState('all')
  const [modelInput, setModelInput] = useState('')
  const [modelName, setModelName] = useState('')
  const [logType, setLogType] = useState('all')
  const [range, setRange] = useState('7d')

  const logsQuery = useQuery({
    queryKey: [
      'merchant',
      'logs',
      page,
      group,
      modelName,
      logType,
      range,
    ],
    queryFn: () =>
      getMerchantLogs(page, PAGE_SIZE, {
        group: group === 'all' ? undefined : group,
        model_name: modelName || undefined,
        type: logType === 'all' ? undefined : Number(logType),
        start_timestamp: rangeStartTimestamp(range),
      }),
  })
  const items = logsQuery.data?.items ?? []
  const stats = logsQuery.data?.stats
  const total = logsQuery.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  /** 应用过滤条件：重置到第一页，避免停留在越界页码。 */
  function applyFilter() {
    setPage(1)
    setModelName(modelInput.trim())
  }

  return (
    <div className='space-y-3'>
      {/* 当前筛选口径的统计 */}
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
        <Card>
          <CardContent className='py-3'>
            <p className='text-muted-foreground text-xs'>{t('Calls')}</p>
            <p className='text-xl font-bold'>
              {formatNumber(stats?.calls ?? 0, locale)}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-3'>
            <p className='text-muted-foreground text-xs'>{t('Failed')}</p>
            <p className='text-xl font-bold'>
              {formatNumber(stats?.fails ?? 0, locale)}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-3'>
            <p className='text-muted-foreground text-xs'>{t('Spend')}</p>
            <p className='text-xl font-bold'>
              {formatUsd((stats?.quota ?? 0) / 500000)}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className='py-3'>
            <p className='text-muted-foreground text-xs'>{t('Tokens')}</p>
            <p className='text-xl font-bold'>
              {formatNumber(stats?.tokens ?? 0, locale)}
            </p>
          </CardContent>
        </Card>
      </div>

      <div className='flex flex-wrap items-end gap-2'>
        <div className='w-36 space-y-1.5'>
          <Label>{t('Time')}</Label>
          <Select
            value={range}
            onValueChange={(v) => {
              setRange(v ?? '7d')
              setPage(1)
            }}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='today'>{t('Today')}</SelectItem>
              <SelectItem value='3d'>{t('Last 3 days')}</SelectItem>
              <SelectItem value='7d'>{t('Last 7 days')}</SelectItem>
              <SelectItem value='30d'>{t('Last 30 days')}</SelectItem>
              <SelectItem value='all'>{t('All time')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='w-40 space-y-1.5'>
          <Label>{t('Pool')}</Label>
          <Select
            value={group}
            onValueChange={(v) => {
              setGroup(v ?? 'all')
              setPage(1)
            }}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All')}</SelectItem>
              {groups.map((g) => (
                <SelectItem key={g} value={g}>
                  {g}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className='w-32 space-y-1.5'>
          <Label>{t('Type')}</Label>
          <Select
            value={logType}
            onValueChange={(v) => {
              setLogType(v ?? 'all')
              setPage(1)
            }}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All')}</SelectItem>
              <SelectItem value='2'>{t('Consume')}</SelectItem>
              <SelectItem value='5'>{t('Error')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className='w-44 space-y-1.5'>
          <Label htmlFor='merchant-log-model'>{t('Model')}</Label>
          <Input
            id='merchant-log-model'
            value={modelInput}
            onChange={(e) => setModelInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && applyFilter()}
            placeholder={t('Filter by model')}
          />
        </div>
        <Button variant='outline' onClick={applyFilter}>
          {t('Search')}
        </Button>
        <span className='text-muted-foreground ml-auto text-xs'>
          {t('{{count}} records', { count: total })}
        </span>
      </div>

      <div className='rounded-xl border'>
        <div className='text-muted-foreground grid grid-cols-[1.1fr_1fr_1.4fr_1fr_1fr_0.8fr_0.8fr_1fr] gap-2 border-b px-3 py-2 text-xs'>
          <span>{t('Time')}</span>
          <span>{t('Pool')}</span>
          <span>{t('Model')}</span>
          <span>{t('Channel')}</span>
          <span>{t('Tokens')}</span>
          <span>{t('Latency')}</span>
          <span>{t('Result')}</span>
          <span className='text-right'>{t('Amount (USD)')}</span>
        </div>
        <div className='divide-y'>
          {items.map((log) => (
            <div
              key={log.id}
              className='grid grid-cols-[1.1fr_1fr_1.4fr_1fr_1fr_0.8fr_0.8fr_1fr] items-center gap-2 px-3 py-2 text-sm'
            >
              <span className='text-muted-foreground'>
                {formatTimestampToDate(log.created_at)}
              </span>
              <span className='truncate font-mono'>
                {log.group || t('Default')}
              </span>
              <span className='truncate font-mono'>{log.model_name}</span>
              <span className='truncate'>
                {log.channel_name || `#${log.channel}`}
              </span>
              <span className='font-mono'>
                {log.prompt_tokens}/{log.completion_tokens}
              </span>
              <span className='font-mono'>{log.use_time}s</span>
              <span>
                {log.type === 5 ? (
                  <Badge variant='destructive'>{t('Failed')}</Badge>
                ) : (
                  <Badge variant='secondary'>{t('Success')}</Badge>
                )}
              </span>
              <span className='text-right font-mono'>
                {formatQuotaWithCurrency(log.quota, {
                  digitsLarge: 4,
                  digitsSmall: 6,
                })}
              </span>
            </div>
          ))}
          {!items.length && (
            <div className='text-muted-foreground px-3 py-8 text-center'>
              {t('No usage records yet')}
            </div>
          )}
        </div>
        {items.length > 0 && (
          <div className='flex items-center justify-end gap-2 border-t px-3 py-2 text-xs'>
            <span className='text-muted-foreground'>
              {t('Page {{page}} of {{total}}', { page, total: totalPages })}
            </span>
            <Button
              variant='outline'
              size='sm'
              className='h-7'
              disabled={page <= 1}
              onClick={() => setPage(page - 1)}
            >
              {t('Prev')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              className='h-7'
              disabled={page >= totalPages}
              onClick={() => setPage(page + 1)}
            >
              {t('Next')}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
