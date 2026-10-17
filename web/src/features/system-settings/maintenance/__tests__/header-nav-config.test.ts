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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import {
  HEADER_NAV_DEFAULT,
  parseHeaderNavModules,
  serializeHeaderNavModules,
} from '../config'

describe('header navigation market module', () => {
  test('defaults enable the market module so the form and reset keep it', () => {
    assert.deepEqual(HEADER_NAV_DEFAULT.market, {
      enabled: true,
      requireAuth: false,
    })
  })

  test('a legacy configuration without the market key falls back to enabled', () => {
    const parsed = parseHeaderNavModules(JSON.stringify({ home: true }))

    assert.deepEqual(parsed.market, { enabled: true, requireAuth: false })
  })

  test('an admin change to the market module survives a save round trip', () => {
    const saved = serializeHeaderNavModules({
      ...parseHeaderNavModules(null),
      market: { enabled: false, requireAuth: true },
    })

    assert.deepEqual(parseHeaderNavModules(saved).market, {
      enabled: false,
      requireAuth: true,
    })
  })

  test('a boolean market value keeps the module default for requireAuth', () => {
    assert.deepEqual(parseHeaderNavModules('{"market":false}').market, {
      enabled: false,
      requireAuth: false,
    })
  })
})
