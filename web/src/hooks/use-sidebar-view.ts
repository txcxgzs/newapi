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
import { useLocation } from '@tanstack/react-router'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { resolveSidebarView } from '@/components/layout/lib/sidebar-view-registry'
import type {
  NavGroup,
  NavItem,
  ResolvedSidebarView,
  SidebarBadges,
} from '@/components/layout/types'
import { useModelPricingIssueBadge } from '@/features/system-settings/models/pricing-issues'
import { hasPermission } from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { type AuthUser, useAuthStore } from '@/stores/auth-store'

import { useSidebarConfig } from './use-sidebar-config'
import { useSidebarData } from './use-sidebar-data'

/** Sentinel key used for the root navigation in animation `key=` props */
const ROOT_VIEW_KEY = '__root'

/**
 * Role-based visibility of a single sidebar item.
 *
 * `requiredRole` sets a lower bound, `maxRole` an upper bound. Items that set
 * neither stay visible for every role, so existing entries keep their
 * behaviour. Route-level guards still enforce access independently.
 */
export function isNavItemVisibleForRole(
  item: NavItem,
  role: number,
  user?: AuthUser | null
): boolean {
  if (item.excludedRoles?.includes(role)) {
    return false
  }
  if (item.requiredRole !== undefined && role < item.requiredRole) {
    return false
  }
  if (item.maxRole !== undefined && role > item.maxRole) {
    return false
  }
  if (
    item.requiredPermission &&
    user?.permissions?.admin_permissions !== undefined &&
    !hasPermission(
      user,
      item.requiredPermission.resource,
      item.requiredPermission.action
    )
  ) {
    return false
  }
  return true
}

export function useSidebarView(): ResolvedSidebarView {
  const { t } = useTranslation()
  const pathname = useLocation({ select: (l) => l.pathname })
  const authUser = useAuthStore((s) => s.auth.user)
  const userRole = authUser?.role
  const modelPricingBadge = useModelPricingIssueBadge()
  const badges: SidebarBadges = { modelPricing: modelPricingBadge }
  const rootSidebarData = useSidebarData(badges)
  const configFilteredRoot = useSidebarConfig(rootSidebarData.navGroups)

  const rootNavGroups = useMemo<NavGroup[]>(() => {
    const role = userRole ?? ROLE.GUEST
    const isAdminOrFinance = role >= ROLE.FINANCE
    return configFilteredRoot
      .filter((group) => (group.id === 'admin' ? isAdminOrFinance : true))
      .map((group) => {
        const items = group.items.filter((item) =>
          isNavItemVisibleForRole(item, role, authUser)
        )
        return items.length === group.items.length ? group : { ...group, items }
      })
      .filter((group) => group.items.length > 0)
  }, [authUser, configFilteredRoot, userRole])

  const view = resolveSidebarView(pathname)

  if (view) {
    return {
      key: view.id,
      view,
      navGroups: view.getNavGroups(t, badges),
    }
  }

  return {
    key: ROOT_VIEW_KEY,
    view: null,
    navGroups: rootNavGroups,
  }
}
