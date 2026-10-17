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
import { useTranslation } from 'react-i18next'

import { useMarketOverview } from '@/features/market/hooks/use-market-data'
import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import { formatMarketPrice, formatMarketRatio } from '../lib/format'

// 首页实时行情条：展示成交额靠前的模型，点击进入行情大屏
export function MarketTicker() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const { data: overview } = useMarketOverview(undefined, 15000)
  const stocks = (overview?.stocks ?? []).slice(0, 12)

  if (!stocks.length) return null

  return (
    <div className='border-y border-transparent bg-transparent [mask-image:linear-gradient(to_right,transparent,black_6%,black_94%,transparent)]'>
      <div className='mx-auto flex max-w-7xl items-center gap-3 overflow-x-auto px-4 py-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden'>
        <Link
          to='/market'
          className='text-muted-foreground flex shrink-0 items-center gap-1.5 text-xs font-medium whitespace-nowrap hover:underline'
        >
          <span className='relative flex size-1.5'>
            <span className='absolute inline-flex h-full w-full animate-ping rounded-full bg-violet-400 opacity-75' />
            <span className='relative inline-flex size-1.5 rounded-full bg-violet-500 dark:bg-violet-400' />
          </span>
          {t('Live Market')}
        </Link>
        <div className='flex items-center gap-5'>
          {stocks.map((s) => {
            const up = s.change_24h_pct >= 0
            return (
              <Link
                key={`${s.pool}|${s.model}`}
                to='/market'
                className='text-muted-foreground/90 flex shrink-0 items-baseline gap-1.5 text-xs'
              >
                <span className='text-muted-foreground/80 font-mono'>
                  {s.model}
                </span>
                <span className='text-muted-foreground font-mono'>
                  {formatMarketPrice(s.last_price, locale)}
                </span>
                <span
                  className={cn(
                    'font-mono',
                    up ? 'text-red-500/90' : 'text-emerald-500/90'
                  )}
                >
                  {up ? '+' : ''}
                  {s.change_24h_pct.toFixed(2)}%
                </span>
                {/* 商家挂牌倍率：与成交价互为独立的挂单口径 */}
                <span className='text-muted-foreground font-mono'>
                  {t('Listed')} {formatMarketRatio(s.listed_ratio)}
                </span>
              </Link>
            )
          })}
        </div>
      </div>
    </div>
  )
}
