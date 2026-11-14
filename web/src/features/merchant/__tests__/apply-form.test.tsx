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
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import {
  getPlatformModels,
  probeMerchantUpstream,
  submitMerchantApplication,
} from '../api'
import { MerchantApplyForm } from '../apply-form'
import type { MerchantApplication } from '../types'

vi.mock('../api', () => ({
  submitMerchantApplication: vi.fn(),
  probeMerchantUpstream: vi.fn(),
  getPlatformModels: vi.fn(),
}))

const submitMock = vi.mocked(submitMerchantApplication)
const probeMock = vi.mocked(probeMerchantUpstream)
const platformModelsMock = vi.mocked(getPlatformModels)

const submittedApplication: MerchantApplication = {
  id: 1,
  user_id: 2,
  shop_name: 'AcmeRelay',
  platform_url: 'https://acme-ai.example.com',
  operation_tenure: '1-2y',
  site_name: 'AcmeRelay',
  site_url: '',
  site_key: '',
  models: '[]',
  upstream_kind: '',
  rate_multiplier: 1,
  status: 0,
  review_note: '',
  reviewed_at: 0,
  created_at: 0,
}

beforeEach(() => {
  submitMock.mockReset()
  submitMock.mockResolvedValue(submittedApplication)
  probeMock.mockReset()
  platformModelsMock.mockReset()
  platformModelsMock.mockResolvedValue([
    'claude-sonnet-4',
    'gpt-4o',
    'gpt-4o-mini',
  ])
})

afterEach(() => {
  vi.clearAllMocks()
})

function renderForm(onSubmitted: () => void) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MerchantApplyForm onSubmitted={onSubmitted} />
    </QueryClientProvider>
  )
}

async function fillQualificationFields(
  user: ReturnType<typeof userEvent.setup>
) {
  await user.type(screen.getByLabelText('Shop Name'), 'MyRelay')
  await user.type(
    screen.getByLabelText('Platform Website URL'),
    'https://portal.myrelay.example.com'
  )
  await user.click(screen.getByLabelText('Operation Tenure'))
  await user.click(await screen.findByText('1 - 2 years'))
  await user.type(screen.getByLabelText('Contact Person'), 'Alice')
  await user.type(screen.getByLabelText('Contact Info'), 'tg:@alice')
  await user.click(
    screen.getByRole('switch', {
      name: 'Merchant Settlement & Commission Agreement',
    })
  )
}

async function fillOptionalChannelFields(
  user: ReturnType<typeof userEvent.setup>
) {
  await user.click(
    screen.getByRole('switch', {
      name: 'Configure initial upstream channel now (optional)',
    })
  )
  await user.type(
    screen.getByLabelText('API Base URL'),
    'https://api.example.com'
  )
  await user.type(screen.getByLabelText('API Key'), 'sk-test')
}

it('submits pure qualification onboarding without requiring an initial upstream channel', async () => {
  const user = userEvent.setup()
  const onSubmitted = vi.fn()
  renderForm(onSubmitted)

  // Displays warnings about Shop Name lock and display-only Platform URL
  expect(
    screen.getByText(/Finance Center reconciliation statements/i)
  ).toBeInTheDocument()
  expect(
    screen.getByText(/never used for API relay or endpoint probing/i)
  ).toBeInTheDocument()
  expect(screen.queryByLabelText('API Base URL')).not.toBeInTheDocument()

  await fillQualificationFields(user)
  await user.click(screen.getByRole('button', { name: 'Submit Application' }))

  await waitFor(() => expect(onSubmitted).toHaveBeenCalledTimes(1))
  expect(submitMock).toHaveBeenCalledWith(
    expect.objectContaining({
      shop_name: 'MyRelay',
      platform_url: 'https://portal.myrelay.example.com',
      operation_tenure: '1-2y',
      site_name: 'MyRelay',
      site_url: undefined,
      site_key: undefined,
      contact_name: 'Alice',
      contact_info: 'tg:@alice',
      agreed_terms: true,
    })
  )
})

