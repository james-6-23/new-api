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
import { z } from 'zod'
import { useForm, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const schema = z.object({
  enabled: z.boolean(),
  defaultAssetLimit: z.coerce.number().int().min(0),
  countAssetGroups: z.boolean(),
})

type Values = z.infer<typeof schema>

/**
 * 火山素材库的每账号资产条数上限。
 *
 * 上游没有查询素材配额的接口，真实上限只能在火山控制台的「配额管理」里看到、
 * 走「配额中心 → 申请配额」提额，所以这里的数字必须由站点管理员手工填。
 * 单个用户的提额不在这里改，走用户管理的个人覆盖值。
 */
export function VolcAssetSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    enabled: boolean
    defaultAssetLimit: number
    countAssetGroups: boolean
  }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: {
      enabled: defaultValues.enabled,
      defaultAssetLimit: defaultValues.defaultAssetLimit,
      countAssetGroups: defaultValues.countAssetGroups,
    },
  })

  const { isDirty, isSubmitting } = form.formState
  const enabled = form.watch('enabled')

  async function onSubmit(values: Values) {
    const updates: Array<{ key: string; value: string }> = []

    if (values.enabled !== defaultValues.enabled) {
      updates.push({
        key: 'volc_asset_setting.enabled',
        value: String(values.enabled),
      })
    }

    if (values.defaultAssetLimit !== defaultValues.defaultAssetLimit) {
      updates.push({
        key: 'volc_asset_setting.default_asset_limit',
        value: String(values.defaultAssetLimit),
      })
    }

    if (values.countAssetGroups !== defaultValues.countAssetGroups) {
      updates.push({
        key: 'volc_asset_setting.count_asset_groups',
        value: String(values.countAssetGroups),
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset(values)
  }

  return (
    <SettingsSection title={t('Asset Library Limit')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save asset library settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enforce asset limit')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Reject new asset creation once a user reaches the limit. When off, the limit is only shown as a reference.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={updateOption.isPending || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='defaultAssetLimit'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Default assets per account')}</FormLabel>
                <FormControl>
                  <Input type='number' min={0} placeholder='100' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Applies to every user without a personal override. 0 means unlimited. Check the upstream console quota page before raising it.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          {enabled && (
            <FormField
              control={form.control}
              name='countAssetGroups'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Count asset groups')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Upstream counts assets and asset groups separately. Turn this on only if you want both to share one limit.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={updateOption.isPending || isSubmitting}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          )}
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
