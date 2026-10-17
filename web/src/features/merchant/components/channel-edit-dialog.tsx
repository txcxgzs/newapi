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
import { RefreshCw } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/handle-server-error'

import {
  getPlatformModels,
  probeMerchantChannel,
  updateMerchantChannel,
} from '../api'
import type { MerchantModelQuote, MerchantPoolChannel } from '../types'
import { buildModelQuotesFromMapping } from '../lib/model-mapping'
import { MerchantModelMappingEditor } from './model-mapping-editor'

/** 渠道名里平台加的前缀；编辑时商家看到的应是自己的站点名。 */
const MERCHANT_NAME_PREFIX = '商家:'

interface ChannelEditDialogProps {
  /** 要编辑的渠道（父组件按渠道挂载本弹窗）。 */
  channel: MerchantPoolChannel
  onClose: () => void
  onSaved: () => void
}

/** 从渠道当前信息推导表单初值与对接行（含已保存的改名映射）。 */
function initialStateFromChannel(channel: MerchantPoolChannel) {
  // 后端保存的是 官方名 → 上游名；行名 = 上游名（未改名即官方名）
  let savedMapping: Record<string, string> = {}
  try {
    if (channel.model_mapping) {
      savedMapping = JSON.parse(channel.model_mapping) as Record<string, string>
    }
  } catch {
    savedMapping = {}
  }
  const rows: string[] = []
  const seed: Record<string, string> = {}
  const rowsSeen = new Set<string>()
  for (const official of channel.models) {
    const upstream = savedMapping[official] ?? official
    // 撞键（该上游名已被另一条显式对接占用）时保留显式映射行
    if (rowsSeen.has(upstream)) continue
    rowsSeen.add(upstream)
    rows.push(upstream)
    seed[upstream] = official
  }
  return {
    siteName: channel.name.startsWith(MERCHANT_NAME_PREFIX)
      ? channel.name.slice(MERCHANT_NAME_PREFIX.length)
      : channel.name,
    siteUrl: channel.base_url ?? '',
    rows,
    seed,
  }
}

/**
 * 编辑商家自己的渠道：站点名称、站址、密钥与模型对接。
 *
 * 站址/密钥留空表示不修改；模型对接默认展示当前上架清单，点「检测」用
 * 已保存或新填的凭据拉取上游完整清单后重新对接。改站址/密钥时后端会
 * 先校验上游再落库；模型对接变更立即重建派生盘口。
 */
