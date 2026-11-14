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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { PriceSyncSettingsSection } from '../price-sync-settings-section'

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <PriceSyncSettingsSection
          defaultValues={{
            enableFloorProtection: true,
            upstreamUsdCnyRate: 6.6707,
            feeMultiplier: 1.06,
            minProfitMult: 1.5,
          }}
        />
      </SettingsPageProvider>
    </>
  )
}

async function renderSettings() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Fixture />
    </QueryClientProvider>
  )
  return screen.findByRole('spinbutton', {
    name: 'Upstream purchase USD/CNY rate',
  })
}

beforeEach(() => {
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

test('shows the current cost and protection values', async () => {
  await renderSettings()

  expect(
    screen.getByRole('switch', { name: 'Enable floor protection' })
  ).toBeChecked()
  expect(
    screen.getByRole('spinbutton', { name: 'Upstream purchase USD/CNY rate' })
  ).toHaveValue(6.6707)
  expect(
    screen.getByRole('spinbutton', { name: 'Fee multiplier' })
  ).toHaveValue(1.06)
  expect(
    screen.getByRole('spinbutton', { name: 'Minimum profit multiplier' })
  ).toHaveValue(1.5)
})

test('saves changed risk-control values with market_setting keys', async () => {
  const user = userEvent.setup()
  await renderSettings()

  const fee = screen.getByRole('spinbutton', { name: 'Fee multiplier' })
  await user.clear(fee)
  await user.type(fee, '1.2')
  await user.tab()
  await user.click(
    screen.getByRole('button', { name: 'Save price sync settings' })
  )

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'market_setting.fee_multiplier',
      value: '1.2',
    })
  )
})

test('saves a disabled floor protection switch as false', async () => {
  const user = userEvent.setup()
  await renderSettings()

  await user.click(
    screen.getByRole('switch', { name: 'Enable floor protection' })
  )
  await user.click(
    screen.getByRole('button', { name: 'Save price sync settings' })
  )

  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'market_setting.enable_floor_protection',
      value: 'false',
    })
  )
})
