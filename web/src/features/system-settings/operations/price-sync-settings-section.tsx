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
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const schema = z.object({
  enableFloorProtection: z.boolean(),
  upstreamUsdCnyRate: z.coerce.number().positive(),
  feeMultiplier: z.coerce.number().positive(),
  minProfitMult: z.coerce.number().positive(),
})

type Values = z.infer<typeof schema>

type PriceSyncSettingsSectionProps = {
  defaultValues: {
    enableFloorProtection: boolean
    upstreamUsdCnyRate: number
    feeMultiplier: number
    minProfitMult: number
  }
}

export function PriceSyncSettingsSection(props: PriceSyncSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: {
      enableFloorProtection: props.defaultValues.enableFloorProtection,
      upstreamUsdCnyRate: props.defaultValues.upstreamUsdCnyRate,
      feeMultiplier: props.defaultValues.feeMultiplier,
      minProfitMult: props.defaultValues.minProfitMult,
    },
  })

  const { isDirty, isSubmitting } = form.formState
  const enableFloorProtection = form.watch('enableFloorProtection')

  async function onSubmit(values: Values) {
    const updates: Array<{ key: string; value: string }> = []

    if (
      values.enableFloorProtection !== props.defaultValues.enableFloorProtection
    ) {
      updates.push({
        key: 'market_setting.enable_floor_protection',
        value: String(values.enableFloorProtection),
      })
    }
    if (values.upstreamUsdCnyRate !== props.defaultValues.upstreamUsdCnyRate) {
      updates.push({
        key: 'market_setting.upstream_usd_cny_rate',
        value: String(values.upstreamUsdCnyRate),
      })
    }
    if (values.feeMultiplier !== props.defaultValues.feeMultiplier) {
      updates.push({
        key: 'market_setting.fee_multiplier',
        value: String(values.feeMultiplier),
      })
    }
    if (values.minProfitMult !== props.defaultValues.minProfitMult) {
      updates.push({
        key: 'market_setting.min_profit_mult',
        value: String(values.minProfitMult),
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset(values)
  }

  return (
    <SettingsSection title={t('Price Sync & Risk Control')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save price sync settings'
          />
          <FormField
            control={form.control}
            name='enableFloorProtection'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable floor protection')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Keep the current listed price until the upstream cost rises above the minimum safe price; when breached, raise it automatically.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='upstreamUsdCnyRate'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Upstream purchase USD/CNY rate')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step='0.0001'
                      placeholder='6.6707'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Exchange rate used to convert upstream USD cost into the platform billing currency. It is separate from the frontend recharge USD exchange rate.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='feeMultiplier'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Fee multiplier')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step='0.01'
                      placeholder='1.06'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Multiplier applied to the upstream cost to cover payment and channel fees.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='minProfitMult'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Minimum profit multiplier')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step='0.01'
                      placeholder='1.5'
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Minimum profit multiplier applied on top of the fee-inclusive cost to derive the minimum safe price.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          {!enableFloorProtection && (
            <p className='text-muted-foreground text-xs'>
              {t(
                'Floor protection is off: every sync overwrites the listed price with the minimum safe price.'
              )}
            </p>
          )}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