export function ChannelEditDialog(props: ChannelEditDialogProps) {
  const { t } = useTranslation()
  const { channel } = props
  const initial = useMemo(
    () => initialStateFromChannel(channel),
    [channel]
  )

  const [siteName, setSiteName] = useState(initial.siteName)
  const [siteUrl, setSiteUrl] = useState(initial.siteUrl)
  const [siteKey, setSiteKey] = useState('')
  const [probing, setProbing] = useState(false)
  const [probeResult, setProbeResult] = useState<{
    price_fetch_ok: boolean
    price_fetch_message?: string
    warnings?: string[]
  } | null>(null)
  // 模型对接行：初值 = 渠道当前上架清单；探测后切换为上游完整清单
  const [upstreamModels, setUpstreamModels] = useState(initial.rows)
  const [mapping, setMapping] = useState<Record<string, string>>(initial.seed)
  const [modelsDirty, setModelsDirty] = useState(false)
  const [saving, setSaving] = useState(false)

  const platformModelsQuery = useQuery({
    queryKey: ['merchant', 'platform-models'],
    queryFn: getPlatformModels,
    staleTime: 5 * 60_000,
  })
  const platformModels = useMemo(
    () => platformModelsQuery.data ?? [],
    [platformModelsQuery.data]
  )

  const nameChanged =
    siteName.trim() !== '' &&
    (MERCHANT_NAME_PREFIX + siteName.trim()) !== channel.name
  const urlChanged =
    siteUrl.trim() !== '' &&
    siteUrl.trim().replace(/\/+$/, '') !==
      (channel.base_url ?? '').replace(/\/+$/, '')
  const keyChanged = siteKey.trim() !== ''
  const changed = nameChanged || urlChanged || keyChanged || modelsDirty

  const runProbe = async () => {
    setProbing(true)
    setProbeResult(null)
    try {
      const result = await probeMerchantChannel({
        channel_id: channel.channel_id,
        site_url: urlChanged ? siteUrl.trim() : undefined,
        site_key: keyChanged ? siteKey.trim() : undefined,
      })
      setProbeResult(result)
      const models = [...(result.models ?? [])].sort((a, b) =>
        a.localeCompare(b)
      )
      if (models.length > 0) {
        // 切换为上游完整清单：已保存的改名映射（官方名→上游名）反解后保留，
        // 其余行同名自动对接
        let inverse: Record<string, string> = {}
        try {
          if (channel.model_mapping) {
            const saved = JSON.parse(
              channel.model_mapping
            ) as Record<string, string>
            for (const [official, upstream] of Object.entries(saved)) {
              inverse[upstream] = official
            }
          }
        } catch {
          inverse = {}
        }
        const seed: Record<string, string> = {}
        for (const upstream of models) {
          if (inverse[upstream]) seed[upstream] = inverse[upstream]
        }
        setUpstreamModels(models)
        setMapping(seed)
        setModelsDirty(true)
      }
    } catch (error) {
      handleServerError(error, t('Probe failed, please check the site URL'))
    } finally {
      setProbing(false)
    }
  }

  const save = async () => {
    setSaving(true)
    try {
      const body: {
        channel_id: number
        site_name?: string
        site_url?: string
        site_key?: string
        models?: MerchantModelQuote[]
      } = { channel_id: channel.channel_id }
      if (nameChanged) body.site_name = siteName.trim()
      if (urlChanged) body.site_url = siteUrl.trim()
      if (keyChanged) body.site_key = siteKey.trim()
      if (modelsDirty) {
        body.models = buildModelQuotesFromMapping(
          upstreamModels,
          mapping,
          platformModels
        )
      }
      await updateMerchantChannel(body)
      toast.success(t('Channel updated'))
      props.onSaved()
      props.onClose()
    } catch (error) {
      handleServerError(error, t('Failed to update channel'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && props.onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t('Edit Channel')}
            {': '}
            {channel.name.replace(MERCHANT_NAME_PREFIX, '')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Edits take effect immediately; site URL or key changes are verified against your upstream before saving.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='max-h-[70vh] space-y-4 overflow-y-auto pr-1'>
          <div className='space-y-1.5'>
            <Label htmlFor='merchant-edit-name'>{t('Site Name')}</Label>
            <Input
              id='merchant-edit-name'
              value={siteName}
              onChange={(e) => setSiteName(e.target.value)}
              placeholder={t('MyRelay')}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='merchant-edit-url'>{t('Site URL')}</Label>
            <Input
              id='merchant-edit-url'
              value={siteUrl}
              onChange={(e) => setSiteUrl(e.target.value)}
              placeholder={t('https://api.example.com')}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='merchant-edit-key'>{t('API Key')}</Label>
            <Input
              id='merchant-edit-key'
              type='password'
              value={siteKey}
              onChange={(e) => setSiteKey(e.target.value)}
              placeholder={t('Leave blank to keep the current key')}
            />
          </div>

          <div className='space-y-2 rounded-lg border border-dashed p-3'>
            <div className='flex items-center justify-between gap-2'>
              <p className='text-sm font-medium'>{t('Model mapping')}</p>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={runProbe}
                disabled={probing}
              >
                <RefreshCw
                  className={`mr-1.5 size-3.5 ${probing ? 'animate-spin' : ''}`}
                  aria-hidden
                />
                {probing
                  ? t('Detecting...')
                  : t('Probe upstream & pull models')}
              </Button>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Edits apply to the current listing; probing pulls the full upstream model list using the saved or newly entered credentials.'
              )}
            </p>
            {probeResult && (probeResult.warnings?.length ?? 0) > 0 && (
              <div className='space-y-0.5 rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-600 dark:text-amber-400'>
                {probeResult.warnings?.map((warning) => (
                  <p key={warning}>{warning}</p>
                ))}
              </div>
            )}
            {probeResult && !probeResult.price_fetch_ok && (
              <Alert variant='destructive'>
                <AlertTitle>{t('Ratio auto-fetch unavailable')}</AlertTitle>
                <AlertDescription className='space-y-1'>
                  {probeResult.price_fetch_message && (
                    <p>{probeResult.price_fetch_message}</p>
                  )}
                  {probeResult.warnings?.map((warning) => (
                    <p key={warning}>{warning}</p>
                  ))}
                </AlertDescription>
              </Alert>
            )}
            {upstreamModels.length > 0 && (
              <MerchantModelMappingEditor
                upstreamModels={upstreamModels}
                platformModels={platformModels}
                value={mapping}
                onChange={(next) => {
                  setMapping(next)
                  setModelsDirty(true)
                }}
                onRemove={(upstream) => {
                  setUpstreamModels((prev) => prev.filter((n) => n !== upstream))
                  setMapping((prev) => {
                    const next = { ...prev }
                    delete next[upstream]
                    return next
                  })
                  setModelsDirty(true)
                }}
              />
            )}
          </div>

          <Button onClick={save} disabled={saving || !changed}>
            {t('Save changes')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
