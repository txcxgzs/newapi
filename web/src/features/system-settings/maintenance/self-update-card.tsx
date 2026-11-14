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
  CheckCircle2,
  Download,
  ExternalLink,
  GitBranch,
  RefreshCw,
} from 'lucide-react'
import { useCallback, useEffect, useId, useState } from 'react'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import {
  getServerErrorMessage,
  requireServerSuccess,
} from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

type SelfUpdateReleaseInfo = {
  tag_name: string
  name: string
  html_url: string
  published_at: string
  prerelease: boolean
  asset_name?: string
  asset_url?: string
  asset_size?: number
  checksum_url?: string
}

type SelfUpdateStatusData = {
  repo: string
  branch: string
  current_version: string
  goos: string
  goarch: string
  latest_release: SelfUpdateReleaseInfo | null
  release_update_available: boolean
  git_available: boolean
  local_branch?: string
  local_commit?: string
  remote_commit?: string
  remote_commit_url?: string
  remote_commit_message?: string
  remote_commit_date?: string
  git_update_available: boolean
  warnings: string[]
}

type SelfUpdateApplyData = {
  mode: 'release' | 'git'
  previous_version?: string
  updated_version?: string
  asset_name?: string
  backup_path?: string
  restarting?: boolean
  old_commit?: string
  new_commit?: string
  message: string
}

type ApiEnvelope<T> = {
  success: boolean
  message?: string
  data?: T
}

function formatByteSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return ''
  const mb = bytes / (1024 * 1024)
  if (mb >= 1) return `${mb.toFixed(1)} MB`
  return `${Math.max(1, Math.round(bytes / 1024))} KB`
}

function formatShortTime(iso?: string): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString()
}

function renderReleaseStatusBadge(status: SelfUpdateStatusData) {
  if (!status.latest_release) {
    return <Badge variant='outline'>暂无 Release</Badge>
  }
  if (status.release_update_available) {
    return <Badge variant='default'>发现新版本</Badge>
  }
  return <Badge variant='secondary'>已是最新</Badge>
}

function renderGitStatusBadge(status: SelfUpdateStatusData) {
  if (!status.git_available) {
    return <Badge variant='outline'>非 Git 运行目录</Badge>
  }
  if (status.git_update_available) {
    return <Badge variant='default'>可拉取新提交</Badge>
  }
  return <Badge variant='secondary'>源码已同步</Badge>
}

export function SelfUpdateCard() {
  const isSuperAdmin = useAuthStore(
    (state) => (state.auth.user?.role ?? 0) >= ROLE.SUPER_ADMIN
  )
  if (!isSuperAdmin) return null
  return <RootSelfUpdateCard />
}

