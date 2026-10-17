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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { MerchantApplyPage } from '@/features/merchant/apply-page'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/merchant/apply/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    // 已是商家：入驻申请不再适用，直接回到商家门户。
    if (auth.user && auth.user.role >= ROLE.MERCHANT) {
      throw redirect({
        to: '/merchant',
      })
    }
  },
  component: MerchantApplyPage,
})
