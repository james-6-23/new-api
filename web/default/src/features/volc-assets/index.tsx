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
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { VolcAssetsDeleteDialog } from './components/volc-assets-delete-dialog'
import { VolcAssetsPrimaryButtons } from './components/volc-assets-primary-buttons'
import { VolcAssetsProvider } from './components/volc-assets-provider'
import { VolcAssetsTable } from './components/volc-assets-table'

const route = getRouteApi('/_authenticated/volc-assets/')

type VolcAssetsTab = 'self' | 'all'

/**
 * 火山透传素材库页面。
 *
 * 任何登录用户都能进，默认只看得到自己的资产（走 /api/volc_asset/self）。
 * 「全部资产」页签与同步按钮只对超管出现 —— 阈值取 ROLE.SUPER_ADMIN，因为后端
 * 那几条路由挂的是 middleware.RootAuth()：它们能看到全站客户的素材、能删上游
 * 资源，管理员权限不足以承担。前后端门槛必须一致，否则不是给人看一个点不动的
 * 页签，就是让人眼睁睁地吃 403。
 *
 * 额度不再单开页签：它是看表格时的参照物，做成筛选栏行内的徽章跟表格同屏。
 */
export function VolcAssets() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const user = useAuthStore((state) => state.auth.user)
  const isRoot = (user?.role ?? ROLE.GUEST) >= ROLE.SUPER_ADMIN

  const tab: VolcAssetsTab = isRoot
    ? search.tab === 'all'
      ? 'all'
      : 'self'
    : 'self'

  const handleTabChange = (value: string) => {
    navigate({
      search: () => ({ tab: value as VolcAssetsTab, page: 1 }),
      replace: true,
    })
  }

  return (
    <VolcAssetsProvider>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Asset Library')}</SectionPageLayout.Title>
        {isRoot && (
          <SectionPageLayout.Actions>
            <VolcAssetsPrimaryButtons />
          </SectionPageLayout.Actions>
        )}
        <SectionPageLayout.Content>
          {isRoot ? (
            <Tabs value={tab} onValueChange={handleTabChange}>
              <TabsList>
                <TabsTrigger value='self'>{t('My Assets')}</TabsTrigger>
                <TabsTrigger value='all'>{t('All Assets')}</TabsTrigger>
              </TabsList>

              <TabsContent value='self' className='mt-3'>
                <VolcAssetsTable admin={false} />
              </TabsContent>
              <TabsContent value='all' className='mt-3'>
                <VolcAssetsTable admin />
              </TabsContent>
            </Tabs>
          ) : (
            <VolcAssetsTable admin={false} />
          )}
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <VolcAssetsDeleteDialog />
    </VolcAssetsProvider>
  )
}
