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
import { ArrowRight, HelpCircle, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'

import {
  buildDefaultCustomModelName,
  buildMerchantCustomPrefix,
  buildNormalizedPlatformModelMap,
  type CustomModelPriceDraft,
  findSuggestedPlatformModel,
  isMerchantCustomTarget,
  resolveModelMapping,
} from '../lib/model-mapping'

/** 对接列表超过这个行数才显示筛选框。 */
const MAPPING_FILTER_THRESHOLD = 8
/** 对接列表单次最多渲染的行数（配合筛选框使用）。 */
const MAPPING_RENDER_LIMIT = 100

/** 保留 6 位有效小数并去掉末尾多余 0。 */
function formatPricePreview(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  return Number(value.toFixed(6)).toString()
}

interface MerchantModelMappingEditorProps {
  upstreamModels: string[]
  platformModels: string[]
  /** 上游模型名 → 暴露模型名（平台模型或 `m{ID}/...` 自定义模型）；缺省按同名/近名自动对接，显式空串表示不上架。 */
  value: Record<string, string>
  onChange: (next: Record<string, string>) => void
  /** 可选：把某个上游模型从清单中移除（不上架且不再显示该行）。 */
  onRemove?: (upstream: string) => void
  /** 当前商家 user_id，用于生成并锁定 `m{ID}/...` 专属模型前缀。 */
  merchantUserId?: number
  /** 当前目标分组倍率，用于计算用户最终实付与“按实付反推基准价”。 */
  groupRatio?: number
  /** 自定义前缀模型的基准价草稿（key 为完整模型名 `m{ID}/alias`）。 */
  customPrices?: Record<string, CustomModelPriceDraft>
  onCustomPricesChange?: (next: Record<string, CustomModelPriceDraft>) => void
}

/**
 * 「上游模型名 ↔ 平台官方模型 / 专属自定义模型」三层对接编辑器：
 * 1) 同名及 `.`/`-`/大小写差异自动智能匹配平台模型；
 * 2) Searchable Combobox 手动映射到任意平台模型；
 * 3) 对平台尚未收录的模型，切换 `m{ID}/<别名>` 专属前缀独立定价上架，支持按用户实付反推基准价并展示温馨提示与查看详情。
 */
export function MerchantModelMappingEditor(
  props: MerchantModelMappingEditorProps
) {
  const { t } = useTranslation()
  const {
    upstreamModels,
    platformModels,
    value,
    onChange,
    onRemove,
    merchantUserId,
    groupRatio = 1,
    customPrices = {},
    onCustomPricesChange,
  } = props

  const [mappingFilter, setMappingFilter] = useState('')
  const [reverseTargetByModel, setReverseTargetByModel] = useState<
    Record<string, CustomModelPriceDraft>
  >({})
  const [showReverseByRow, setShowReverseByRow] = useState<
    Record<string, boolean>
  >({})
  const [showDetailByRow, setShowDetailByRow] = useState<
    Record<string, boolean>
  >({})

  const effectiveRatio =
    Number.isFinite(groupRatio) && groupRatio > 0 ? groupRatio : 1
  const customPrefix = buildMerchantCustomPrefix(merchantUserId)
  const canCreateCustom = Boolean(
    onCustomPricesChange && merchantUserId && merchantUserId > 0
  )

  const platformModelSet = useMemo(
    () => new Set(platformModels),
    [platformModels]
  )
  const normalizedMap = useMemo(
    () => buildNormalizedPlatformModelMap(platformModels),
    [platformModels]
  )
  const comboboxOptions = useMemo(
    () =>
      platformModels.map((name) => ({
        value: name,
        label: name,
      })),
    [platformModels]
  )

  const filteredRows = upstreamModels.filter((name) =>
    name.toLowerCase().includes(mappingFilter.trim().toLowerCase())
  )

  const duplicateOfficials = useMemo(() => {
    const seen = new Set<string>()
    const duplicates = new Set<string>()
    for (const upstream of upstreamModels) {
      const target = resolveModelMapping(
        upstream,
        value,
        platformModelSet,
        normalizedMap
      )
      if (!target) continue
      if (seen.has(target)) {
        duplicates.add(target)
      } else {
        seen.add(target)
      }
    }
    return duplicates
  }, [upstreamModels, value, platformModelSet, normalizedMap])

  const mappedCount = upstreamModels.filter((name) => {
    const resolved = resolveModelMapping(
      name,
      value,
      platformModelSet,
      normalizedMap
    )
    return resolved && !duplicateOfficials.has(resolved)
  }).length

  function switchToCustomMode(upstream: string) {
    if (!canCreateCustom) return
    const defaultCustom = buildDefaultCustomModelName(upstream, merchantUserId)
    onChange({ ...value, [upstream]: defaultCustom })
    if (onCustomPricesChange && !customPrices[defaultCustom]) {
      onCustomPricesChange({
        ...customPrices,
        [defaultCustom]: { input: '', output: '' },
      })
    }
  }

  function switchToPlatformMode(upstream: string) {
    const next = { ...value }
    delete next[upstream]
    onChange(next)
  }

  function renameCustomAlias(
    upstream: string,
    oldFullName: string,
    newSuffix: string
  ) {
    const cleanSuffix = newSuffix.replaceAll(/[,\s/]+/g, '')
    const nextFullName = `${customPrefix}${cleanSuffix}`
    onChange({ ...value, [upstream]: nextFullName })
    if (onCustomPricesChange && oldFullName !== nextFullName) {
      const prevDraft = customPrices[oldFullName]
      if (prevDraft && !customPrices[nextFullName]) {
        onCustomPricesChange({
          ...customPrices,
          [nextFullName]: prevDraft,
        })
      }
    }
  }

  function updateCustomPrice(
    modelName: string,
    field: 'input' | 'output',
    val: string
  ) {
    if (!onCustomPricesChange) return
    const existing = customPrices[modelName] ?? { input: '', output: '' }
    onCustomPricesChange({
      ...customPrices,
      [modelName]: {
        ...existing,
        [field]: val,
      },
    })
  }

  function applyReverseCalculation(modelName: string) {
    if (!onCustomPricesChange) return
    const wanted = reverseTargetByModel[modelName]
    if (!wanted) return
    const wantedIn = Number(wanted.input)
    const wantedOut = Number(wanted.output)
    const existing = customPrices[modelName] ?? { input: '', output: '' }
    onCustomPricesChange({
      ...customPrices,
      [modelName]: {
        input:
          Number.isFinite(wantedIn) && wantedIn > 0
            ? formatPricePreview(wantedIn / effectiveRatio)
            : existing.input,
        output:
          Number.isFinite(wantedOut) && wantedOut > 0
            ? formatPricePreview(wantedOut / effectiveRatio)
            : existing.output,
      },
    })
  }

  return (
    <div className='space-y-2'>
      <div className='flex items-center justify-between gap-2'>
        <p className='text-sm font-medium'>{t('Model mapping')}</p>
        <Badge variant='secondary'>
          {t('{{mapped}} of {{total}} mapped', {
            mapped: mappedCount,
            total: upstreamModels.length,
          })}
        </Badge>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(
          '优先自动匹配同名或仅点(.)/横杠(-)不同的平台模型；平台暂未收录的模型可点「专属前缀上架」使用 m{{id}}/ 前缀独立定价。',
          { id: merchantUserId || 0 }
        )}
      </p>
      {duplicateOfficials.size > 0 && (
        <p className='text-xs text-amber-600 dark:text-amber-400'>
          {t(
            'Duplicate mapping: {{model}} is mapped by multiple upstream models; only the first takes effect.',
            { model: [...duplicateOfficials].join(', ') }
          )}
        </p>
      )}
      {upstreamModels.length > MAPPING_FILTER_THRESHOLD && (
        <Input
          value={mappingFilter}
          onChange={(e) => setMappingFilter(e.target.value)}
          placeholder={t('Filter models')}
          className='h-8'
        />
      )}
      <div className='overflow-hidden rounded-md border'>
        <div className='text-muted-foreground bg-muted/40 grid grid-cols-[1.1fr_1.7fr_auto] items-center gap-2 px-2 py-1 text-xs'>
          <span>{t('Upstream model')}</span>
          <span className='flex items-center gap-1'>
            <ArrowRight aria-hidden className='size-3' />
            {t('暴露模型名（平台模型 / 专属自定义）')}
          </span>
          <span />
        </div>
        <div className='max-h-[420px] divide-y overflow-y-auto'>
          {filteredRows.slice(0, MAPPING_RENDER_LIMIT).map((upstream) => {
            const explicit = value[upstream]
            const resolved = resolveModelMapping(
              upstream,
              value,
              platformModelSet,
              normalizedMap
            )
            const isCustomMode =
              canCreateCustom && resolved.startsWith(customPrefix)
            const suggestion =
              explicit === undefined
                ? findSuggestedPlatformModel(
                    upstream,
                    platformModelSet,
                    normalizedMap
                  )
                : null
            const isSmartMatched = Boolean(suggestion?.matchedByNormalization)
            const duplicated = Boolean(resolved && duplicateOfficials.has(resolved))

            const customSuffix = isCustomMode
              ? resolved.slice(customPrefix.length)
              : ''
            const priceDraft = customPrices[resolved] ?? {
              input: '',
              output: '',
            }
            const baseIn = Number(priceDraft.input)
            const baseOut = Number(priceDraft.output)
            const hasValidBase =
              Number.isFinite(baseIn) &&
              baseIn > 0 &&
              Number.isFinite(baseOut) &&
              baseOut > 0
            const finalIn = hasValidBase ? baseIn * effectiveRatio : 0
            const finalOut = hasValidBase ? baseOut * effectiveRatio : 0
            const netIn = finalIn * 0.95
            const netOut = finalOut * 0.95
            const showReverse = Boolean(showReverseByRow[upstream])
            const showDetails = Boolean(showDetailByRow[upstream])
            const wantedDraft = reverseTargetByModel[resolved] ?? {
              input: '',
              output: '',
            }

            return (
              <div key={upstream} className='space-y-2 px-2 py-2'>
                <div className='grid grid-cols-[1.1fr_1.7fr_auto] items-center gap-2'>
                  <div className='min-w-0 space-y-0.5'>
                    <span
                      className='block truncate font-mono text-xs'
                      title={upstream}
                    >
                      {upstream}
                    </span>
                    {isSmartMatched && (
                      <Badge variant='secondary' className='h-4 px-1 text-[10px]'>
                        {t('点/横杠自动对齐')}
                      </Badge>
                    )}
                    {isMerchantCustomTarget(resolved, merchantUserId) && (
                      <Badge variant='outline' className='h-4 px-1 text-[10px]'>
                        {t('专属前缀')}
                      </Badge>
                    )}
                  </div>

                  <div className='flex min-w-0 items-center gap-1.5'>
                    {isCustomMode ? (
                      <div className='flex min-w-0 flex-1 items-center gap-1'>
                        <span className='bg-muted text-muted-foreground shrink-0 rounded border px-1.5 py-1 font-mono text-xs'>
                          {customPrefix}
                        </span>
                        <Input
                          value={customSuffix}
                          onChange={(e) =>
                            renameCustomAlias(upstream, resolved, e.target.value)
                          }
                          placeholder={t('输入自定义模型后缀')}
                          className='h-8 min-w-0 flex-1 font-mono text-xs'
                        />
                      </div>
                    ) : (
                      <Combobox
                        options={comboboxOptions}
                        value={resolved || null}
                        onValueChange={(next) => {
                          onChange({ ...value, [upstream]: next ?? '' })
                        }}
                        placeholder={t('Select a platform model')}
                        searchPlaceholder={t('Filter models')}
                        emptyText={t('No matching platform model')}
                        className='h-8 w-full'
                        aria-label={t('Platform model for {{model}}', {
                          model: upstream,
                        })}
                      />
                    )}
                    {duplicated && (
                      <Badge variant='destructive'>{t('Duplicate')}</Badge>
                    )}
                  </div>

                  <div className='flex items-center gap-1.5'>
                    {!resolved && !isCustomMode && (
                      <span className='text-muted-foreground whitespace-nowrap text-xs'>
                        {t('Skipped')}
                      </span>
                    )}
                    {canCreateCustom && (
                      <Button
                        type='button'
                        variant={isCustomMode ? 'secondary' : 'outline'}
                        size='sm'
                        className='h-7 px-2 text-[11px]'
                        onClick={() =>
                          isCustomMode
                            ? switchToPlatformMode(upstream)
                            : switchToCustomMode(upstream)
                        }
                      >
                        {isCustomMode ? t('改用平台模型') : t('专属前缀上架')}
                      </Button>
                    )}
                    {onRemove && (
                      <button
                        type='button'
                        aria-label={t('Remove from list')}
                        title={t('Remove from list')}
                        className='text-muted-foreground hover:text-destructive'
                        onClick={() => onRemove(upstream)}
                      >
                        <Trash2 aria-hidden className='size-3.5' />
                      </button>
                    )}
                  </div>
                </div>

                {isCustomMode && (
                  <div className='bg-muted/30 space-y-2 rounded-md border p-2 text-xs'>
                    <div className='grid grid-cols-1 gap-2 sm:grid-cols-2'>
                      <div className='space-y-1'>
                        <span className='text-muted-foreground block text-[11px]'>
                          {t('输入基准原价 ($/1M · 折前基准)')}
                        </span>
                        <Input
                          type='number'
                          step='any'
                          min='0'
                          value={priceDraft.input}
                          onChange={(e) =>
                            updateCustomPrice(resolved, 'input', e.target.value)
                          }
                          placeholder='例如 10'
                          className='h-7 font-mono text-xs'
                        />
                      </div>
                      <div className='space-y-1'>
                        <span className='text-muted-foreground block text-[11px]'>
                          {t('输出基准原价 ($/1M · 折前基准)')}
                        </span>
                        <Input
                          type='number'
                          step='any'
                          min='0'
                          value={priceDraft.output}
                          onChange={(e) =>
                            updateCustomPrice(resolved, 'output', e.target.value)
                          }
                          placeholder='例如 30'
                          className='h-7 font-mono text-xs'
                        />
                      </div>
                    </div>

                    <div className='flex flex-wrap items-center justify-between gap-2 text-[11px]'>
                      <button
                        type='button'
                        className='text-primary hover:underline'
                        onClick={() =>
                          setShowReverseByRow((prev) => ({
                            ...prev,
                            [upstream]: !prev[upstream],
                          }))
                        }
                      >
                        {showReverse
                          ? t('收起实付反推计算器')
                          : t('想直接定最终售价？按用户实付反推基准价')}
                      </button>
                      <button
                        type='button'
                        className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 hover:underline'
                        onClick={() =>
                          setShowDetailByRow((prev) => ({
                            ...prev,
                            [upstream]: !prev[upstream],
                          }))
                        }
                      >
                        <HelpCircle className='size-3' />
                        {t('查看详情')}
                      </button>
                    </div>

                    {showReverse && (
                      <div className='bg-background space-y-1.5 rounded border p-2'>
                        <p className='text-muted-foreground text-[11px]'>
                          {t(
                            '输入期望用户最终实付单价（按当前分组倍率 {{ratio}}x），系统自动反推折前基准原价：',
                            { ratio: formatPricePreview(effectiveRatio) }
                          )}
                        </p>
                        <div className='flex flex-wrap items-center gap-2'>
                          <Input
                            type='number'
                            step='any'
                            min='0'
                            value={wantedDraft.input}
                            onChange={(e) =>
                              setReverseTargetByModel((prev) => ({
                                ...prev,
                                [resolved]: {
                                  ...(prev[resolved] ?? {
                                    input: '',
                                    output: '',
                                  }),
                                  input: e.target.value,
                                },
                              }))
                            }
                            placeholder={t('期望用户实付输入 $/1M')}
                            className='h-7 min-w-[130px] flex-1 font-mono text-xs'
                          />
                          <Input
                            type='number'
                            step='any'
                            min='0'
                            value={wantedDraft.output}
                            onChange={(e) =>
                              setReverseTargetByModel((prev) => ({
                                ...prev,
                                [resolved]: {
                                  ...(prev[resolved] ?? {
                                    input: '',
                                    output: '',
                                  }),
                                  output: e.target.value,
                                },
                              }))
                            }
                            placeholder={t('期望用户实付输出 $/1M')}
                            className='h-7 min-w-[130px] flex-1 font-mono text-xs'
                          />
                          <Button
                            type='button'
                            size='sm'
                            variant='secondary'
                            className='h-7 text-[11px]'
                            onClick={() => applyReverseCalculation(resolved)}
                          >
                            {t('反推填入基准价')}
                          </Button>
                        </div>
                      </div>
                    )}

                    <div className='rounded border border-amber-500/30 bg-amber-500/5 px-2 py-1.5 text-[11px]'>
                      {hasValidBase ? (
                        <div className='space-y-0.5'>
                          <p className='font-medium text-amber-700 dark:text-amber-300'>
                            {t(
                              '温馨提示：用户最终实付 输入 ${{fin}} / 输出 ${{fout}}（每 1M tokens · 站内 1¥=1$ 即约 ¥{{fin}} / ¥{{fout}}）',
                              {
                                fin: formatPricePreview(finalIn),
                                fout: formatPricePreview(finalOut),
                              }
                            )}
                          </p>
                          <p className='text-muted-foreground'>
                            {t(
                              '商家预估实收（按默认 95% 分成）：输入 ${{netIn}} / 输出 ${{netOut}}',
                              {
                                netIn: formatPricePreview(netIn),
                                netOut: formatPricePreview(netOut),
                              }
                            )}
                          </p>
                        </div>
                      ) : (
                        <p className='text-muted-foreground'>
                          {t(
                            '温馨提示：填写输入与输出基准原价（或使用上方实付反推）后，将在此实时显示用户最终实付金额与您的预估收益。'
                          )}
                        </p>
                      )}
                    </div>

                    {showDetails && (
                      <div className='text-muted-foreground bg-background space-y-1 rounded border p-2 text-[11px]'>
                        <p className='text-foreground font-medium'>
                          {t('专属前缀模型计费详情')}
                        </p>
                        <p>
                          {t(
                            '1. 用户最终实付 = 基准原价 × 所属分组倍率（当前 {{ratio}}x）。若分组倍率调整，最终售价同步按比例变化。',
                            { ratio: formatPricePreview(effectiveRatio) }
                          )}
                        </p>
                        <p>
                          {t(
                            '2. 商家预估实收 = 用户最终实付 × 签约分成比例（默认 95%）。站内默认 1 人民币充值 = $1 额度。'
                          )}
                        </p>
                        <p>
                          {t(
                            '3. 定价仅作用于专属模型 {{model}}，与其他商家及平台模型完全隔离；不同分组可通过修改后缀（如 -v2）分别定价。',
                            { model: resolved }
                          )}
                        </p>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )
          })}
          {filteredRows.length > MAPPING_RENDER_LIMIT && (
            <p className='text-muted-foreground px-2 py-2 text-xs'>
              {t('Use the filter to see the remaining {{count}} models', {
                count: filteredRows.length - MAPPING_RENDER_LIMIT,
              })}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}