it('maps upstream models when optional initial channel setup is enabled', async () => {
  const user = userEvent.setup()
  const onSubmitted = vi.fn()
  probeMock.mockResolvedValue({
    kind: 'newapi',
    price_sync_capable: true,
    models_endpoint_ok: true,
    model_count: 3,
    price_fetch_ok: true,
    ratio_total: 3,
    ratio_preview: [
      { model_name: 'gpt-4o', input_price: 2.5, output_price: 10 },
    ],
    models: ['claude-sonnet-4', 'gpt-4o', 'upstream-only-alias'],
  })
  renderForm(onSubmitted)

  await fillQualificationFields(user)
  await fillOptionalChannelFields(user)
  await user.click(
    screen.getByRole('button', { name: 'Probe upstream & pull models' })
  )
  expect(await screen.findByText('Model mapping')).toBeInTheDocument()
  expect(screen.queryByLabelText('Listed Models')).not.toBeInTheDocument()
  expect(screen.getByText('2 of 3 mapped')).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Submit Application' }))

  await waitFor(() => expect(onSubmitted).toHaveBeenCalledTimes(1))
  expect(probeMock).toHaveBeenCalledWith({
    site_url: 'https://api.example.com',
    site_key: 'sk-test',
    upstream_kind: '',
  })
  expect(submitMock).toHaveBeenCalledWith(
    expect.objectContaining({
      shop_name: 'MyRelay',
      platform_url: 'https://portal.myrelay.example.com',
      operation_tenure: '1-2y',
      site_name: 'MyRelay',
      site_url: 'https://api.example.com',
      site_key: 'sk-test',
      contact_name: 'Alice',
      contact_info: 'tg:@alice',
      agreed_terms: true,
      upstream_kind: '',
      join_auto_group: true,
      rate_multiplier: 1,
      models: [{ model: 'claude-sonnet-4' }, { model: 'gpt-4o' }],
    })
  )
})

it('falls back to the manual model list when no models could be pulled', async () => {
  const user = userEvent.setup()
  const onSubmitted = vi.fn()
  probeMock.mockResolvedValue({
    kind: 'sub2api',
    price_sync_capable: false,
    models_endpoint_ok: false,
    model_count: 0,
    price_fetch_ok: false,
    price_fetch_message: '该上游类型不提供价格接口，报价将按平台标准价挂牌',
  })
  renderForm(onSubmitted)

  await fillQualificationFields(user)
  await fillOptionalChannelFields(user)
  await user.click(
    screen.getByRole('button', { name: 'Probe upstream & pull models' })
  )
  const manual = await screen.findByLabelText('Listed Models')
  const fallback = screen.getByLabelText('分组倍率（GroupRatio）')
  await user.clear(fallback)
  await user.type(fallback, '0.5')
  await user.type(manual, 'gpt-4, default-pool\ngpt-4o')
  await user.click(screen.getByRole('button', { name: 'Submit Application' }))

  await waitFor(() => expect(onSubmitted).toHaveBeenCalledTimes(1))
  expect(submitMock).toHaveBeenCalledWith(
    expect.objectContaining({
      shop_name: 'MyRelay',
      site_name: 'MyRelay',
      site_url: 'https://api.example.com',
      site_key: 'sk-test',
      rate_multiplier: 0.5,
      models: [
        { model: 'gpt-4', pool: 'default-pool' },
        { model: 'gpt-4o' },
      ],
    })
  )
})

it('blocks onboarding submission when settlement terms are not agreed', async () => {
  const user = userEvent.setup()
  const onSubmitted = vi.fn()
  renderForm(onSubmitted)

  await user.type(screen.getByLabelText('Shop Name'), 'MyRelay')
  await user.type(
    screen.getByLabelText('Platform Website URL'),
    'https://portal.myrelay.example.com'
  )
  await user.type(screen.getByLabelText('Contact Info'), 'tg:@alice')
  await user.click(screen.getByRole('button', { name: 'Submit Application' }))

  expect(submitMock).not.toHaveBeenCalled()
  expect(onSubmitted).not.toHaveBeenCalled()
})

it('blocks submission when a filled manual model row has no model name', async () => {
  const user = userEvent.setup()
  const onSubmitted = vi.fn()
  probeMock.mockResolvedValue({
    kind: '',
    price_sync_capable: false,
    models_endpoint_ok: false,
    model_count: 0,
    price_fetch_ok: false,
  })
  renderForm(onSubmitted)

  await fillQualificationFields(user)
  await fillOptionalChannelFields(user)
  await user.click(
    screen.getByRole('button', { name: 'Probe upstream & pull models' })
  )
  const manual = await screen.findByLabelText('Listed Models')
  await user.type(manual, ', pro')
  await user.click(screen.getByRole('button', { name: 'Submit Application' }))

  expect(submitMock).not.toHaveBeenCalled()
  expect(onSubmitted).not.toHaveBeenCalled()
})
