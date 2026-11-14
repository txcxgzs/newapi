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
import { Landmark, QrCode, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getFinanceOverview } from './api'
import { FinanceLedgerReconcileTab } from './components/ledger-reconcile-tab'
import { FinanceOverviewTab } from './components/overview-tab'
import { FinanceSettlementsTab } from './components/settlements-tab'
import { FinanceSubscriptionsRedemptionsTab } from './components/subscriptions-redemptions-tab'
import { FinanceTopUpsTab } from './components/topups-tab'
import { FinanceWithdrawalsTab } from './components/withdrawals-tab'

export function FinanceCenter() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [tab, setTab] = useState('overview')
  const authUser = useAuthStore((state) => state.auth.user)
  const isFinanceRole = authUser?.role === ROLE.FINANCE

  const overviewBadgeQuery = useQuery({
    queryKey: ['finance', 'overview-badge'],
    queryFn: () => getFinanceOverview(),
    staleTime: 30_000,
  })
  const pendingWithdrawals =
    overviewBadgeQuery.data?.merchant_stats.pending_withdrawal_count ?? 0

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='inline-flex items-center gap-2'>
          <Landmark className='text-primary size-5' />
          <span>{t('Finance Center')}</span>
          {isFinanceRole && (
            <Badge variant='secondary' className='text-xs'>
              {t('Finance Officer Workbench')}
            </Badge>
          )}
        </span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          size='sm'
          variant='outline'
          onClick={() => queryClient.invalidateQueries({ queryKey: ['finance'] })}
        >
          <RefreshCw className='mr-1.5 size-3.5' />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <Tabs value={tab} onValueChange={setTab} className='space-y-4'>
          <TabsList className='flex-wrap'>
            <TabsTrigger value='overview'>{t('Financial Overview')}</TabsTrigger>
            <TabsTrigger value='withdrawals' className='gap-1.5'>
              <QrCode className='size-3.5' />
              <span>{t('Withdrawals & QR Payouts')}</span>
              {pendingWithdrawals > 0 && (
                <Badge
                  variant='destructive'
                  className='h-4 px-1.5 text-[10px] leading-none'
                >
                  {pendingWithdrawals}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value='topups'>{t('Top-up Orders')}</TabsTrigger>
            <TabsTrigger value='subscriptions'>
              {t('Subscriptions & Redemptions')}
            </TabsTrigger>
            <TabsTrigger value='settlements'>
              {t('Settlement & Payouts')}
            </TabsTrigger>
            <TabsTrigger value='ledger'>
              {t('Unified Ledger & Reconciliation')}
            </TabsTrigger>
          </TabsList>

          <TabsContent value='overview'>
            <FinanceOverviewTab
              onOpenWithdrawals={() => setTab('withdrawals')}
            />
          </TabsContent>
          <TabsContent value='withdrawals'>
            <FinanceWithdrawalsTab />
          </TabsContent>
          <TabsContent value='topups'>
            <FinanceTopUpsTab />
          </TabsContent>
          <TabsContent value='subscriptions'>
            <FinanceSubscriptionsRedemptionsTab />
          </TabsContent>
          <TabsContent value='settlements'>
            <FinanceSettlementsTab />
          </TabsContent>
          <TabsContent value='ledger'>
            <FinanceLedgerReconcileTab />
          </TabsContent>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
