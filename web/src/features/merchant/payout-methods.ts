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
export const PAYOUT_METHOD_OPTIONS = [
  { value: '', labelKey: 'Select payout method (can bind later)' },
  { value: 'usdt_trc20', labelKey: 'USDT (TRC-20)' },
  { value: 'usdt_bep20', labelKey: 'USDT (BEP-20)' },
  { value: 'alipay', labelKey: 'Alipay' },
  { value: 'wechat', labelKey: 'WeChat Pay' },
  { value: 'bank', labelKey: 'Bank Card' },
  { value: 'other', labelKey: 'Other' },
] as const

export function getPayoutMethodLabel(method?: string): string {
  const found = PAYOUT_METHOD_OPTIONS.find((item) => item.value === method)
  return found ? found.labelKey : method || '--'
}
