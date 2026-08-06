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
import { useEffect, useMemo, useState } from 'react'
import { VChart } from '@visactor/react-vchart'
import { useTranslation } from 'react-i18next'
import { useChartTheme } from '@/lib/use-chart-theme'
import { VCHART_OPTION } from '@/lib/vchart'
import { getAvailabilityRpm } from '../api'

/** sparkline 保留的采样点数。 */
const MAX_POINTS = 30
/** 轮询间隔，与设计文档一致。 */
const POLL_INTERVAL_MS = 10_000

export function LiveRpmCard() {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const [history, setHistory] = useState<number[]>([])

  // 自持轮询而非 useQuery：这里要的是「把每次采样追加进历史」，
  // 只在异步回调里 setState，不产生 effect 内的同步级联渲染。
  useEffect(() => {
    let cancelled = false

    const sample = async () => {
      try {
        const rpm = await getAvailabilityRpm()
        if (cancelled) return
        setHistory((prev) => [...prev, rpm].slice(-MAX_POINTS))
      } catch {
        // 单次采样失败只是曲线缺一个点，保持轮询继续
      }
    }

    void sample()
    const timer = setInterval(sample, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [])

  const latest = history.at(-1)

  const spec = useMemo(() => {
    // 用序号而非时钟做 x 轴：轮询间隔固定，序号足以表达先后
    const values = history.map((value, index) => ({
      tick: String(index),
      value,
    }))

    return {
      type: 'area',
      data: [{ id: 'rpm', values }],
      xField: 'tick',
      yField: 'value',
      area: { style: { fillOpacity: 0.18, fill: '#4a93b5' } },
      line: { style: { lineWidth: 2, stroke: '#4a93b5' } },
      point: { visible: false },
      axes: [
        { orient: 'bottom', visible: false },
        { orient: 'left', visible: false },
      ],
      tooltip: { visible: false },
      background: { fill: 'transparent' },
      animation: false,
    }
  }, [history])

  return (
    <div
      data-testid='status-live-rpm'
      className='flex flex-col gap-2 rounded-2xl border p-4'
    >
      <div className='flex items-baseline justify-between gap-2'>
        <span className='text-muted-foreground text-xs font-medium'>
          {t('Live Requests / min')}
        </span>
        <span className='text-lg font-semibold tabular-nums'>
          {latest === undefined ? '—' : latest}
        </span>
      </div>
      <div className='h-[48px]'>
        {themeReady && history.length > 1 && (
          <VChart
            spec={{
              ...spec,
              theme: resolvedTheme === 'dark' ? 'dark' : 'light',
              background: 'transparent',
            }}
            option={VCHART_OPTION}
          />
        )}
      </div>
      <div className='text-muted-foreground text-[11px]'>
        {t('Rolling 60-second count on this node')}
      </div>
    </div>
  )
}
