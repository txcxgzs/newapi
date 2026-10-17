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
import { ArrowLeft } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { useMarketKline, useMarketOverview } from '../hooks/use-market-data'
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
  const [pool, setPool] = useState<string>('')
  const [picked, setPicked] = useState<{
    pool: string
    model: string
  } | null>(null)
  const [interval, setIntervalPeriod] = useState<CandleInterval>('1m')

  const { data: overview, isLoading } = useMarketOverview(pool || undefined)

  const stocks = useMemo(() => overview?.stocks ?? [], [overview])
  const pools = useMemo(() => overview?.pools ?? [], [overview])

  // 未手动选择时跟随成交额第一的模型，避免在副作用中回写 state
  const selected = picked ?? stocks[0] ?? null

  const { data: candles } = useMarketKline(
    selected?.pool ?? '',
    selected?.model ?? '',
    interval,
    200
  )

  const current = stocks.find(
    (s) => s.pool === selected?.pool && s.model === selected?.model
  )
  const up = (current?.change_24h_pct ?? 0) >= 0

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
      </div>

      <div className='grid gap-4 lg:grid-cols-[420px_1fr]'>
        {/* 股票列表 */}
        <div className='bg-card max-h-[560px] overflow-hidden rounded-xl border'>
          {isLoading ? (
            <div className='text-muted-foreground flex h-64 items-center justify-center'>
              {t('Loading...')}
            </div>
          ) : (
            <StockList
              stocks={stocks}
              selectedPool={selected?.pool ?? ''}
              selectedModel={selected?.model ?? ''}
              onSelect={(p, m) => setPicked({ pool: p, model: m })}
              className='max-h-[560px]'
            />
          )}
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
                  {t('24h High')}: {formatMarketPrice(current.high_24h, locale)}
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
    </div>
  )
}
