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
import { AxiosError, AxiosHeaders } from 'axios'
import { describe, expect, it } from 'vitest'

import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  FINANCE_ROLE_KEY,
  hasAnyPermission,
  hasPermission,
  normalizeAdminPermissions,
  normalizeAuditReasonRules,
  requiresAuditReason,
  type PermissionCatalog,
} from '../admin-permissions'
import {
  AUDIT_REASON_HEADER,
  extractAuditReasonRequiredInfo,
  withAuditReasonHeaders,
} from '../audit-reason'
import { ROLE, getRoleLabelKey } from '../roles'

const SAMPLE_CATALOG: PermissionCatalog = {
  resources: [
    {
      resource: 'finance',
      label_key: 'Finance Center',
      actions: [
        { action: 'read', label_key: 'Read', description_key: '' },
        {
          action: 'payout',
          label_key: 'Payout',
          description_key: '',
          sensitive: true,
          default_audit_reason: true,
        },
      ],
    },
    {
      resource: 'channel',
      label_key: 'Channels',
      actions: [
        { action: 'read', label_key: 'Read', description_key: '' },
        {
          action: 'secret_view',
          label_key: 'Secret View',
          description_key: '',
          sensitive: true,
          default_audit_reason: true,
        },
      ],
    },
  ],
  roles: [
    {
      key: FINANCE_ROLE_KEY,
      name: 'Finance Officer',
      built_in: true,
      superuser: false,
      grants: {
        finance: { read: true, payout: true },
        channel: { read: false, secret_view: false },
      },
      audit_reason_defaults: {
        finance: { read: false, payout: true },
      },
    },
  ],
}

describe('RBAC & Finance Officer roles', () => {
  it('recognizes ROLE.FINANCE = 6 and ROLE.MERCHANT = 2', () => {
    expect(ROLE.FINANCE).toBe(6)
    expect(ROLE.MERCHANT).toBe(2)
    expect(getRoleLabelKey(ROLE.FINANCE)).toBe('Finance Officer')
    expect(getRoleLabelKey(ROLE.MERCHANT)).toBe('Merchant')
  })

  it('normalizes permissions and audit reason rules using the Finance Officer baseline', () => {
    const perms = normalizeAdminPermissions(
      undefined,
      SAMPLE_CATALOG,
      FINANCE_ROLE_KEY
    )
    expect(perms.finance.read).toBe(true)
    expect(perms.finance.payout).toBe(true)
    expect(perms.channel.secret_view).toBe(false)

    const rules = normalizeAuditReasonRules(
      undefined,
      SAMPLE_CATALOG,
      FINANCE_ROLE_KEY
    )
    expect(rules.finance.read).toBe(false)
    expect(rules.finance.payout).toBe(true)
    expect(rules.channel.secret_view).toBe(true)
  })

  it('checks requiresAuditReason for scoped operators and exempts root', () => {
    const adminUser = {
      id: 10,
      username: 'subadmin',
      role: ROLE.ADMIN,
      permissions: {
        admin_permissions: {
          finance: { read: true, payout: true },
          channel: { read: true, secret_view: true },
        },
        audit_reason_actions: {
          finance: { read: false, payout: true },
          channel: { secret_view: true },
        },
      },
    }
    const rootUser = {
      ...adminUser,
      id: 1,
      role: ROLE.SUPER_ADMIN,
    }

    expect(
      hasPermission(
        adminUser,
        ADMIN_PERMISSION_RESOURCES.FINANCE,
        ADMIN_PERMISSION_ACTIONS.READ
      )
    ).toBe(true)
    expect(
      hasAnyPermission(adminUser, ADMIN_PERMISSION_RESOURCES.FINANCE, [
        ADMIN_PERMISSION_ACTIONS.PAYOUT,
      ])
    ).toBe(true)

    expect(
      requiresAuditReason(
        adminUser,
        ADMIN_PERMISSION_RESOURCES.FINANCE,
        ADMIN_PERMISSION_ACTIONS.READ
      )
    ).toBe(false)
    expect(
      requiresAuditReason(
        adminUser,
        ADMIN_PERMISSION_RESOURCES.FINANCE,
        ADMIN_PERMISSION_ACTIONS.PAYOUT
      )
    ).toBe(true)
    expect(
      requiresAuditReason(
        adminUser,
        ADMIN_PERMISSION_RESOURCES.CHANNEL,
        ADMIN_PERMISSION_ACTIONS.SECRET_VIEW
      )
    ).toBe(true)
    expect(
      requiresAuditReason(
        rootUser,
        ADMIN_PERMISSION_RESOURCES.FINANCE,
        ADMIN_PERMISSION_ACTIONS.PAYOUT
      )
    ).toBe(false)
  })

  it('extracts AUDIT_REASON_REQUIRED metadata and attaches X-Audit-Reason header', () => {
    const axiosErr = new AxiosError(
      'reason required',
      'ERR_BAD_REQUEST',
      undefined,
      undefined,
      {
        status: 400,
        statusText: 'Bad Request',
        headers: new AxiosHeaders(),
        config: { headers: new AxiosHeaders() },
        data: {
          code: 'AUDIT_REASON_REQUIRED',
          resource: 'finance',
          action: 'payout',
          min_length: 15,
          message: 'Please provide an audit reason',
        },
      }
    )

    expect(extractAuditReasonRequiredInfo(axiosErr)).toEqual({
      required: true,
      resource: 'finance',
      action: 'payout',
      minLength: 15,
      message: 'Please provide an audit reason',
    })

    const cfg = withAuditReasonHeaders('  Monthly merchant payout settle #7  ')
    expect((cfg.headers as Record<string, string>)[AUDIT_REASON_HEADER]).toBe(
      'Monthly merchant payout settle #7'
    )
  })
})
