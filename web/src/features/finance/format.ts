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
import { formatCurrencyFromUSD, formatLocalCurrencyAmount } from '@/lib/currency'

export const FINANCE_PAGE_SIZE = 20

export function formatFinanceUsd(
  value: number | null | undefined,
  digits = 4
): string {
  return formatCurrencyFromUSD(value ?? 0, {
    digitsLarge: digits,
    digitsSmall: digits,
  })
}

export function formatFinanceMoney(
  value: number | null | undefined,
  digits = 2
): string {
  return formatLocalCurrencyAmount(value ?? 0, {
    digitsLarge: digits,
    digitsSmall: digits,
  })
}

export function getWithdrawalStatusMeta(status: number): {
  variant: 'default' | 'secondary' | 'destructive' | 'outline'
  labelKey: string
} {
  if (status === 0) {
    return { variant: 'secondary', labelKey: 'Pending Review' }
  }
  if (status === 1) {
    return { variant: 'outline', labelKey: 'Approved · Awaiting Pay' }
  }
  if (status === 2) {
    return { variant: 'default', labelKey: 'Paid' }
  }
  return { variant: 'destructive', labelKey: 'Rejected · Refunded' }
}
