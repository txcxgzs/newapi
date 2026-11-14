/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import axios, { type AxiosRequestConfig } from 'axios'

import { requiresAuditReason } from '@/lib/admin-permissions'
import type { AuthUser } from '@/stores/auth-store'

export const MIN_AUDIT_REASON_LENGTH = 15
export const AUDIT_REASON_HEADER = 'X-Audit-Reason'

export interface AuditReasonPromptOptions {
  title?: string
  description?: string
  summary?: string
  resource?: string
  action?: string
  minLength?: number
  presets?: string[]
}

type PromptHandler = (
  options: AuditReasonPromptOptions
) => Promise<string | null>

let registeredPromptHandler: PromptHandler | null = null

export function registerAuditReasonPrompt(
  handler: PromptHandler | null
): void {
  registeredPromptHandler = handler
}

export function promptAuditReason(
  options: AuditReasonPromptOptions = {}
): Promise<string | null> {
  if (!registeredPromptHandler) {
    return Promise.resolve(null)
  }
  return registeredPromptHandler(options)
}

export async function ensureAuditReason(
  user: AuthUser | null | undefined,
  resource: string,
  action: string,
  options: Omit<AuditReasonPromptOptions, 'resource' | 'action'> = {}
): Promise<string | null> {
  if (!requiresAuditReason(user, resource, action)) {
    return ''
  }
  return promptAuditReason({
    ...options,
    resource,
    action,
  })
}

export function withAuditReasonHeaders(
  reason?: string | null,
  config: AxiosRequestConfig = {}
): AxiosRequestConfig {
  const trimmed = reason?.trim() ?? ''
  if (!trimmed) return config
  return {
    ...config,
    headers: {
      ...config.headers,
      [AUDIT_REASON_HEADER]: trimmed,
    },
  }
}

export function extractAuditReasonRequiredInfo(error: unknown): {
  required: boolean
  resource?: string
  action?: string
  minLength?: number
  message?: string
} {
  if (!axios.isAxiosError(error)) {
    return { required: false }
  }
  const data = error.response?.data as
    | {
        code?: string
        resource?: string
        action?: string
        min_length?: number
        message?: string
      }
    | undefined
  if (data?.code === 'AUDIT_REASON_REQUIRED') {
    return {
      required: true,
      resource: data.resource,
      action: data.action,
      minLength: data.min_length,
      message: data.message,
    }
  }
  return { required: false }
}
