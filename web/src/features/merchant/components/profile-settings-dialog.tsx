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
import { Lock } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { handleServerError } from '@/lib/handle-server-error'

import { updateMerchantProfile } from '../api'
import { OPERATION_TENURE_OPTIONS } from '../lib/payout-qrcode'
import { PAYOUT_METHOD_OPTIONS } from '../payout-methods'
import type { MerchantProfile } from '../types'
import { PayoutQrcodeUploader } from './payout-qrcode-uploader'

export function MerchantProfileSettingsDialog({
  open,
  onOpenChange,
  profile,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  profile?: MerchantProfile
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [platformUrl, setPlatformUrl] = useState('')
  const [operationTenure, setOperationTenure] = useState('')
  const [contactName, setContactName] = useState('')
  const [contactInfo, setContactInfo] = useState('')
  const [description, setDescription] = useState('')
  const [payoutMethod, setPayoutMethod] = useState('')
  const [payoutAccount, setPayoutAccount] = useState('')
  const [payoutHolder, setPayoutHolder] = useState('')
  const [payoutQrcode, setPayoutQrcode] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open || !profile) return
    setPlatformUrl(profile.platform_url ?? '')
    setOperationTenure(profile.operation_tenure ?? '')
    setContactName(profile.contact_name ?? '')
    setContactInfo(profile.contact_info ?? '')
    setDescription(profile.description ?? '')
    setPayoutMethod(profile.payout_method ?? '')
    setPayoutAccount(profile.payout_account ?? '')
    setPayoutHolder(profile.payout_holder ?? '')
    setPayoutQrcode(profile.payout_qrcode ?? '')
  }, [open, profile])

  async function handleSave() {
    if (payoutMethod && !payoutAccount.trim()) {
      toast.error(
        t('Please enter the payout account for the selected payout method')
      )
      return
    }
    setSaving(true)
    try {
      await updateMerchantProfile({
        platform_url: platformUrl.trim(),
        operation_tenure: operationTenure,
        contact_name: contactName.trim(),
        contact_info: contactInfo.trim(),
        description: description.trim(),
        payout_method: payoutMethod,
        payout_account: payoutAccount.trim(),
        payout_holder: payoutHolder.trim(),
        payout_qrcode: payoutQrcode,
      })
      toast.success(t('Merchant profile updated'))
      onSaved()
      onOpenChange(false)
    } catch (error) {
      handleServerError(error, t('Failed to update merchant profile'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] max-w-xl overflow-y-auto'>
        <DialogHeader>
          <DialogTitle>{t('Merchant Contact & Payout Profile')}</DialogTitle>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='bg-muted/30 flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-xs'>
            <div className='flex items-center gap-2'>
              <Lock className='text-muted-foreground size-3.5' />
              <span>
                {t('Shop Name')}:{' '}
                <strong className='text-foreground'>
                  {profile?.shop_name || profile?.site_name || '--'}
                </strong>
              </span>
            </div>
            <span className='text-muted-foreground'>
              {profile?.merchant_code ? `${profile.merchant_code} · ` : ''}
              T+{profile?.settle_hold_days ?? 0} · {t('Min withdraw')}: $
              {(profile?.min_withdraw ?? 0).toFixed(2)}
            </span>
          </div>
          <p className='text-muted-foreground text-[11px]'>
            {t(
              'Shop Name is locked across headers, Finance Center reconciliation, and group prefixes. Use "Request Shop Rename" on the Merchant Portal header if a change is needed.'
            )}
          </p>

          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label htmlFor='portal-profile-platform-url'>
                {t('Platform Website URL')}
              </Label>
              <Input
                id='portal-profile-platform-url'
                value={platformUrl}
                onChange={(e) => setPlatformUrl(e.target.value)}
                placeholder='https://your-platform.example.com'
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='portal-profile-tenure'>
                {t('Operation Tenure')}
              </Label>
              <Select
                items={OPERATION_TENURE_OPTIONS.map((option) => ({
                  value: option.value,
                  label: t(option.labelKey),
                }))}
                value={operationTenure}
                onValueChange={(next) => {
                  if (next !== null) setOperationTenure(next)
                }}
              >
                <SelectTrigger id='portal-profile-tenure' className='w-full'>
                  <SelectValue placeholder={t('Select operation duration')} />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {OPERATION_TENURE_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {t(option.labelKey)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label htmlFor='portal-profile-contact-name'>
                {t('Contact Person')}
              </Label>
              <Input
                id='portal-profile-contact-name'
                value={contactName}
                onChange={(e) => setContactName(e.target.value)}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='portal-profile-contact-info'>
                {t('Contact Info')}
              </Label>
              <Input
                id='portal-profile-contact-info'
                value={contactInfo}
                onChange={(e) => setContactInfo(e.target.value)}
              />
            </div>
          </div>

          <div className='space-y-1.5'>
            <Label htmlFor='portal-profile-desc'>
              {t('Channel Supply & SLA Notes')}
            </Label>
            <Input
              id='portal-profile-desc'
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>

          <div className='space-y-1.5'>
            <Label htmlFor='portal-profile-payout-method'>
              {t('Default Payout Method')}
            </Label>
            <Select
              items={PAYOUT_METHOD_OPTIONS.map((option) => ({
                value: option.value,
                label: t(option.labelKey),
              }))}
              value={payoutMethod}
              onValueChange={(next) => {
                if (next !== null) setPayoutMethod(next)
              }}
            >
              <SelectTrigger id='portal-profile-payout-method' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {PAYOUT_METHOD_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {t(option.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label htmlFor='portal-profile-payout-account'>
                {t('Payout Account')}
              </Label>
              <Input
                id='portal-profile-payout-account'
                value={payoutAccount}
                onChange={(e) => setPayoutAccount(e.target.value)}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='portal-profile-payout-holder'>
                {t('Account Holder')}
              </Label>
              <Input
                id='portal-profile-payout-holder'
                value={payoutHolder}
                onChange={(e) => setPayoutHolder(e.target.value)}
              />
            </div>
          </div>

          <div className='space-y-1.5'>
            <Label htmlFor='portal-profile-payout-qrcode'>
              {t('Payout Collection QR Code')}
            </Label>
            <PayoutQrcodeUploader
              id='portal-profile-payout-qrcode'
              value={payoutQrcode}
              onChange={setPayoutQrcode}
            />
          </div>

          <div className='flex justify-end gap-2 pt-2'>
            <Button variant='outline' onClick={() => onOpenChange(false)}>
              {t('Cancel')}
            </Button>
            <Button disabled={saving} onClick={handleSave}>
              {saving ? t('Saving...') : t('Save Profile')}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
