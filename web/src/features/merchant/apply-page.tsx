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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  CheckCircle2,
  Clock,
  QrCode,
  ShieldCheck,
  Store,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

import { getMyMerchantApplication } from './api'
import { MerchantApplyForm } from './apply-form'

/**
 * 商家入驻申请页（/merchant/apply）。
 *
 * 与商家门户分开，任何已登录用户都可访问；已是商家时由路由层重定向到
 * `/merchant`。先完成商户资质与结算档案入驻，过审后即可在商家工作台按需配置渠道与分组。
 */
export function MerchantApplyPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const applicationQuery = useQuery({
    queryKey: ['merchant', 'application'],
    queryFn: getMyMerchantApplication,
  })
  const application = applicationQuery.data

  return (
    <div className='mx-auto w-full max-w-6xl space-y-6 px-4 py-8'>
      {/* Hero Header */}
      <div className='relative overflow-hidden rounded-2xl border bg-gradient-to-br from-slate-900 via-slate-800 to-slate-900 p-6 text-white shadow-sm dark:from-slate-950 dark:via-slate-900 dark:to-slate-950'>
        <div className='flex flex-col gap-4 md:flex-row md:items-center md:justify-between'>
          <div className='space-y-2'>
            <Badge className='border-white/20 bg-white/10 text-white hover:bg-white/15'>
              <Store className='mr-1.5 size-3.5' />
              {t('Merchant Partner Program')}
            </Badge>
            <h1 className='text-2xl font-bold tracking-tight sm:text-3xl'>
              {t('Become a Merchant')}
            </h1>
            <p className='max-w-2xl text-sm text-slate-300'>
              {t(
                'Verify your existing platform identity, lock your Shop Name, and bind your payout QR & account. Once approved, you enter the Merchant Portal to configure upstream channels and pricing groups.'
              )}
            </p>
          </div>
          <div className='grid shrink-0 grid-cols-3 gap-2 rounded-xl border border-white/15 bg-white/5 p-3 text-center text-xs'>
            <div className='space-y-1 px-2'>
              <div className='mx-auto flex size-7 items-center justify-center rounded-full bg-white/15 font-semibold'>
                1
              </div>
              <p className='font-medium'>{t('Submit Profile')}</p>
            </div>
            <div className='space-y-1 px-2'>
              <div className='mx-auto flex size-7 items-center justify-center rounded-full bg-white/15 font-semibold'>
                2
              </div>
              <p className='font-medium'>{t('Admin Review')}</p>
            </div>
            <div className='space-y-1 px-2'>
              <div className='mx-auto flex size-7 items-center justify-center rounded-full bg-white/15 font-semibold'>
                3
              </div>
              <p className='font-medium'>{t('Launch Channels')}</p>
            </div>
          </div>
        </div>
      </div>

      {/* Application Status Alerts */}
      {application?.status === 0 && (
        <Alert className='border-amber-500/40 bg-amber-500/10'>
          <Clock className='size-4 text-amber-600 dark:text-amber-400' />
          <AlertTitle className='text-amber-800 dark:text-amber-300'>
            {t('Pending Review')} ·{' '}
            {application.shop_name || application.site_name}
          </AlertTitle>
          <AlertDescription className='text-xs text-amber-700 dark:text-amber-300/90'>
            {t(
              'Your onboarding application has been received and is awaiting administrator review.'
            )}
          </AlertDescription>
        </Alert>
      )}
      {application?.status === 2 && (
        <Alert variant='destructive'>
          <AlertCircle className='size-4' />
          <AlertTitle>{t('Previous Application Rejected')}</AlertTitle>
          <AlertDescription className='text-xs'>
            {t('Review Note')}:{' '}
            {application.review_note || t('No note provided')}
          </AlertDescription>
        </Alert>
      )}

      {/* Main 2-column Workspace */}
      <div className='grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]'>
        <MerchantApplyForm
          onSubmitted={() => {
            queryClient.invalidateQueries({
              queryKey: ['merchant', 'application'],
            })
          }}
        />

        <aside className='space-y-4 lg:sticky lg:top-6 lg:self-start'>
          <Card>
            <CardHeader className='pb-3'>
              <CardTitle className='flex items-center gap-2 text-sm'>
                <ShieldCheck className='text-primary size-4' />
                {t('Onboarding Guidelines')}
              </CardTitle>
            </CardHeader>
            <CardContent className='text-muted-foreground space-y-3 text-xs leading-relaxed'>
              <div className='flex items-start gap-2.5'>
                <CheckCircle2 className='mt-0.5 size-3.5 shrink-0 text-emerald-500' />
                <div>
                  <p className='text-foreground font-medium'>
                    {t('Shop Name Governance')}
                  </p>
                  <p>
                    {t(
                      'Shop Name identifies you across the Merchant Portal header, Finance Center reconciliation, and group prefixes. Renaming after approval requires admin review.'
                    )}
                  </p>
                </div>
              </div>
              <div className='flex items-start gap-2.5'>
                <CheckCircle2 className='mt-0.5 size-3.5 shrink-0 text-emerald-500' />
                <div>
                  <p className='text-foreground font-medium'>
                    {t('Platform URL vs. API Base URL')}
                  </p>
                  <p>
                    {t(
                      'Your Platform Website URL is used strictly for qualification verification and display. Relay endpoints are configured separately per channel.'
                    )}
                  </p>
                </div>
              </div>
              <div className='flex items-start gap-2.5'>
                <QrCode className='text-primary mt-0.5 size-3.5 shrink-0' />
                <div>
                  <p className='text-foreground font-medium'>
                    {t('QR-Enabled Withdrawals')}
                  </p>
                  <p>
                    {t(
                      'Upload your Alipay, WeChat, or USDT receipt QR code once. Finance officers scan the exact snapshotted code when settling your payouts.'
                    )}
                  </p>
                </div>
              </div>
              <div className='flex items-start gap-2.5'>
                <Wallet className='mt-0.5 size-3.5 shrink-0 text-amber-500' />
                <div>
                  <p className='text-foreground font-medium'>
                    {t('Settlement & Split Ratio')}
                  </p>
                  <p>
                    {t(
                      'Default revenue share is 95% merchant / 5% platform fee with configurable T+N settlement hold.'
                    )}
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>
        </aside>
      </div>
    </div>
  )
}
