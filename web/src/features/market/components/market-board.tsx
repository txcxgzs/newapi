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
import { Link } from '@tanstack/react-router'
import {
  Activity,
  ArrowLeft,
  BarChart3,
  BookOpen,
  CandlestickChart,
  Check,
  ChevronDown,
  Copy,
  Layers,
  RefreshCw,
  Search,
  ShieldCheck,
  Store,
  X,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { useMarketKline, useMarketOrderBook, useMarketOverview } from '../hooks/use-market-data'
import {
  formatMarketPrice,
  formatMarketRatio,
  formatMarketVolume,
} from '../lib/format'
import type { CandleInterval } from '../types'
import { MarketKlineChart } from './market-kline-chart'
import { StockList } from './stock-list'

const INTERVALS: CandleInterval[] = ['1m', '5m', '1h']
const ALL_POOLS = 'all'

export function MarketBoard() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const authUser = useAuthStore((state) => state.auth.user)
  const userRole = authUser?.role ?? 0
  const [pool, setPool] = useState<string>('')
  const [picked, setPicked] = useState<{
    pool: string
    model: string
  } | null>(null)
  const [interval, setIntervalPeriod] = useState<CandleInterval>('1m')
  const [orderBookScope, setOrderBookScope] = useState<'selected' | 'all'>(
    'selected'
  )
  const [orderBookStrategy, setOrderBookStrategy] =
    useState<string>('price_ttft')
  const [orderBookSearch, setOrderBookSearch] = useState<string>('')
  const [guideOpen, setGuideOpen] = useState<boolean>(false)
  const [copiedCurl, setCopiedCurl] = useState<boolean>(false)

  const {
    data: overview,
    isLoading,
    isFetching,
    refetch,
  } = useMarketOverview(pool || undefined)

  const stocks = useMemo(() => overview?.stocks ?? [], [overview])
  const pools = useMemo(() => overview?.pools ?? [], [overview])
  const showPoolFilter = pools.length > 0 || pool !== ''

  // 未手动选择时跟随成交额第一的模型，避免在副作用中回写 state
  const selected = picked ?? stocks[0] ?? null

  const { data: candles } = useMarketKline(
    selected?.pool ?? '',
    selected?.model ?? '',
    interval,
    200
  )

  const {
    data: orderBookData,
    isFetching: orderBookFetching,
    refetch: refetchOrderBook,
  } = useMarketOrderBook({
    pool:
      orderBookScope === 'selected'
        ? (selected?.pool ?? (pool || undefined))
        : pool || undefined,
    model: orderBookScope === 'selected' ? selected?.model : undefined,
    strategy: orderBookStrategy,
  })
  const orderBookEntries = useMemo(() => {
    const entries = orderBookData?.entries ?? []
    const kw = orderBookSearch.trim().toLowerCase()
    if (!kw) return entries
    return entries.filter(
      (entry) =>
        entry.model.toLowerCase().includes(kw) ||
        entry.merchant_name.toLowerCase().includes(kw) ||
        entry.merchant_code.toLowerCase().includes(kw) ||
        entry.pool.toLowerCase().includes(kw)
    )
  }, [orderBookData, orderBookSearch])

  const sampleModel = selected?.model || 'gpt-4o'
  const originUrl =
    typeof window !== 'undefined' && window.location?.origin
      ? window.location.origin
      : 'https://your-gateway.example.com'
  const buyerCurlSnippet = `curl -sS -D - ${originUrl}/v1/chat/completions \\
  -H "Authorization: Bearer sk-your-token" \\
  -H "Content-Type: application/json" \\
  -H "X-Routing-Strategy: ${orderBookStrategy}" \\
  -H "X-Max-TTFT-Ms: 800" \\
  -H "X-Min-TPS: 25" \\
  -H "X-Max-Price: 5.0" \\
  -d '{"model":"${sampleModel}","messages":[{"role":"user","content":"hi"}]}'`

  async function handleCopyCurl() {
    try {
      await navigator.clipboard.writeText(buyerCurlSnippet)
      setCopiedCurl(true)
      toast.success(t('Copied example request'))
      setTimeout(() => setCopiedCurl(false), 1800)
    } catch {
      toast.error(t('Failed to copy to clipboard'))
    }
  }

  const orderBookEmptyText = (() => {
    if (orderBookFetching) return t('Loading order book...')
    if (orderBookSearch) {
      return t('No order-book quotes match "{{query}}"', {
        query: orderBookSearch,
      })
    }
    return t('No active order-book bids available')
  })()

  function renderQuoteSourceBadge(src: string) {
    switch (src) {
      case 'merchant':
        return (
          <Badge variant='default' className='h-5 px-1.5 text-[10px]'>
            {t('Merchant Bid')}
          </Badge>
        )
      case 'auto':
        return (
          <Badge variant='secondary' className='h-5 px-1.5 text-[10px]'>
            {t('Auto Sync')}
          </Badge>
        )
      case 'manual':
        return (
          <Badge variant='outline' className='h-5 px-1.5 text-[10px]'>
            {t('Manual Lock')}
          </Badge>
        )
      default:
        return (
          <Badge variant='secondary' className='h-5 px-1.5 text-[10px]'>
            {src || t('Derived')}
          </Badge>
        )
    }
  }

  const current = stocks.find(
    (s) => s.pool === selected?.pool && s.model === selected?.model
  )
  const up = (current?.change_24h_pct ?? 0) >= 0

  const emptyAction = (() => {
    if (!authUser) {
      return (
        <Button variant='outline' size='sm' render={<Link to='/pricing' />}>
          {t('Model Pricing')}
        </Button>
      )
    }
    if (userRole >= ROLE.ADMIN) {
      return (
        <Button variant='outline' size='sm' render={<Link to='/channels' />}>
          {t('Manage Channels')}
        </Button>
      )
    }
    if (userRole >= ROLE.MERCHANT && userRole !== ROLE.FINANCE) {
      return (
        <Button variant='outline' size='sm' render={<Link to='/merchant' />}>
          {t('Merchant Portal')}
        </Button>
      )
    }
    if (userRole !== ROLE.FINANCE) {
      return (
        <Button
          variant='outline'
          size='sm'
          render={<Link to='/merchant/apply' />}
        >
          {t('Become a Merchant')}
        </Button>
      )
    }
    return null
  })()

  function renderBody() {
    if (isLoading) {
      return (
        <div className='bg-card text-muted-foreground flex min-h-[360px] flex-col items-center justify-center gap-3 rounded-xl border p-8'>
          <RefreshCw className='size-6 animate-spin opacity-60' />
          <span className='text-sm'>{t('Loading...')}</span>
        </div>
      )
    }

    if (!stocks.length) {
      return (
        <div className='bg-card flex min-h-[380px] flex-col items-center justify-center rounded-xl border px-6 py-12 text-center'>
          <div className='bg-primary/10 text-primary mb-4 flex size-14 items-center justify-center rounded-2xl'>
            <CandlestickChart className='size-7' />
          </div>
          <h2 className='text-lg font-semibold tracking-tight'>
            {pool
              ? t('No transaction data in pool "{{pool}}" yet', { pool })
              : t('No model market data yet')}
          </h2>
          <p className='text-muted-foreground mt-2 max-w-xl text-sm leading-relaxed'>
            {t(
              'Once channels go live and API requests start generating transactions, 1m / 5m / 1h K-line candles and merchant listed quotes will appear here automatically.'
            )}
          </p>

          <div className='mt-6 grid w-full max-w-2xl grid-cols-1 gap-3 text-left sm:grid-cols-3'>
            <div className='bg-muted/35 rounded-lg border px-3.5 py-3'>
              <div className='mb-1 flex items-center gap-1.5 text-xs font-medium'>
                <Activity className='text-primary size-3.5' />
                {t('Minute-level K-Line')}
              </div>
              <p className='text-muted-foreground text-xs leading-relaxed'>
                {t('Blended $/1M-token transaction prices updated every minute')}
              </p>
            </div>
            <div className='bg-muted/35 rounded-lg border px-3.5 py-3'>
              <div className='mb-1 flex items-center gap-1.5 text-xs font-medium'>
                <BarChart3 className='text-primary size-3.5' />
                {t('24h Volume & Trend')}
              </div>
              <p className='text-muted-foreground text-xs leading-relaxed'>
                {t('Real-time 24h high, low, call count, and price change')}
              </p>
            </div>
            <div className='bg-muted/35 rounded-lg border px-3.5 py-3'>
              <div className='mb-1 flex items-center gap-1.5 text-xs font-medium'>
                <Layers className='text-primary size-3.5' />
                {t('Merchant Listing Reference')}
              </div>
              <p className='text-muted-foreground text-xs leading-relaxed'>
                {t('Active pool listed ratios alongside actual fill prices')}
              </p>
            </div>
          </div>

          <div className='mt-6 flex flex-wrap items-center justify-center gap-2.5'>
            {pool && (
              <Button variant='default' size='sm' onClick={() => setPool('')}>
                {t('View All Pools')}
              </Button>
            )}
            <Button
              variant={pool ? 'outline' : 'default'}
              size='sm'
              onClick={() => void refetch()}
              disabled={isFetching}
            >
              <RefreshCw
                className={cn('mr-1.5 size-3.5', isFetching && 'animate-spin')}
              />
              {t('Refresh')}
            </Button>
            {emptyAction}
          </div>
        </div>
      )
    }

    return (
      <div className='grid gap-4 lg:grid-cols-[420px_1fr]'>
        {/* 股票列表 */}
        <div className='bg-card max-h-[560px] overflow-hidden rounded-xl border'>
          <StockList
            stocks={stocks}
            selectedPool={selected?.pool ?? ''}
            selectedModel={selected?.model ?? ''}
            onSelect={(p, m) => setPicked({ pool: p, model: m })}
            className='max-h-[560px]'
          />
        </div>

        {/* K线与详情 */}
        <div className='bg-card rounded-xl border p-4'>
          {current ? (
            <>
              <div className='mb-3 flex flex-wrap items-baseline justify-between gap-3'>
                <div>
                  <div className='flex items-baseline gap-3'>
                    <span className='font-mono text-xl font-semibold'>
                      {current.model}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      {current.pool}
                    </span>
                  </div>
                  <div className='mt-1 flex items-baseline gap-3'>
                    <span
                      className={`font-mono text-3xl font-bold ${up ? 'text-red-500' : 'text-emerald-500'}`}
                    >
                      {formatMarketPrice(current.last_price, locale)}
                    </span>
                    <span
                      className={`font-mono text-sm ${up ? 'text-red-500' : 'text-emerald-500'}`}
                    >
                      {up ? '+' : ''}
                      {current.change_24h_pct.toFixed(2)}%
                    </span>
                  </div>
                </div>
                <ToggleGroup
                  value={[interval]}
                  onValueChange={(values) => {
                    const next = values[0]
                    if (next === '1m' || next === '5m' || next === '1h') {
                      setIntervalPeriod(next)
                    }
                  }}
                  variant='outline'
                  size='sm'
                  aria-label={t('Interval')}
                >
                  {INTERVALS.map((i) => (
                    <ToggleGroupItem key={i} value={i}>
                      {i}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </div>
              <div className='text-muted-foreground mb-3 grid grid-cols-2 gap-2 text-xs sm:grid-cols-4'>
                <span>
                  {t('24h High')}:{' '}
                  {formatMarketPrice(current.high_24h, locale)}
                </span>
                <span>
                  {t('24h Low')}: {formatMarketPrice(current.low_24h, locale)}
                </span>
                <span>
                  {t('24h Calls')}:{' '}
                  {formatNumber(current.vol_calls_24h, locale)}
                </span>
                <span>
                  {t('24h Volume')}:{' '}
                  {formatMarketVolume(current.vol_quota_24h, locale)}
                </span>
              </div>
              {/* 商家挂牌区：与成交价相互独立，展示号池内商家的挂单倍率与最低挂牌报价 */}
              <div className='bg-muted/40 mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg px-3 py-2 text-xs'>
                <span className='text-foreground font-medium'>
                  {t('Merchant Listing')}
                </span>
                <span className='text-muted-foreground'>
                  {t('Listed Ratio')}:{' '}
                  <span className='text-foreground font-mono font-semibold'>
                    {formatMarketRatio(current.listed_ratio)}
                  </span>
                </span>
                <span className='text-muted-foreground'>
                  {t('Avg Ratio')}:{' '}
                  <span className='font-mono'>
                    {formatMarketRatio(current.listed_ratio_avg)}
                  </span>
                </span>
                <span className='text-muted-foreground'>
                  {t('Listed Ask')}:{' '}
                  <span className='font-mono'>
                    {formatMarketPrice(current.listed_input_price, locale)} /{' '}
                    {formatMarketPrice(current.listed_output_price, locale)}
                  </span>
                </span>
                <span className='text-muted-foreground'>
                  {t('Listed Channels')}:{' '}
                  <span className='font-mono'>{current.listed_channels}</span>
                </span>
              </div>
              <MarketKlineChart candles={candles ?? []} interval={interval} />
              <p className='text-muted-foreground mt-2 text-center text-xs'>
                {t(
                  'Candles reflect blended $/1M-token transaction prices. Requests are billed at the routed channel listed price multiplied by the group ratio'
                )}
              </p>
            </>
          ) : (
            <div className='text-muted-foreground flex h-80 items-center justify-center'>
              {t('Select a model to view its market')}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className='mx-auto w-full max-w-7xl px-4 py-6'>
      {/* 行情页是独立路由，没有站点导航，这里提供返回首页的入口 */}
      <Button
        variant='ghost'
        size='sm'
        className='text-muted-foreground hover:text-foreground -ml-2 mb-2'
        render={<Link to='/' />}
      >
        <ArrowLeft />
        {t('Back')}
      </Button>
      <div className='mb-4 flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h1 className='text-2xl font-bold tracking-tight'>
            {t('API Market')}
          </h1>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Real transaction prices of every model pool, aggregated per minute'
            )}
          </p>
        </div>
        {showPoolFilter && (
          <ToggleGroup
            value={[pool || ALL_POOLS]}
            onValueChange={(values) => {
              const next = values[0]
              setPool(!next || next === ALL_POOLS ? '' : next)
            }}
            variant='outline'
            size='sm'
            className='flex-wrap justify-end'
            aria-label={t('Pool')}
          >
            <ToggleGroupItem value={ALL_POOLS}>{t('All Pools')}</ToggleGroupItem>
            {pools.map((p) => (
              <ToggleGroupItem key={p} value={p}>
                {p}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        )}
      </div>

      {renderBody()}

      <div className='bg-card mt-4 rounded-xl border p-4'>
        <div className='mb-3 flex flex-wrap items-center justify-between gap-3'>
          <div>
            <div className='flex items-center gap-2'>
              <h2 className='text-sm font-semibold tracking-tight'>
                {t('Live Order Book')}
              </h2>
              <Badge variant='secondary' className='h-5 px-1.5 font-mono text-[10px]'>
                {orderBookEntries.length}
              </Badge>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Real-time merchant quotes ranked by price, TTFT first-token latency, and health EMA'
              )}
            </p>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <div className='relative w-44 sm:w-52'>
              <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2' />
              <Input
                value={orderBookSearch}
                onChange={(e) => setOrderBookSearch(e.target.value)}
                placeholder={t('Filter model or provider...')}
                className='h-8 pr-7 pl-8 text-xs'
              />
              {orderBookSearch && (
                <button
                  type='button'
                  aria-label={t('Clear search')}
                  onClick={() => setOrderBookSearch('')}
                  className='text-muted-foreground hover:text-foreground absolute top-1/2 right-2 -translate-y-1/2'
                >
                  <X className='size-3.5' />
                </button>
              )}
            </div>
            <ToggleGroup
              value={[orderBookStrategy]}
              onValueChange={(values) => {
                if (values[0]) setOrderBookStrategy(values[0])
              }}
              variant='outline'
              size='sm'
            >
              <ToggleGroupItem value='price_ttft'>
                {t('Price × TTFT')}
              </ToggleGroupItem>
              <ToggleGroupItem value='lowest_price'>
                {t('Lowest Price')}
              </ToggleGroupItem>
              <ToggleGroupItem value='lowest_ttft'>
                {t('Lowest TTFT')}
              </ToggleGroupItem>
            </ToggleGroup>
            <ToggleGroup
              value={[orderBookScope]}
              onValueChange={(values) => {
                if (values[0] === 'selected' || values[0] === 'all') {
                  setOrderBookScope(values[0])
                }
              }}
              variant='outline'
              size='sm'
            >
              <ToggleGroupItem value='selected'>
                {t('Selected Model')}
              </ToggleGroupItem>
              <ToggleGroupItem value='all'>{t('All Models')}</ToggleGroupItem>
            </ToggleGroup>
            <Button
              variant='outline'
              size='sm'
              className='h-8 px-2.5 text-xs'
              onClick={() => void refetchOrderBook()}
              disabled={orderBookFetching}
              aria-label={t('Refresh order book')}
            >
              <RefreshCw
                className={cn(
                  'size-3.5',
                  orderBookFetching && 'animate-spin'
                )}
              />
            </Button>
          </div>
        </div>

        <div className='overflow-x-auto'>
          <table className='w-full text-left text-xs'>
            <thead>
              <tr className='border-border/60 text-muted-foreground border-b'>
                <th className='px-2 py-2 font-medium'>#</th>
                <th className='px-2 py-2 font-medium'>{t('Provider')}</th>
                <th className='px-2 py-2 font-medium'>{t('Pool')}</th>
                <th className='px-2 py-2 font-medium'>{t('Model')}</th>
                <th className='px-2 py-2 text-right font-medium'>
                  {t('Ask In / Out ($/1M)')}
                </th>
                <th className='px-2 py-2 text-right font-medium'>
                  {t('TTFT')}
                </th>
                <th className='px-2 py-2 text-right font-medium'>
                  {t('TPS')}
                </th>
                <th className='px-2 py-2 text-right font-medium'>
                  {t('Success')}
                </th>
                <th className='px-2 py-2 text-right font-medium'>
                  {t('Source')}
                </th>
              </tr>
            </thead>
            <tbody>
              {orderBookEntries.length === 0 ? (
                <tr>
                  <td
                    colSpan={9}
                    className='text-muted-foreground py-6 text-center'
                  >
                    {orderBookEmptyText}
                  </td>
                </tr>
              ) : (
                orderBookEntries.map((entry) => {
                  const isTop = entry.rank === 1
                  const lowHealth =
                    entry.sample_count > 0 && entry.success_rate < 0.9
                  return (
                    <tr
                      key={`${entry.pool}-${entry.model}-${entry.rank}-${entry.merchant_code}`}
                      className={cn(
                        'border-border/40 hover:bg-muted/30 border-b last:border-0',
                        isTop && 'bg-emerald-500/[0.03]'
                      )}
                    >
                      <td className='px-2 py-2 font-mono font-semibold'>
                        <div className='flex items-center gap-1.5'>
                          <span>#{entry.rank}</span>
                          {isTop && (
                            <Badge
                              variant='outline'
                              className='h-4 border-emerald-500/40 bg-emerald-500/10 px-1 text-[10px] text-emerald-600 dark:text-emerald-400'
                            >
                              {t('Best')}
                            </Badge>
                          )}
                        </div>
                      </td>
                      <td className='px-2 py-2'>
                        <span className='font-medium'>
                          {entry.merchant_name}
                        </span>
                        {entry.merchant_code && (
                          <span className='text-muted-foreground ml-1 font-mono'>
                            ({entry.merchant_code})
                          </span>
                        )}
                      </td>
                      <td className='px-2 py-2 font-mono'>{entry.pool}</td>
                      <td className='px-2 py-2 font-mono font-medium'>
                        {entry.model}
                      </td>
                      <td className='px-2 py-2 text-right font-mono tabular-nums'>
                        {formatMarketPrice(entry.input_price, locale)} /{' '}
                        {formatMarketPrice(entry.output_price, locale)}
                      </td>
                      <td
                        className={cn(
                          'px-2 py-2 text-right font-mono tabular-nums',
                          !entry.has_live_metric &&
                            'text-muted-foreground italic'
                        )}
                        title={
                          entry.has_live_metric
                            ? t('Measured EMA first-token latency')
                            : t('Cold-start baseline prior')
                        }
                      >
                        {entry.has_live_metric
                          ? `${Math.round(entry.ttft_ms)} ms`
                          : `~${Math.round(entry.ttft_ms)} ms`}
                      </td>
                      <td className='px-2 py-2 text-right font-mono tabular-nums'>
                        {entry.tps > 0 ? `${entry.tps.toFixed(1)}/s` : '--'}
                      </td>
                      <td
                        className={cn(
                          'px-2 py-2 text-right font-mono tabular-nums',
                          lowHealth && 'font-medium text-amber-600 dark:text-amber-400'
                        )}
                      >
                        {entry.sample_count > 0
                          ? `${(entry.success_rate * 100).toFixed(1)}% (${entry.sample_count})`
                          : '--'}
                      </td>
                      <td className='px-2 py-2 text-right'>
                        {renderQuoteSourceBadge(entry.quote_source)}
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      <Collapsible
        open={guideOpen}
        onOpenChange={setGuideOpen}
        className='bg-card mt-4 rounded-xl border p-4'
      >
        <CollapsibleTrigger
          render={
            <button
              type='button'
              className='flex w-full items-center justify-between gap-2 text-left'
            />
          }
        >
          <div className='flex items-center gap-2'>
            <BookOpen className='text-primary size-4' />
            <div>
              <h2 className='text-sm font-semibold tracking-tight'>
                {t('Order-Book Routing & Bidding Guide')}
              </h2>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Per-request auction routing headers, X-Relay-* audit verification, and provider live bidding rules'
                )}
              </p>
            </div>
          </div>
          <ChevronDown
            className={cn(
              'text-muted-foreground size-4 shrink-0 transition-transform',
              guideOpen && 'rotate-180'
            )}
          />
        </CollapsibleTrigger>
        <CollapsibleContent className='mt-4 grid gap-4 border-t pt-4 lg:grid-cols-2'>
          <div className='bg-muted/25 space-y-3 rounded-lg border p-3.5 text-xs'>
            <div className='flex items-center justify-between gap-2'>
              <div className='flex items-center gap-1.5 font-semibold'>
                <ShieldCheck className='text-primary size-4' />
                <span>{t('Buyer SLA & Header Override Quickstart')}</span>
              </div>
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-7 gap-1 px-2 text-[11px]'
                onClick={() => void handleCopyCurl()}
              >
                {copiedCurl ? (
                  <Check className='size-3 text-emerald-500' />
                ) : (
                  <Copy className='size-3' />
                )}
                {copiedCurl ? t('Copied') : t('Copy cURL')}
              </Button>
            </div>
            <pre className='bg-background overflow-x-auto rounded-md border p-2.5 font-mono text-[11px] leading-relaxed'>
              {buyerCurlSnippet}
            </pre>
            <div className='space-y-1.5 leading-relaxed'>
              <p className='font-medium'>
                {t('Verifiable Response Headers & Usage Logs')}
              </p>
              <p className='text-muted-foreground'>
                {t(
                  'Every response includes X-Relay-Provider, X-Relay-Provider-Code, X-Relay-Group, X-Relay-Strategy, X-Relay-Quote-Source, X-Relay-Price-Input, and X-Relay-Price-Output, and records them permanently in your Usage Logs.'
                )}
              </p>
              <div className='flex flex-wrap gap-2 pt-1'>
                {authUser ? (
                  <>
                    <Button
                      variant='outline'
                      size='sm'
                      className='h-7 text-xs'
                      render={<Link to='/keys' />}
                    >
                      {t('Configure Token SLA')}
                    </Button>
                    <Button
                      variant='outline'
                      size='sm'
                      className='h-7 text-xs'
                      render={<Link to='/usage-logs' />}
                    >
                      {t('Inspect Fill Logs')}
                    </Button>
                  </>
                ) : (
                  <Button
                    variant='outline'
                    size='sm'
                    className='h-7 text-xs'
                    render={<Link to='/sign-in' />}
                  >
                    {t('Sign in to configure keys')}
                  </Button>
                )}
              </div>
            </div>
          </div>

          <div className='bg-muted/25 space-y-3 rounded-lg border p-3.5 text-xs'>
            <div className='flex items-center gap-1.5 font-semibold'>
              <Store className='text-primary size-4' />
              <span>{t('Provider Real-Time Bidding & Settlement')}</span>
            </div>
            <ul className='text-muted-foreground list-disc space-y-1.5 pl-4 leading-relaxed'>
              <li>
                {t(
                  'Quote priority: Merchant real-time bids (merchant) / auto-synced quotes (auto) / manual locks (manual) override static model ratios directly at settlement.'
                )}
              </li>
              <li>
                {t(
                  'Live performance EMA: Each call measures actual TTFT and streaming TPS. Low success rates (<85%) automatically penalize rank score so fast, reliable providers win more volume.'
                )}
              </li>
              <li>
                {t(
                  'Retry protection: If a primary node fails before first token, failover resets base pricing cleanly before settling on a backup node—no stale quotes leak across attempts.'
                )}
              </li>
              <li>
                {t(
                  'Automation API: Providers can push live quotes via PUT /api/merchant/portal/quotes without manual review.'
                )}
              </li>
            </ul>
            <div className='flex flex-wrap gap-2 pt-1'>
              <Button
                variant='outline'
                size='sm'
                className='h-7 text-xs'
                render={
                  <Link
                    to={
                      userRole >= ROLE.MERCHANT ? '/merchant' : '/merchant/apply'
                    }
                  />
                }
              >
                {userRole >= ROLE.MERCHANT
                  ? t('Open Merchant Bidding Portal')
                  : t('Apply as a Compute Provider')}
              </Button>
            </div>
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  )
}
