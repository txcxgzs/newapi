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
import { ArrowRight, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'

import { resolveModelMapping } from '../lib/model-mapping'

/** 对接列表超过这个行数才显示筛选框。 */
const MAPPING_FILTER_THRESHOLD = 8
/** 对接列表单次最多渲染的行数（配合筛选框使用）。 */
const MAPPING_RENDER_LIMIT = 100

interface MerchantModelMappingEditorProps {
  upstreamModels: string[]
  platformModels: string[]
  /** 上游模型名 → 平台官方模型名；缺省条目按同名自动对接，显式空串表示不上架。 */
  value: Record<string, string>
  onChange: (next: Record<string, string>) => void
  /** 可选：把某个上游模型从清单中移除（不上架且不再显示该行）。 */
  onRemove?: (upstream: string) => void
}

/**
 * 「上游模型名 ↔ 平台官方模型名」对接编辑器。
 * 入驻/扩容表单与渠道编辑弹窗共用：同名模型自动对接，未对接或移除的不上架。
 */
export function MerchantModelMappingEditor(
  props: MerchantModelMappingEditorProps
) {
  const { t } = useTranslation()
  const { upstreamModels, platformModels, value, onChange, onRemove } = props
  const [mappingFilter, setMappingFilter] = useState('')

  const platformModelSet = useMemo(
    () => new Set(platformModels),
    [platformModels]
  )

  const filteredRows = upstreamModels.filter((name) =>
    name.toLowerCase().includes(mappingFilter.trim().toLowerCase())
  )

  const duplicateOfficials = useMemo(() => {
    const seen = new Set<string>()
    const duplicates = new Set<string>()
    for (const upstream of upstreamModels) {
      const official = resolveModelMapping(upstream, value, platformModelSet)
      if (!official) continue
      if (seen.has(official)) {
        duplicates.add(official)
      } else {
        seen.add(official)
      }
    }
    return duplicates
  }, [upstreamModels, value, platformModelSet])

  const mappedCount = upstreamModels.filter(
    (name) =>
      resolveModelMapping(name, value, platformModelSet) &&
      !duplicateOfficials.has(resolveModelMapping(name, value, platformModelSet))
  ).length

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
          'Map each upstream model to the platform model it serves; unmapped models stay off the shelf. Same-name models are mapped automatically.'
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
        <div className='text-muted-foreground grid grid-cols-[1.2fr_1.6fr_auto] items-center gap-2 bg-muted/40 px-2 py-1 text-xs'>
          <span>{t('Upstream model')}</span>
          <span className='flex items-center gap-1'>
            <ArrowRight aria-hidden className='size-3' />
            {t('Platform model')}
          </span>
          <span />
        </div>
        <div className='max-h-64 divide-y overflow-y-auto'>
          {filteredRows.slice(0, MAPPING_RENDER_LIMIT).map((upstream) => {
            const official = resolveModelMapping(upstream, value, platformModelSet)
            const duplicated = duplicateOfficials.has(official)
            return (
              <div
                key={upstream}
                className='grid grid-cols-[1.2fr_1.6fr_auto] items-center gap-2 px-2 py-1.5'
              >
                <span className='truncate font-mono text-xs' title={upstream}>
                  {upstream}
                </span>
                <div className='flex items-center gap-1.5'>
                  <Combobox
                    options={platformModels.map((name) => ({
                      value: name,
                      label: name,
                    }))}
                    value={official || null}
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
                  {duplicated && (
                    <Badge variant='destructive'>{t('Duplicate')}</Badge>
                  )}
                </div>
                <div className='flex items-center gap-1.5'>
                  {!official && (
                    <span className='text-muted-foreground whitespace-nowrap text-xs'>
                      {t('Skipped')}
                    </span>
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
