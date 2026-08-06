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
/**
 * TaskStatusBadge — colored Chinese status pill for seedance video tasks.
 * Four states: 排队中 (amber) / 生成中 (blue, with percent) / 成功 (green) /
 * 失败 (red). Unknown statuses fall back to a grey pill.
 */
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

type StatusStyle = { labelKey: string; className: string }

const STATUS_STYLES: Record<string, StatusStyle> = {
  SUCCESS: {
    labelKey: 'Success',
    className:
      'border-emerald-200/60 bg-emerald-50/60 text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/30 dark:text-emerald-400',
  },
  FAILURE: {
    labelKey: 'Failed',
    className:
      'border-rose-200/60 bg-rose-50/60 text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-400',
  },
  IN_PROGRESS: {
    labelKey: 'Generating',
    className:
      'border-blue-200/60 bg-blue-50/60 text-blue-700 dark:border-blue-900/50 dark:bg-blue-950/30 dark:text-blue-400',
  },
  SUBMITTED: {
    labelKey: 'Queued',
    className:
      'border-amber-200/60 bg-amber-50/60 text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-400',
  },
  QUEUED: {
    labelKey: 'Queued',
    className:
      'border-amber-200/60 bg-amber-50/60 text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-400',
  },
}

const UNKNOWN_STYLE: StatusStyle = {
  labelKey: 'Unknown',
  className: 'border-border/60 bg-muted/40 text-muted-foreground',
}

interface TaskStatusBadgeProps {
  status: string
  /** e.g. "50%" — appended for the in-progress state */
  progress?: string
  className?: string
}

export function TaskStatusBadge(props: TaskStatusBadgeProps) {
  const { t } = useTranslation()
  const { status, progress, className } = props
  const style = STATUS_STYLES[status] || UNKNOWN_STYLE

  let label = t(style.labelKey)
  if (status === 'IN_PROGRESS' || status === 'SUBMITTED' || status === 'QUEUED') {
    const pct = parseInt(progress || '0', 10) || 0
    if (pct > 0) label = `${label} ${pct}%`
  }

  return (
    <span
      className={cn(
        'inline-flex h-6 w-fit items-center rounded-md border px-2 text-xs font-medium whitespace-nowrap tabular-nums',
        style.className,
        className
      )}
    >
      {label}
    </span>
  )
}