function RootSelfUpdateCard() {
  const restartSwitchId = useId()
  const [status, setStatus] = useState<SelfUpdateStatusData | null>(null)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [autoRestart, setAutoRestart] = useState(false)
  const [confirmMode, setConfirmMode] = useState<'release' | 'git' | null>(null)
  const [applyingMode, setApplyingMode] = useState<'release' | 'git' | null>(
    null
  )
  const [lastResult, setLastResult] = useState<SelfUpdateApplyData | null>(null)

  const fetchStatus = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      const res = await api.get<ApiEnvelope<SelfUpdateStatusData>>(
        '/api/system_update/status'
      )
      const payload = requireServerSuccess(res.data)
      if (payload.data) {
        setStatus(payload.data)
      }
    } catch (error) {
      setLoadError(getServerErrorMessage(error, '获取定制分支更新状态失败'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchStatus()
  }, [fetchStatus])

  async function handleConfirmApply() {
    if (!confirmMode) return
    const mode = confirmMode
    setApplyingMode(mode)
    try {
      const res = await api.post<ApiEnvelope<SelfUpdateApplyData>>(
        '/api/system_update/apply',
        mode === 'release'
          ? { mode: 'release', restart: autoRestart }
          : { mode: 'git' }
      )
      const payload = requireServerSuccess(res.data)
      setConfirmMode(null)
      if (payload.data) {
        setLastResult(payload.data)
        toast.success(payload.data.message || '更新执行成功')
        if (!payload.data.restarting) {
          void fetchStatus()
        }
      }
    } catch (error) {
      handleServerError(error, '执行分支更新失败')
    } finally {
      setApplyingMode(null)
    }
  }

  const repoSlug = status?.repo || 'txcxgzs/newapi'
  const branchName = status?.branch || 'main'
  const repoUrl = `https://github.com/${repoSlug}`
  const latestRelease = status?.latest_release
  const canApplyRelease = Boolean(latestRelease?.asset_url)

  return (
    <div className='space-y-4 rounded-lg border p-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div className='space-y-1'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='font-semibold'>定制分支一键更新</span>
            <a
              href={repoUrl}
              target='_blank'
              rel='noopener noreferrer'
              className='text-muted-foreground hover:text-foreground inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-xs transition-colors'
            >
              <GitBranch className='size-3' />
              <span>
                {repoSlug} ({branchName})
              </span>
              <ExternalLink className='size-3' />
            </a>
          </div>
          <p className='text-muted-foreground text-xs'>
            基于当前 Fork 仓库独立检测与更新，保持与官方上游低耦合；上方官方更新提示仅作参考。
          </p>
        </div>

        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => void fetchStatus()}
          disabled={loading || applyingMode !== null}
        >
          {loading ? (
            <Spinner data-icon='inline-start' aria-hidden='true' />
          ) : (
            <RefreshCw className='size-4' data-icon='inline-start' />
          )}
          检查分支更新
        </Button>
      </div>

      {loadError && (
        <div className='border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-xs'>
          {loadError}
        </div>
      )}

      {status && (
        <div className='grid gap-3 md:grid-cols-2'>
          <div className='space-y-2 rounded-md border p-3'>
            <div className='flex items-center justify-between gap-2'>
              <span className='text-muted-foreground text-xs font-medium'>
                Release 二进制产物 ({status.goos}/{status.goarch})
              </span>
              {renderReleaseStatusBadge(status)}
            </div>

            {latestRelease ? (
              <div className='space-y-1 text-xs'>
                <div className='flex items-center justify-between gap-2'>
                  <span className='font-mono text-sm font-semibold'>
                    {latestRelease.tag_name}
                  </span>
                  {latestRelease.html_url && (
                    <a
                      href={latestRelease.html_url}
                      target='_blank'
                      rel='noopener noreferrer'
                      className='text-primary inline-flex items-center gap-1 hover:underline'
                    >
                      查看详情
                      <ExternalLink className='size-3' />
                    </a>
                  )}
                </div>
                <div className='text-muted-foreground break-all'>
                  {latestRelease.asset_name ? (
                    <>
                      产物：{latestRelease.asset_name}
                      {latestRelease.asset_size
                        ? ` (${formatByteSize(latestRelease.asset_size)})`
                        : ''}
                    </>
                  ) : (
                    '未匹配到当前系统架构的二进制产物'
                  )}
                </div>
                {latestRelease.published_at && (
                  <div className='text-muted-foreground'>
                    发布时间：{formatShortTime(latestRelease.published_at)}
                  </div>
                )}
              </div>
            ) : (
              <div className='text-muted-foreground text-xs'>
                推送版本 Tag（如 <span className='font-mono'>v1.0.0</span>
                ）触发 GitHub Actions Release 后，即可在此直接下载替换二进制。
              </div>
            )}
          </div>

          <div className='space-y-2 rounded-md border p-3'>
            <div className='flex items-center justify-between gap-2'>
              <span className='text-muted-foreground text-xs font-medium'>
                分支最新源码 ({branchName})
              </span>
              {renderGitStatusBadge(status)}
            </div>

            <div className='space-y-1 text-xs'>
              <div className='flex flex-wrap items-center gap-2'>
                <span>
                  运行版本：
                  <span className='font-mono font-medium'>
                    {status.current_version}
                  </span>
                </span>
                {status.local_commit && (
                  <span className='text-muted-foreground font-mono'>
                    本地 {status.local_branch || 'HEAD'}@{status.local_commit}
                  </span>
                )}
              </div>

              {status.remote_commit ? (
                <div className='text-muted-foreground space-y-0.5'>
                  <div className='flex items-center gap-1.5'>
                    <span>远程最新：</span>
                    {status.remote_commit_url ? (
                      <a
                        href={status.remote_commit_url}
                        target='_blank'
                        rel='noopener noreferrer'
                        className='text-primary font-mono hover:underline'
                      >
                        {status.remote_commit}
                      </a>
                    ) : (
                      <span className='font-mono'>{status.remote_commit}</span>
                    )}
                    {status.remote_commit_date && (
                      <span>
                        ({formatShortTime(status.remote_commit_date)})
                      </span>
                    )}
                  </div>
                  {status.remote_commit_message && (
                    <div className='truncate'>
                      {status.remote_commit_message}
                    </div>
                  )}
                </div>
              ) : (
                <div className='text-muted-foreground'>暂未获取到远程提交信息</div>
              )}
            </div>
          </div>
        </div>
      )}

      {status && status.warnings.length > 0 && (
        <div className='bg-muted/50 text-muted-foreground space-y-1 rounded-md border px-3 py-2 text-xs'>
          {status.warnings.map((warning) => (
            <div key={warning}>{warning}</div>
          ))}
        </div>
      )}

      {lastResult && (
        <div className='flex items-start gap-2 rounded-md border border-emerald-500/40 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-300'>
          <CheckCircle2 className='mt-0.5 size-4 shrink-0' />
          <span>{lastResult.message}</span>
        </div>
      )}

      <div className='flex flex-col gap-3 border-t pt-3 sm:flex-row sm:items-center sm:justify-between'>
        <label
          htmlFor={restartSwitchId}
          className='flex cursor-pointer items-center gap-2 text-xs select-none'
        >
          <Switch
            id={restartSwitchId}
            checked={autoRestart}
            onCheckedChange={setAutoRestart}
            disabled={applyingMode !== null}
          />
          <span className='text-muted-foreground'>
            更新二进制后自动退出进程（Docker / Systemd 托管时自动拉起）
          </span>
        </label>

        <div className='flex flex-wrap items-center gap-2'>
          {status?.git_available && (
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setConfirmMode('git')}
              disabled={applyingMode !== null}
            >
              {applyingMode === 'git' ? (
                <Spinner data-icon='inline-start' aria-hidden='true' />
              ) : (
                <GitBranch className='size-4' data-icon='inline-start' />
              )}
              一键同步源码 (Git Pull)
            </Button>
          )}

          <Button
            type='button'
            size='sm'
            onClick={() => setConfirmMode('release')}
            disabled={!canApplyRelease || applyingMode !== null}
          >
            {applyingMode === 'release' ? (
              <Spinner data-icon='inline-start' aria-hidden='true' />
            ) : (
              <Download className='size-4' data-icon='inline-start' />
            )}
            一键更新二进制 (Release)
          </Button>
        </div>
      </div>

      <AlertDialog
        open={confirmMode !== null}
        onOpenChange={(open) => {
          if (!open && applyingMode === null) {
            setConfirmMode(null)
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirmMode === 'release'
                ? '确认下载并替换当前 Release 二进制？'
                : '确认拉取 Git 源码最新提交？'}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirmMode === 'release'
                ? `将从 ${repoSlug} 下载 ${latestRelease?.tag_name || ''} (${latestRelease?.asset_name || ''}) 并校验 SHA-256 后备份原程序为 .bak 进行替换。${
                    autoRestart
                      ? '替换完成后服务将退出进程等待守护进程重启。'
                      : '替换完成后需手动重启服务进程使新版本生效。'
                  }`
                : `将从 https://github.com/${repoSlug}.git 拉取 ${branchName} 分支并执行快进合并（git merge --ff-only）。若有本地未提交冲突会自动中止；拉取后需重新编译/重启生效。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={applyingMode !== null}>
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => void handleConfirmApply()}
              disabled={applyingMode !== null}
            >
              {applyingMode !== null ? '正在执行...' : '确认执行'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
