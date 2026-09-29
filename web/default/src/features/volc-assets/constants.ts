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
import { type StatusBadgeProps } from '@/components/status-badge'

/** 账本里的两类资源,与后端 model.VolcResourceType* 对齐。 */
export const VOLC_RESOURCE_TYPE = {
  ASSET: 'asset',
  ASSET_GROUP: 'asset_group',
} as const

export const VOLC_RESOURCE_TYPE_VALUES = Object.values(VOLC_RESOURCE_TYPE)

/** 官方素材状态枚举。Processing 是上传后转码中,此时还不能用 asset:// 引用。 */
export const VOLC_ASSET_STATUS = {
  ACTIVE: 'Active',
  PROCESSING: 'Processing',
  FAILED: 'Failed',
} as const

export const VOLC_ASSET_STATUSES: Record<
  string,
  Pick<StatusBadgeProps, 'variant'> & { labelKey: string }
> = {
  [VOLC_ASSET_STATUS.ACTIVE]: { variant: 'success', labelKey: 'Active' },
  [VOLC_ASSET_STATUS.PROCESSING]: {
    variant: 'warning',
    labelKey: 'Processing',
  },
  [VOLC_ASSET_STATUS.FAILED]: { variant: 'danger', labelKey: 'Failed' },
}

export const VOLC_ASSET_TYPES: Record<string, string> = {
  Image: 'Image',
  Video: 'Video',
  Audio: 'Audio',
}

export function getVolcResourceTypeOptions(t: (key: string) => string) {
  return [
    { label: t('Asset'), value: VOLC_RESOURCE_TYPE.ASSET },
    { label: t('Asset Group'), value: VOLC_RESOURCE_TYPE.ASSET_GROUP },
  ]
}

export function getVolcAssetStatusOptions(t: (key: string) => string) {
  return Object.entries(VOLC_ASSET_STATUSES).map(([value, meta]) => ({
    label: t(meta.labelKey),
    value,
  }))
}

/** 同步认领无主行时写入的 user_id,与后端 model.VolcAssetOrphanOwnerId 对齐。 */
export const VOLC_ASSET_ORPHAN_OWNER_ID = 0

export const ERROR_MESSAGES = {
  LOAD_FAILED: 'Failed to load assets',
  QUOTA_LOAD_FAILED: 'Failed to load asset quota',
  SYNC_FAILED: 'Failed to sync assets',
  DELETE_FAILED: 'Failed to delete asset',
} as const

export const SUCCESS_MESSAGES = {
  ASSET_DELETED: 'Asset deleted successfully',
  SYNC_COMPLETED: 'Asset sync completed',
  COPY_SUCCESS: 'Copied to clipboard',
} as const
