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
import { VChart } from '@visactor/react-vchart'
import { registerCandlestickChart } from '@visactor/vchart-extension'
import { Component, useMemo, type ComponentProps, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'

import { formatMarketPrice } from '../lib/format'
import type { MarketCandle } from '../types'

// candlestick 属于扩展图表，需要显式注册（同时注册蜡烛图 series 与 tooltip 处理器）
registerCandlestickChart()

// 中国市场惯例：红涨绿跌
export const UP_COLOR = '#ef4444'
export const DOWN_COLOR = '#22c55e'

// 主图叠加的均线周期与配色（琥珀 / 蓝 / 紫，彼此区分度足够）
const MA_WINDOWS = [5, 10, 20] as const
const MA_COLORS: Record<(typeof MA_WINDOWS)[number], string> = {
  5: '#f59e0b',
  10: '#3b82f6',
  20: '#a855f7',
}

// 十字光标 / 均线之外的辅助线统一用弱化灰，避免抢走 K 线主体的视觉焦点
const GUIDE_COLOR = 'rgba(148, 163, 184, 0.65)'

// 图表内单条数据：桶时间戳 + OHLC + 成交量，供 tooltip 回调取值。
//
// X 轴必须以**桶时间戳**（每根唯一）作为字段，而不是格式化后的短标签：
// 1h 周期的短标签只到"日"，24 根会共用同一个类目，被 band 轴折叠成一根巨柱。
// 显示用的可读标签统一交给 axis 的 formatMethod。
interface MarketChartDatum {
  ts: number
  open: number
  high: number
  low: number
  close: number
  volume: number
}

function formatBucketLabel(ts: number, interval: string): string {
  const d = new Date(ts * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  const day = `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  if (interval === '1h') return `${day} ${pad(d.getHours())}:00`
  return `${day} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// 简单移动均线：窗口不足的前缀补 null，折线自然地从有值处开始
function calcMA(closes: number[], window: number): (number | null)[] {
  const result: (number | null)[] = []
  let sum = 0
  for (let i = 0; i < closes.length; i++) {
    sum += closes[i]
    if (i >= window) sum -= closes[i - window]
    result.push(i >= window - 1 ? sum / window : null)
  }
  return result
}

// react-vchart 在图表实例异常时可能抛出渲染错误，边界兜底避免炸掉整页；
// resetKey 变化（新数据到达）时整体重挂载重试。
interface ChartBoundaryState {
  hasError: boolean
  resetKey: string
}

interface ChartBoundaryProps {
  resetKey: string
  fallback: ReactNode
  children: ReactNode
}

export class MarketChartErrorBoundary extends Component<
  ChartBoundaryProps,
  ChartBoundaryState
> {
  state: ChartBoundaryState = { hasError: false, resetKey: this.props.resetKey }

  static getDerivedStateFromProps(
    props: { resetKey: string },
    state: ChartBoundaryState
  ) {
    if (props.resetKey !== state.resetKey) {
      return { hasError: false, resetKey: props.resetKey }
    }
    return null
  }

  componentDidCatch() {
    this.setState({ hasError: true })
  }

  render() {
    if (this.state.hasError) {
      return this.props.fallback
    }
    return this.props.children
  }
}

interface MarketKlineChartProps {
  candles: MarketCandle[]
  interval: string
  className?: string
}

export function MarketKlineChart({
  candles,
  interval,
  className,
}: MarketKlineChartProps) {
  const { t, i18n } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  const spec = useMemo(() => {
    if (!candles.length) return null

    const rows: MarketChartDatum[] = candles.map((c) => ({
      ts: c.bucket_ts,
      open: c.open,
      high: c.high,
      low: c.low,
      close: c.close,
      // 成交量口径用成交笔数：不受模型单价量级影响，柱高更稳定、更接近看盘的“手数”
      volume: c.volume_calls,
    }))

    const closes = candles.map((c) => c.close)
    const last = rows.at(-1)
    const lastClose = last?.close ?? 0
    // 最新价参考线跟随最后一根 K 线的涨跌配色
    const lastColor = lastClose >= (last?.open ?? 0) ? UP_COLOR : DOWN_COLOR

    // 均线各算一次，避免逐行重复计算
    const maValues = MA_WINDOWS.map((w) => calcMA(closes, w))

    // 主图（region-k）放蜡烛图与均线；副图（region-volume）放成交量柱，共用同一时间轴。
    // 各 series 内联自己的 data，规避多 region 下顶层 data + dataIndex 的兼容坑。
    const series = [
      {
        id: 'candlestick',
        type: 'candlestick',
        regionId: 'region-k',
        data: { values: rows },
        xField: 'ts',
        yField: ['open', 'close', 'high', 'low'],
        openField: 'open',
        closeField: 'close',
        highField: 'high',
        lowField: 'low',
        rising: { style: { boxFill: UP_COLOR, stroke: UP_COLOR } },
        falling: { style: { boxFill: DOWN_COLOR, stroke: DOWN_COLOR } },
        tooltip: {
          // 悬浮时只展示一根 K 线的关键盘口信息，价格沿用页面的 formatMarketPrice
          dimension: {
            // X 轴字段是原始时间戳，标题需要转成可读时间，否则会显示一串数字
            title: {
              value: (d: MarketChartDatum | undefined) =>
                d ? formatBucketLabel(d.ts, interval) : '',
            },
            content: [
              {
                key: t('Open'),
                value: (d: MarketChartDatum | undefined) =>
                  formatMarketPrice(d?.open, locale),
              },
              {
                key: t('High'),
                value: (d: MarketChartDatum | undefined) =>
                  formatMarketPrice(d?.high, locale),
              },
              {
                key: t('Low'),
                value: (d: MarketChartDatum | undefined) =>
                  formatMarketPrice(d?.low, locale),
              },
              {
                key: t('Close'),
                value: (d: MarketChartDatum | undefined) =>
                  formatMarketPrice(d?.close, locale),
              },
              {
                key: t('Volume'),
                value: (d: MarketChartDatum | undefined) =>
                  formatNumber(d?.volume, locale),
              },
            ],
          },
        },
      },
      ...MA_WINDOWS.map((w, wi) => ({
        id: `ma${w}`,
        type: 'line',
        regionId: 'region-k',
        data: {
          values: rows.map((row, i) => ({
            ts: row.ts,
            ma: maValues[wi][i],
          })),
        },
        xField: 'ts',
        yField: 'ma',
        line: { style: { stroke: MA_COLORS[w], lineWidth: 1.5 } },
        point: { visible: false },
        // 均线值已在悬浮提示外单独用图例展示，这里关闭 tooltip 以免信息冗余
        tooltip: { visible: false },
      })),
      {
        id: 'volume',
        type: 'bar',
        regionId: 'region-volume',
        data: { values: rows },
        xField: 'ts',
        yField: 'volume',
        bar: {
          style: {
            // 柱色跟随当根 K 线涨跌，与主图形成呼应
            fill: (d: MarketChartDatum) =>
              d.close >= d.open ? UP_COLOR : DOWN_COLOR,
          },
        },
        tooltip: { visible: false },
      },
    ]

    return {
      type: 'common',
      // 上下两个 region：上方 K 线（含均线），下方成交量；共用一根时间轴
      region: [{ id: 'region-k', height: 0.72 }, { id: 'region-volume' }],
      layout: {
        type: 'grid',
        col: 2,
        row: 3,
        elements: [
          { modelId: 'region-k', col: 1, row: 0 },
          { modelId: 'region-volume', col: 1, row: 2 },
          { modelId: 'axis-x', col: 1, row: 1 },
          { modelId: 'axis-y-k', col: 0, row: 0 },
          { modelId: 'axis-y-volume', col: 0, row: 2 },
        ],
      },
      // 顶部留白避免最高价的蜡烛贴边，右侧留白给“最新价”标签让位
      padding: { top: 10, right: 8 },
      series,
      axes: [
        {
          id: 'axis-y-k',
          regionId: 'region-k',
          orient: 'left',
          // 价格轴必须关闭"从 0 起算"：模型单价动辄十几美元，从 0 起算会把
          // 整段行情压成一条贴着顶部的细线，看不出任何波动。
          zero: false,
          label: {
            // 价格量级跨度极大（1e-6 ~ 1e3），刻度统一走 formatMarketPrice 保证精度
            formatMethod: (value: number) => formatMarketPrice(value, locale),
            style: { fontSize: 10 },
          },
        },
        {
          id: 'axis-y-volume',
          regionId: 'region-volume',
          orient: 'left',
          // 成交量从 0 起算才符合看盘直觉
          zero: true,
          label: {
            formatMethod: (value: number) => formatNumber(value, locale),
            style: { fontSize: 10 },
          },
        },
        {
          id: 'axis-x',
          regionId: ['region-volume', 'region-k'],
          orient: 'bottom',
          // xField 是原始时间戳（保证每根唯一），刻度文案在这里格式化；
          // minGap 控制标签最小间距，小屏下会自动抽样隐藏，避免时间刻度挤成一团
          label: {
            formatMethod: (value: number) => formatBucketLabel(value, interval),
            minGap: 10,
            style: { fontSize: 10 },
          },
        },
      ],
      // 十字光标 + 最新价水平虚线，贴近看盘软件的操作习惯
      crosshair: {
        trigger: 'hover',
        xField: {
          visible: true,
          line: {
            type: 'line',
            style: { stroke: GUIDE_COLOR, lineDash: [4, 4], lineWidth: 1 },
          },
          label: { visible: true },
        },
        yField: {
          visible: true,
          line: {
            type: 'line',
            style: { stroke: GUIDE_COLOR, lineDash: [4, 4], lineWidth: 1 },
          },
          label: {
            visible: true,
            formatMethod: (value: string | number) =>
              formatMarketPrice(Number(value), locale),
          },
        },
      },
      markLine: [
        {
          y: lastClose,
          // 关联蜡烛图 series，使 y 值落在主图的价格轴上
          relativeSeriesIndex: 0,
          line: {
            style: { stroke: lastColor, lineDash: [6, 4], lineWidth: 1 },
          },
          label: {
            visible: true,
            position: 'end',
            text: formatMarketPrice(lastClose, locale),
            labelBackground: {
              visible: true,
              style: { fill: lastColor, cornerRadius: 2 },
            },
            style: { fill: '#ffffff', fontSize: 10 },
          },
        },
      ],
      tooltip: { activeType: 'dimension' },
      background: 'transparent',
    }
  }, [candles, interval, locale, t])

  if (!candles.length) {
    return (
      <div
        className={
          className ??
          'text-muted-foreground flex h-80 items-center justify-center'
        }
      >
        {t('No transaction data yet')}
      </div>
    )
  }

  const lastTs = candles.at(-1)?.bucket_ts ?? 0

  return (
    <div className='w-full'>
      {/* 多条独立 line series 无法直接用图表内 legend 列出，这里用轻量 HTML 图例标注均线颜色 */}
      <div className='mb-1 flex flex-wrap items-center justify-end gap-x-3 gap-y-1 text-[11px]'>
        {MA_WINDOWS.map((w) => (
          <span
            key={`ma-legend-${w}`}
            className='text-muted-foreground inline-flex items-center gap-1'
          >
            <span
              className='inline-block h-0.5 w-4 rounded-full'
              style={{ backgroundColor: MA_COLORS[w] }}
            />
            {t(`MA${w}`)}
          </span>
        ))}
      </div>
      <div
        className={className ?? 'h-[360px] w-full sm:h-[440px]'}
        role='img'
        aria-label={t(
          'K-line candlestick chart ({{interval}}), latest close {{close}}',
          {
            interval,
            close: formatMarketPrice(candles.at(-1)?.close, locale),
          }
        )}
      >
        {themeReady && spec && (
          <MarketChartErrorBoundary
            resetKey={`${resolvedTheme}-${interval}-${lastTs}-${candles.length}`}
            fallback={
              <div className='text-muted-foreground flex h-80 items-center justify-center'>
                {t('Chart failed to render')}
              </div>
            }
          >
            <VChart
              key={`kline-${resolvedTheme}-${interval}-${lastTs}`}
              spec={
                {
                  ...spec,
                  theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                  background: 'transparent',
                } as unknown as ComponentProps<typeof VChart>['spec']
              }
              option={VCHART_OPTION}
            />
          </MarketChartErrorBoundary>
        )}
      </div>
    </div>
  )
}
