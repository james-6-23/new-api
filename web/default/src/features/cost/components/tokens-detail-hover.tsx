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
import { useTranslation } from 'react-i18next'
import { Info } from 'lucide-react'
import { formatNumber } from '@/lib/format'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'

interface TokensDetailHoverProps {
  promptTokens: number
  completionTokens: number
  cacheReadTokens: number
  cacheCreationTokens: number
  totalTokens: number
}

function TokenRow({ label, value }: { label: string; value: number }) {
  return (
    <div className='flex items-center justify-between gap-6 py-1.5 text-sm'>
      <span className='text-muted-foreground'>{label}</span>
      <span className='tabular-nums'>{formatNumber(value)}</span>
    </div>
  )
}

/**
 * Breaks a row's `total_tokens` down into its four raw components on hover —
 * a small info marker next to the number, no click or dialog needed. The four
 * buckets are disjoint (input excludes cache), so they sum to the total.
 */
export function TokensDetailHover({
  promptTokens,
  completionTokens,
  cacheReadTokens,
  cacheCreationTokens,
  totalTokens,
}: TokensDetailHoverProps) {
  const { t } = useTranslation()

  return (
    <HoverCard>
      <HoverCardTrigger
        delay={100}
        closeDelay={80}
        // Focusable so keyboard users get the same breakdown; the hover card
        // primitive opens on focus as well as hover.
        tabIndex={0}
        className='inline-flex cursor-help items-center gap-1 rounded tabular-nums outline-none focus-visible:ring-[3px]'
      >
        {formatNumber(totalTokens)}
        <Info className='text-muted-foreground size-3' aria-hidden />
        <span className='sr-only'>{t('Total Tokens')}</span>
      </HoverCardTrigger>
      <HoverCardContent className='w-56'>
        <div className='flex flex-col divide-y'>
          <TokenRow label={t('Uncached Input Tokens')} value={promptTokens} />
          <TokenRow label={t('Output Tokens')} value={completionTokens} />
          <TokenRow label={t('Cache Read Tokens')} value={cacheReadTokens} />
          <TokenRow
            label={t('Cache Creation Tokens')}
            value={cacheCreationTokens}
          />
        </div>
      </HoverCardContent>
    </HoverCard>
  )
}
