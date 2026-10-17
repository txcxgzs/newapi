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
import { useTranslation } from 'react-i18next'

import { getMyMerchantApplication } from './api'
import { MerchantApplyForm } from './apply-form'

/**
 * 商家入驻申请页（/merchant/apply）。
 *
 * 与商家门户分开，任何已登录用户都可访问；已是商家时由路由层重定向到
 * `/merchant`。这里保留申请状态提示，让被拒的申请人仍能看到审核备注。
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
    <div className='mx-auto w-full max-w-3xl px-4 py-8'>
      <h1 className='mb-4 text-2xl font-bold'>{t('Become a Merchant')}</h1>
      <p className='text-muted-foreground mb-6'>
        {t(
          'Submit your site, API key and model quotes. After approval, your channel joins the pools and starts competing.'
        )}
      </p>
      {application?.status === 0 && (
        <p className='text-muted-foreground mb-4 text-sm'>
          {t('Pending Review')}
        </p>
      )}
      {application?.status === 2 && application.review_note && (
        <p className='text-destructive mb-4 text-sm'>
          {t('Review Note')}: {application.review_note}
        </p>
      )}
      <MerchantApplyForm
        onSubmitted={() => {
          queryClient.invalidateQueries({
            queryKey: ['merchant', 'application'],
          })
        }}
      />
    </div>
  )
}
