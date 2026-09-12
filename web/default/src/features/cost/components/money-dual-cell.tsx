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
import { cn } from '@/lib/utils'
import { resolveDualMoney, type MoneyPrimaryCurrency } from '../lib'

interface MoneyDualCellProps {
  /** The backend's single figure, already in the display currency. */
  amount: number | null | undefined
  primary: MoneyPrimaryCurrency
  /** Filter rate, used only to derive the secondary line. */
  exchangeRate: number
  /** Applied to the primary (top) line only, e.g. profit's green/red sign coloring. */
  primaryClassName?: string
  className?: string
}

/**
 * Two-line money display shared by the cost report's KPI cards, dimension
 * table cells, and breakdown sub-rows: a primary line in the admin-configured
 * display currency (系统设置 → 运营设置 → 额度展示类型) and a muted, smaller
 * secondary line in the other currency, derived with the filter's rate.
 */
export function MoneyDualCell({
  amount,
  primary,
  exchangeRate,
  primaryClassName,
  className,
}: MoneyDualCellProps) {
  const { primary: primaryText, secondary: secondaryText } = resolveDualMoney(
    amount,
    primary,
    exchangeRate
  )
  return (
    <div
      className={cn(
        'flex flex-col items-end leading-tight whitespace-nowrap tabular-nums',
        className
      )}
    >
      <span className={primaryClassName}>{primaryText}</span>
      <span className='text-muted-foreground text-[11px]'>
        {secondaryText}
      </span>
    </div>
  )
}
