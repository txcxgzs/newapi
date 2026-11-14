/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { ShieldAlert } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  type AuditReasonPromptOptions,
  MIN_AUDIT_REASON_LENGTH,
  registerAuditReasonPrompt,
} from '@/lib/audit-reason'

const DEFAULT_PRESETS = [
  '客户工单核查处理：核实用户反馈的额度、权限或配置异常问题',
  '日常运维巡检调优：例行检查渠道稳定性、模型路由与连接参数',
  '财务结算打款核验：核对商家提现金额、收款二维码与入账流水',
  '商家审核与合规核查：核验商家资质、网站运行信息与分组定价',
]

export function AuditReasonDialogHost() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [options, setOptions] = useState<AuditReasonPromptOptions>({})
  const [reason, setReason] = useState('')
  const resolverRef = useRef<((value: string | null) => void) | null>(null)

  const minLength = options.minLength ?? MIN_AUDIT_REASON_LENGTH
  const trimmedLen = [...reason.trim()].length
  const isValid = trimmedLen >= minLength

  const handleClose = useCallback((result: string | null) => {
    setOpen(false)
    const resolve = resolverRef.current
    resolverRef.current = null
    if (resolve) resolve(result)
  }, [])

  useEffect(() => {
    registerAuditReasonPrompt((nextOptions) => {
      if (resolverRef.current) {
        resolverRef.current(null)
      }
      setOptions(nextOptions)
      setReason('')
      setOpen(true)
      return new Promise<string | null>((resolve) => {
        resolverRef.current = resolve
      })
    })
    return () => {
      registerAuditReasonPrompt(null)
      if (resolverRef.current) {
        resolverRef.current(null)
        resolverRef.current = null
      }
    }
  }, [])

  const presets =
    options.presets && options.presets.length > 0
      ? options.presets
      : DEFAULT_PRESETS

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) handleClose(null)
      }}
    >
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <ShieldAlert className='h-5 w-5 text-amber-600' />
            <span>
              {options.title || t('Sensitive Operation Audit Reason')}
            </span>
          </DialogTitle>
          <DialogDescription>
            {options.description ||
              t(
                'This action is audit-tracked. Please enter the business reason or ticket reference before proceeding.'
              )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-1'>
          {options.summary || options.resource ? (
            <div className='rounded-lg border border-amber-200 bg-amber-50/70 px-3 py-2 text-xs text-amber-950 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-200'>
              {options.summary ? (
                <div className='font-medium'>{options.summary}</div>
              ) : null}
              {options.resource && options.action ? (
                <div className='mt-1 flex items-center gap-1.5 text-[11px] opacity-85'>
                  <span>{t('Permission Scope')}:</span>
                  <Badge variant='outline' className='font-mono text-[10px]'>
                    {options.resource}:{options.action}
                  </Badge>
                </div>
              ) : null}
            </div>
          ) : null}

          <div className='space-y-2'>
            <div className='flex items-center justify-between'>
              <Label htmlFor='audit-reason-textarea'>
                {t('Operation Purpose & Audit Reason')}
              </Label>
              <span
                className={`text-xs ${isValid ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'}`}
              >
                {t('{{count}} / {{min}} chars minimum', {
                  count: trimmedLen,
                  min: minLength,
                })}
              </span>
            </div>
            <Textarea
              id='audit-reason-textarea'
              rows={4}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t(
                'Please describe why this operation is needed (at least 15 characters, e.g. ticket ID, merchant request, troubleshooting scope)...'
              )}
              autoFocus
            />
          </div>

          <div className='space-y-1.5'>
            <div className='text-xs text-muted-foreground'>
              {t('Quick Templates')}
            </div>
            <div className='flex flex-wrap gap-1.5'>
              {presets.map((preset) => (
                <Button
                  key={preset}
                  type='button'
                  variant='outline'
                  size='sm'
                  className='h-auto py-1 text-[11px]'
                  onClick={() => setReason(preset)}
                >
                  {preset.split('：')[0]}
                </Button>
              ))}
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleClose(null)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={!isValid}
            onClick={() => handleClose(reason.trim())}
          >
            {t('Confirm & Proceed')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
