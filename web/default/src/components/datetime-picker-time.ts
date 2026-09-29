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
 * 时间输入框(`<input type='time'>`)的值与 Date 之间的换算。
 * 抽成独立模块以便用测试覆盖 —— 仓库无 jsdom/testing-library。
 */

/** 把 Date 的时间部分格式化为 `HH:mm` 或 `HH:mm:ss`。 */
export function formatTimeValue(date: Date, withSeconds: boolean): string {
  const pad = (n: number) => n.toString().padStart(2, '0')
  const base = `${pad(date.getHours())}:${pad(date.getMinutes())}`
  return withSeconds ? `${base}:${pad(date.getSeconds())}` : base
}

/** 解析 `HH:mm` 或 `HH:mm:ss`;非法输入一律降级为 0,避免写出 NaN 时间。 */
export function parseTimeValue(value: string): {
  hours: number
  minutes: number
  seconds: number
} {
  const [h, m, s] = value.split(':').map((part) => Number.parseInt(part, 10))
  const safe = (n: number) => (Number.isFinite(n) ? n : 0)
  return { hours: safe(h), minutes: safe(m), seconds: safe(s) }
}
