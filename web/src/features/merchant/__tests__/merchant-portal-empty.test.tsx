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
import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, AxiosHeaders } from 'axios'
import type { AnchorHTMLAttributes, ReactNode } from 'react'
import { toast } from 'sonner'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { createAppQueryClient } from '@/lib/query-client'
import { ROLE } from '@/lib/roles'
import { createServerError } from '@/lib/server-error-message'

import {
  getMerchantSummary,
  getMyMerchantApplication,
  getPlatformModels,
} from '../api'
import { Merchant } from '../index'

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  Link: ({
    to,
    children,
    ...rest
  }: { to: string; children?: ReactNode } & AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
}))

vi.mock('../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api')>()
  return {
    ...actual,
    getMerchantSummary: vi.fn(),
    getMyMerchantApplication: vi.fn(),
    getPlatformModels: vi.fn(),
  }
})

const getSummaryMock = vi.mocked(getMerchantSummary)
const getApplicationMock = vi.mocked(getMyMerchantApplication)
const getPlatformModelsMock = vi.mocked(getPlatformModels)

function make403Error(message: string): Error {
  const axiosErr = new AxiosError(
    message,
    AxiosError.ERR_BAD_REQUEST,
    undefined,
    undefined,
    {
      status: 403,
      statusText: 'Forbidden',
      headers: new AxiosHeaders(),
      config: { headers: new AxiosHeaders() },
      data: { success: false, message },
    }
  )
  return createServerError(axiosErr)
}

function renderMerchantPage() {
  const client = createAppQueryClient()
  return render(
    <QueryClientProvider client={client}>
      <Merchant />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: { id: 1, role: ROLE.SUPER_ADMIN } },
  } as never)
  getApplicationMock.mockResolvedValue(null)
  getPlatformModelsMock.mockResolvedValue(['gpt-4o'])
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

it('shows friendly un-onboarded state without popping error toast when an admin has no merchant profile', async () => {
  const user = userEvent.setup()
  getSummaryMock.mockRejectedValue(make403Error('商家资料不存在'))

  renderMerchantPage()

  expect(await screen.findByText('No Merchant Profile Yet')).toBeInTheDocument()
  expect(
    screen.getByRole('button', { name: 'Go to Merchant Management' })
  ).toHaveAttribute('href', '/merchant-admin')
  expect(toast.error).not.toHaveBeenCalled()

  await user.click(
    screen.getByRole('button', { name: 'Apply to Become a Merchant' })
  )
  expect(await screen.findByLabelText('Shop Name')).toBeInTheDocument()
})

it('shows suspended state cleanly without popping error toast when merchant account is suspended', async () => {
  getSummaryMock.mockRejectedValue(make403Error('商家账号已被停用'))

  renderMerchantPage()

  expect(
    await screen.findByText('Merchant Account Suspended')
  ).toBeInTheDocument()
  await waitFor(() => {
    expect(toast.error).not.toHaveBeenCalled()
  })
})
