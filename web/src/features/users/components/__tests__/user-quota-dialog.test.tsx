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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { adjustUserQuota } from '../../api'
import { UserQuotaDialog } from '../user-quota-dialog'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock('../../api', () => ({ adjustUserQuota: vi.fn() }))

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

beforeEach(() => {
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
})

function renderGiftDialog(props: {
  onSuccess: () => void
  onOpenChange: (open: boolean) => void
}) {
  render(
    <I18nextProvider i18n={i18n}>
      <UserQuotaDialog
        open
        onOpenChange={props.onOpenChange}
        userId={7}
        currentQuota={1000}
        onSuccess={props.onSuccess}
        kind='gift'
      />
    </I18nextProvider>
  )
}

test('sends the add_gift_quota action and reports success when adjusting gift quota', async () => {
  vi.mocked(adjustUserQuota).mockResolvedValue({ success: true })
  const onSuccess = vi.fn()
  const onOpenChange = vi.fn()
  const user = userEvent.setup()
  renderGiftDialog({ onSuccess, onOpenChange })

  const dialog = await screen.findByRole('dialog', {
    name: 'Adjust Gift Quota',
  })
  expect(within(dialog).getByText(/Current gift quota/)).toBeInTheDocument()

  await user.type(within(dialog).getByRole('spinbutton'), '1')
  await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))

  await waitFor(() =>
    expect(adjustUserQuota).toHaveBeenCalledWith({
      id: 7,
      action: 'add_gift_quota',
      mode: 'add',
      value: 500000,
    })
  )
  await waitFor(() =>
    expect(toast.success).toHaveBeenCalledWith(
      'Gift quota adjusted successfully'
    )
  )
  expect(onOpenChange).toHaveBeenCalledWith(false)
  expect(onSuccess).toHaveBeenCalledTimes(1)
})

test('keeps the gift dialog open and reports failure when the adjustment is rejected', async () => {
  vi.mocked(adjustUserQuota).mockResolvedValue({
    success: false,
    message: 'Gift quota update rejected',
  })
  const onSuccess = vi.fn()
  const onOpenChange = vi.fn()
  const user = userEvent.setup()
  renderGiftDialog({ onSuccess, onOpenChange })

  const dialog = await screen.findByRole('dialog', {
    name: 'Adjust Gift Quota',
  })
  await user.type(within(dialog).getByRole('spinbutton'), '1')
  await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))

  await waitFor(() =>
    expect(adjustUserQuota).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'add_gift_quota' })
    )
  )
  await waitFor(() => expect(toast.error).toHaveBeenCalled())
  expect(onSuccess).not.toHaveBeenCalled()
  expect(onOpenChange).not.toHaveBeenCalledWith(false)
  expect(
    screen.getByRole('dialog', { name: 'Adjust Gift Quota' })
  ).toBeVisible()
})
