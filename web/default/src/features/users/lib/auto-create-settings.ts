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
import { useMemo } from 'react'
import { useSystemOptions } from '@/features/system-settings/hooks/use-system-options'
import {
  type AutoCreateUserCopyItem,
  type AutoCreateUserSettings,
} from '../types'

/**
 * In-sync mirror of operation_setting.defaultAutoCreateUserSetting() — anything
 * the server omits (e.g. a brand-new deployment that hasn't saved settings yet)
 * falls back to this set so the UI always renders something coherent.
 */
export const DEFAULT_AUTO_CREATE_USER_SETTINGS: AutoCreateUserSettings = {
  username_prefix: 'User-',
  username_suffix_length: 4,
  username_suffix_charset: 'alphanumeric',
  password_mode: 'same_as_username',
  random_password_length: 12,
  default_quota: 0,
  default_group: 'default',
  site_url: '',
  copy_templates: [
    { label: '站点', template: '{{site}}' },
    { label: '用户名', template: '{{username}}' },
    { label: '密码', template: '{{password}}' },
  ],
}

const KEY_PREFIX = 'auto_create_user_setting.'

function parseScalar<T extends string | number>(
  raw: string | undefined,
  fallback: T
): T {
  if (raw === undefined) return fallback
  if (typeof fallback === 'number') {
    const n = Number(raw)
    return (Number.isFinite(n) ? n : fallback) as T
  }
  return raw as T
}

function parseCopyTemplates(
  raw: string | undefined,
  fallback: AutoCreateUserCopyItem[]
): AutoCreateUserCopyItem[] {
  if (!raw) return fallback
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return fallback
    const cleaned: AutoCreateUserCopyItem[] = []
    for (const item of parsed) {
      if (
        item &&
        typeof item === 'object' &&
        typeof item.label === 'string' &&
        typeof item.template === 'string'
      ) {
        cleaned.push({ label: item.label, template: item.template })
      }
    }
    return cleaned
  } catch {
    return fallback
  }
}

/**
 * Resolve auto-create-user settings from the system-options response.
 * Falls back to DEFAULT_AUTO_CREATE_USER_SETTINGS for any missing key.
 */
export function useAutoCreateUserSettings(): {
  settings: AutoCreateUserSettings
  isLoading: boolean
} {
  const { data, isLoading } = useSystemOptions()

  const settings = useMemo<AutoCreateUserSettings>(() => {
    const map = new Map<string, string>()
    if (data?.data) {
      for (const { key, value } of data.data) {
        if (key.startsWith(KEY_PREFIX)) {
          map.set(key.slice(KEY_PREFIX.length), value)
        }
      }
    }
    return {
      username_prefix: parseScalar(
        map.get('username_prefix'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.username_prefix
      ),
      username_suffix_length: parseScalar(
        map.get('username_suffix_length'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.username_suffix_length
      ),
      username_suffix_charset: parseScalar(
        map.get('username_suffix_charset'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.username_suffix_charset
      ),
      password_mode: parseScalar(
        map.get('password_mode'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.password_mode
      ),
      random_password_length: parseScalar(
        map.get('random_password_length'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.random_password_length
      ),
      default_quota: parseScalar(
        map.get('default_quota'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.default_quota
      ),
      default_group: parseScalar(
        map.get('default_group'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.default_group
      ),
      site_url: parseScalar(
        map.get('site_url'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.site_url
      ),
      copy_templates: parseCopyTemplates(
        map.get('copy_templates'),
        DEFAULT_AUTO_CREATE_USER_SETTINGS.copy_templates
      ),
    }
  }, [data])

  return { settings, isLoading }
}

/**
 * Substitute {{username}} / {{password}} / {{site}} in a copy-template string.
 * Mirrors service.RenderCopyItem in the backend — unknown placeholders pass
 * through literally.
 */
export function renderCopyItem(
  template: string,
  values: { username: string; password: string; site: string }
): string {
  return template
    .split('{{username}}')
    .join(values.username)
    .split('{{password}}')
    .join(values.password)
    .split('{{site}}')
    .join(values.site)
}
