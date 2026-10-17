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
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'
import { cn } from '@/lib/utils'

import {
  formatMarketPrice,
  formatMarketRatio,
  formatMarketVolume,
} from '../lib/format'
import type { MarketStock } from '../types'

interface StockListProps {
  stocks: MarketStock[]
  selectedPool: string
  selectedModel: string
  onSelect: (pool: string, model: string) => void
  className?: string
}

export function StockList({
  stocks,
  selectedPool,
  selectedModel,
  onSelect,
  className,
}: StockListProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  if (!stocks.length) {
    return (
      <div
        className={
          className ??
          'text-muted-foreground flex h-64 items-center justify-center'
        }
      >
        {t('No transaction data yet')}
      </div>
    )
  }
  return (
    <div className={cn('overflow-y-auto', className)}>
      <div className='text-muted-foreground grid grid-cols-[1fr_auto_auto_auto] gap-x-3 border-b px-3 py-2 text-xs'>
        <span>{t('Model')}</span>
        <span className='text-right'>{t('Last Price')}</span>
        <span className='w-16 text-right'>{t('Listed Ratio')}</span>
        <span className='w-16 text-right'>{t('24h Change')}</span>
      </div>
      <div className='divide-y'>
        {stocks.map((s) => {
          const up = s.change_24h_pct >= 0
          const selected = s.pool === selectedPool && s.model === selectedModel
          return (
            <button
              key={`${s.pool}|${s.model}`}
              type='button'
              aria-pressed={selected}
              onClick={() => onSelect(s.pool, s.model)}
              className={cn(
                'grid w-full grid-cols-[1fr_auto_auto_auto] items-center gap-x-3 px-3 py-2 text-left transition-colors hover:bg-muted/60',
                selected && 'bg-muted'
              )}
            >
              <span className='min-w-0'>
                <span className='block truncate font-mono text-sm font-medium'>
                  {s.model}
                </span>
                <span className='text-muted-foreground block truncate text-xs'>
                  {s.pool} · {formatMarketVolume(s.vol_quota_24h, locale)}
                </span>
              </span>
              <span className='text-right font-mono text-sm'>
                {formatMarketPrice(s.last_price, locale)}
              </span>
              <span className='text-muted-foreground w-16 text-right font-mono text-xs'>
                {formatMarketRatio(s.listed_ratio)}
              </span>
              <span
                className={cn(
                  'w-16 text-right font-mono text-sm',
                  up ? 'text-red-500' : 'text-emerald-500'
                )}
              >
                {up ? '+' : ''}
                {s.change_24h_pct.toFixed(2)}%
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
