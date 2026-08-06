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
import type { ReactNode } from 'react'
import { HelpCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'

/**
 * A "?" marker appended to a column header, explaining on hover how that
 * column is computed. Used where the number is derived rather than raw (profit
 * rate, cost ratio, group discount) and admins would otherwise have to guess
 * the formula.
 */
export function CostHelpHover({
  label,
  children,
  contentClassName,
}: {
  label: ReactNode
  children: ReactNode
  contentClassName?: string
}) {
  const { t } = useTranslation()
  return (
    <span className='inline-flex items-center gap-1'>
      {label}
      <HoverCard>
        <HoverCardTrigger
          delay={100}
          closeDelay={80}
          tabIndex={0}
          className='inline-flex cursor-help items-center rounded outline-none focus-visible:ring-[3px]'
        >
          <HelpCircle className='text-muted-foreground size-3.5' aria-hidden />
          <span className='sr-only'>{t('How is this calculated?')}</span>
        </HoverCardTrigger>
        <HoverCardContent
          align='end'
          className={cn('w-72 text-left font-normal', contentClassName)}
        >
          {children}
        </HoverCardContent>
      </HoverCard>
    </span>
  )
}

/** One "term = expression" line inside a help hover. */
export function CostHelpFormula({
  term,
  expression,
}: {
  term: string
  expression: string
}) {
  return (
    <div className='flex flex-col gap-0.5'>
      <span className='font-medium'>{term}</span>
      <span className='text-muted-foreground tabular-nums'>{expression}</span>
    </div>
  )
}

/** Caveat bullets under the formulas — the "why doesn't this add up" answers. */
export function CostHelpNotes({ notes }: { notes: string[] }) {
  return (
    <ul className='text-muted-foreground flex list-disc flex-col gap-1 pl-4'>
      {notes.map((note) => (
        <li key={note}>{note}</li>
      ))}
    </ul>
  )
}
