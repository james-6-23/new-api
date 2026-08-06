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
import z from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { ROLE } from '@/lib/roles'
import { CostAccounting } from '@/features/cost'

const costSearchSchema = z.object({
  start: z.number().optional(),
  end: z.number().optional(),
  tab: z.enum(['users', 'models', 'channels']).optional().catch('users'),
  p: z.number().optional().catch(1),
  username: z.string().optional(),
  channel: z.number().optional(),
  model_name: z.string().optional(),
  rate: z.number().optional(),
})

export const Route = createFileRoute('/_authenticated/cost/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.SUPER_ADMIN) {
      throw redirect({
        to: '/403',
      })
    }

    // 功能开关关闭时不渲染页面骨架 —— 真正的安全边界在后端中间件，
    // 这里只是避免用户看到空表格再吃 403。
    const { config } = useSystemConfigStore.getState()
    if (config.costAccountingEnabled === false) {
      throw redirect({
        to: '/403',
      })
    }
  },
  validateSearch: costSearchSchema,
  component: CostAccounting,
})
