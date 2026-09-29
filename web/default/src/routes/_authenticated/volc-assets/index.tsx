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
import { createFileRoute } from '@tanstack/react-router'
import { VolcAssets } from '@/features/volc-assets'
import {
  VOLC_ASSET_STATUS,
  VOLC_RESOURCE_TYPE,
} from '@/features/volc-assets/constants'

const volcAssetsSearchSchema = z.object({
  tab: z.enum(['self', 'all']).optional().catch('self'),
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  filter: z.string().optional().catch(''),
  /** 资产 id，精确匹配。与 filter 的模糊关键字分开：客户手上拿到的就是完整 id。 */
  assetId: z.string().optional().catch(''),
  /** 创建时间范围，URL 里存毫秒，发请求前换成秒。 */
  startTime: z.number().optional().catch(undefined),
  endTime: z.number().optional().catch(undefined),
  userId: z.number().optional().catch(undefined),
  channelId: z.number().optional().catch(undefined),
  status: z
    .array(
      z.enum([
        VOLC_ASSET_STATUS.ACTIVE,
        VOLC_ASSET_STATUS.PROCESSING,
        VOLC_ASSET_STATUS.FAILED,
      ])
    )
    .optional()
    .catch([]),
  resourceType: z
    .array(z.enum([VOLC_RESOURCE_TYPE.ASSET, VOLC_RESOURCE_TYPE.ASSET_GROUP]))
    .optional()
    .catch([]),
})

// 不设 beforeLoad 角色门槛:这页对任何登录用户开放,他们只看得到自己的资产
// (后端 /api/volc_asset/self 从认证上下文取 user_id,不接受 query 覆盖)。
export const Route = createFileRoute('/_authenticated/volc-assets/')({
  validateSearch: volcAssetsSearchSchema,
  component: VolcAssets,
})
