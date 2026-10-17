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

import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { getChannelTypeLabel } from '@/features/channels/lib/channel-utils'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { MerchantApplication, UpstreamProbe } from '../types'
import { upstreamKindLabelKey } from '../upstream-kinds'

interface UpstreamProbeDialogProps {
  /** 被探测的申请；为 null 时关闭弹窗。 */
  application: MerchantApplication | null
  probe: UpstreamProbe | null
  loading: boolean
  onOpenChange: (open: boolean) => void
}

/** 展示一次上游探测的结果：解析类型、渠道类型、同步能力与全部告警。 */
export function UpstreamProbeDialog(props: UpstreamProbeDialogProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const probe = props.probe
  const warnings = probe?.warnings ?? []
  const declaredKind = probe?.declared_kind ?? ''

  return (
    <Dialog open={!!props.application} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t('Upstream probe')}: {props.application?.site_name}
          </DialogTitle>
        </DialogHeader>
        {props.loading && (
          <LoadingState inline size='sm' message={t('Probing...')} />
        )}
        {probe && (
          <div className='space-y-3'>
            <dl className='grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm'>
              <dt className='text-muted-foreground'>{t('Resolved type')}</dt>
              <dd>{t(upstreamKindLabelKey(probe.kind))}</dd>
              {declaredKind !== '' && (
                <>
                  <dt className='text-muted-foreground'>
                    {t('Declared type')}
                  </dt>
                  <dd>{t(upstreamKindLabelKey(declaredKind))}</dd>
                </>
              )}
              <dt className='text-muted-foreground'>{t('Channel type')}</dt>
              <dd>{`${t(getChannelTypeLabel(probe.channel_type))} (${probe.channel_type})`}</dd>
              <dt className='text-muted-foreground'>
                {t('Price sync capable')}
              </dt>
              <dd>{probe.price_sync_capable ? t('Yes') : t('No')}</dd>
              <dt className='text-muted-foreground'>
                {t('New API pricing API')}
              </dt>
              <dd>{probe.new_api_pricing ? t('Yes') : t('No')}</dd>
              <dt className='text-muted-foreground'>{t('Models endpoint')}</dt>
              <dd>{probe.models_endpoint_ok ? t('Yes') : t('No')}</dd>
              <dt className='text-muted-foreground'>{t('Model count')}</dt>
              <dd>{formatNumber(probe.model_count, locale)}</dd>
            </dl>
            {warnings.length > 0 ? (
              <Alert variant='destructive'>
                <AlertTitle>{t('Warnings')}</AlertTitle>
                <AlertDescription>
                  <ul className='list-disc space-y-1 pl-4'>
                    {warnings.map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </AlertDescription>
              </Alert>
            ) : (
              <p className='text-muted-foreground text-xs'>
                {t('No warnings')}
              </p>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
