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
  site_name: 'MyRelay',
  site_url: 'https://api.example.com',
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

async function fillBaseFields(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Site Name'), 'MyRelay')
  await user.type(screen.getByLabelText('Site URL'), 'https://api.example.com')
  await user.type(screen.getByLabelText('API Key'), 'sk-test')
}

it('maps upstream models to platform models and submits the mapping', async () => {
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

  await fillBaseFields(user)
  await user.click(
    screen.getByRole('button', { name: 'Probe upstream & pull models' })
  )
  // 拉到模型清单：出现对接编辑器，且同名模型自动对接；手填清单默认收起
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
  // 同名模型直接上架；上游独有的名字未对接，不上架
  expect(submitMock).toHaveBeenCalledWith({
    site_name: 'MyRelay',
    site_url: 'https://api.example.com',
    site_key: 'sk-test',
    upstream_kind: '',
    rate_multiplier: undefined,
    models: [{ model: 'claude-sonnet-4' }, { model: 'gpt-4o' }],
  })
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
    price_fetch_message:
      '该上游类型不提供价格接口，报价将按平台标准价挂牌',
  })
  renderForm(onSubmitted)

  await fillBaseFields(user)
  await user.click(
    screen.getByRole('button', { name: 'Probe upstream & pull models' })
  )
  // 没拉到模型清单：自动展开手动兜底，并出现折扣倍率兜底输入
  const manual = await screen.findByLabelText('Listed Models')
  const fallback = screen.getByLabelText('Optional discount rate (fallback)')
  await user.clear(fallback)
  await user.type(fallback, '0.5')
  await user.type(manual, 'gpt-4, default-pool\ngpt-4o')
  await user.click(screen.getByRole('button', { name: 'Submit Application' }))

  await waitFor(() => expect(onSubmitted).toHaveBeenCalledTimes(1))
  expect(submitMock).toHaveBeenCalledWith({
    site_name: 'MyRelay',
    site_url: 'https://api.example.com',
    site_key: 'sk-test',
    upstream_kind: '',
    rate_multiplier: 0.5,
    models: [
      { model: 'gpt-4', pool: 'default-pool' },
      { model: 'gpt-4o' },
    ],
  })
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

  await fillBaseFields(user)
  await user.click(
    screen.getByRole('button', { name: 'Probe upstream & pull models' })
  )
  const manual = await screen.findByLabelText('Listed Models')
  // 缺少模型名（仅号池）的行应被判定为非法并阻止提交
  await user.type(manual, ', pro')
  await user.click(screen.getByRole('button', { name: 'Submit Application' }))

  expect(submitMock).not.toHaveBeenCalled()
  expect(onSubmitted).not.toHaveBeenCalled()
})
