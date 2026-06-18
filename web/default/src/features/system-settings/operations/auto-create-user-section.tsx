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
import * as z from 'zod'
import { useTranslation } from 'react-i18next'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import { zodResolver } from '@hookform/resolvers/zod'
import { Button } from '@/components/ui/button'
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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * Form schema. See note in performance-section.tsx — react-hook-form
 * interprets dotted `name` strings as nested paths, so we model the form
 * with a nested object and let `useSettingsForm` flatten back to dotted
 * `auto_create_user_setting.<field>` keys at submit time.
 */
const sectionSchema = z.object({
  auto_create_user_setting: z.object({
    username_prefix: z.string(),
    username_suffix_length: z.number().int().min(1).max(32),
    username_suffix_charset: z.enum(['alphanumeric', 'digits', 'letters']),
    password_mode: z.enum(['same_as_username', 'random']),
    random_password_length: z.number().int().min(8).max(64),
    default_quota: z.number().int().min(0),
    default_group: z.string(),
    site_url: z.string(),
    // JSON-encoded copy_templates lives here as a string so the dirty-tracking
    // diff and on-the-wire payload match what the server expects exactly.
    copy_templates: z.string(),
  }),
})

type SectionFormValues = z.infer<typeof sectionSchema>

type CopyTemplateRow = { label: string; template: string }

type AutoCreateUserSectionProps = {
  defaultValues: SectionFormValues
}

function safeParseCopyTemplates(raw: string): CopyTemplateRow[] {
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter(
        (it): it is CopyTemplateRow =>
          it &&
          typeof it === 'object' &&
          typeof it.label === 'string' &&
          typeof it.template === 'string'
      )
      .map((it) => ({ label: it.label, template: it.template }))
  } catch {
    return []
  }
}

export function AutoCreateUserSection({
  defaultValues,
}: AutoCreateUserSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<SectionFormValues>({
      resolver: zodResolver(sectionSchema),
      defaultValues,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({
            key,
            value: value as string | number | boolean,
          })
        }
      },
    })

  // Watch the copy_templates JSON string and render it as an editable list.
  // We keep a local rows state in sync so the list editor doesn't have to
  // parse on every render.
  const watchedTemplatesRaw = form.watch(
    'auto_create_user_setting.copy_templates'
  )
  const [rows, setRows] = useState<CopyTemplateRow[]>(() =>
    safeParseCopyTemplates(watchedTemplatesRaw)
  )

  // Re-sync local rows if the upstream value changes (e.g. reset).
  const watchedSignature = useMemo(
    () => watchedTemplatesRaw,
    [watchedTemplatesRaw]
  )
  useEffect(() => {
    const current = JSON.stringify(rows)
    if (current !== watchedSignature) {
      setRows(safeParseCopyTemplates(watchedSignature))
    }
    // We only want this to fire when the form value changes externally, not
    // when our local edits update the rows.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [watchedSignature])

  const commitRows = (next: CopyTemplateRow[]) => {
    setRows(next)
    form.setValue(
      'auto_create_user_setting.copy_templates',
      JSON.stringify(next),
      { shouldDirty: true, shouldValidate: true, shouldTouch: true }
    )
  }

  const passwordMode = form.watch('auto_create_user_setting.password_mode')

  return (
    <SettingsSection title={t('Auto Create User Settings')}>
      <FormNavigationGuard when={isDirty} />

      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit}>
          <SettingsPageFormActions
            onSave={handleSubmit}
            isSaving={updateOption.isPending || isSubmitting}
          />
          <FormDirtyIndicator isDirty={isDirty} />

          <FormField
            control={form.control}
            name='auto_create_user_setting.username_prefix'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Username prefix')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Prepended verbatim to the random suffix. Example: "User-" → "User-AB12".'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='auto_create_user_setting.username_suffix_length'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Username suffix length')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={32}
                    value={field.value ?? 0}
                    onChange={(e) =>
                      field.onChange(parseInt(e.target.value || '0', 10))
                    }
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='auto_create_user_setting.username_suffix_charset'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Username suffix charset')}</FormLabel>
                <Select
                  items={[
                    { value: 'alphanumeric', label: t('Alphanumeric') },
                    { value: 'digits', label: t('Digits only') },
                    { value: 'letters', label: t('Letters only') },
                  ]}
                  onValueChange={(v) => v !== null && field.onChange(v)}
                  value={field.value}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value='alphanumeric'>
                        {t('Alphanumeric')}
                      </SelectItem>
                      <SelectItem value='digits'>{t('Digits only')}</SelectItem>
                      <SelectItem value='letters'>
                        {t('Letters only')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='auto_create_user_setting.password_mode'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Password mode')}</FormLabel>
                <Select
                  items={[
                    {
                      value: 'same_as_username',
                      label: t('Same as username'),
                    },
                    { value: 'random', label: t('Random') },
                  ]}
                  onValueChange={(v) => v !== null && field.onChange(v)}
                  value={field.value}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value='same_as_username'>
                        {t('Same as username')}
                      </SelectItem>
                      <SelectItem value='random'>{t('Random')}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />

          {passwordMode === 'random' && (
            <FormField
              control={form.control}
              name='auto_create_user_setting.random_password_length'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Random password length')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={8}
                      max={64}
                      value={field.value ?? 0}
                      onChange={(e) =>
                        field.onChange(parseInt(e.target.value || '0', 10))
                      }
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}

          <FormField
            control={form.control}
            name='auto_create_user_setting.default_quota'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Default quota')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    value={field.value ?? 0}
                    onChange={(e) =>
                      field.onChange(parseInt(e.target.value || '0', 10))
                    }
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Raw quota units. Leave at 0 to inherit the system-wide new-user quota at preview time.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='auto_create_user_setting.default_group'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Default group')}</FormLabel>
                <FormControl>
                  <Input {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='auto_create_user_setting.site_url'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Site URL')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    placeholder='https://example.com'
                  />
                </FormControl>
                <FormDescription>
                  {t('Used to substitute {{site}} in the copy templates.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormItem>
            <FormLabel>{t('Copy templates')}</FormLabel>
            <FormDescription>
              {t('Placeholders: {{username}}, {{password}}, {{site}}')}
            </FormDescription>
            <div className='flex flex-col gap-2 rounded-md border p-2'>
              {rows.length === 0 && (
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'No copy items are configured. Add some in System Settings → Operations → Auto Create User.'
                  )}
                </p>
              )}
              {rows.map((row, idx) => (
                <div
                  key={idx}
                  className='flex flex-col gap-2 rounded border p-2 sm:flex-row sm:items-end'
                >
                  <div className='flex-1'>
                    <FormLabel className='text-xs text-muted-foreground'>
                      {t('Label')}
                    </FormLabel>
                    <Input
                      value={row.label}
                      onChange={(e) => {
                        const next = [...rows]
                        next[idx] = { ...row, label: e.target.value }
                        commitRows(next)
                      }}
                    />
                  </div>
                  <div className='flex-[2]'>
                    <FormLabel className='text-xs text-muted-foreground'>
                      {t('Template')}
                    </FormLabel>
                    <Input
                      value={row.template}
                      onChange={(e) => {
                        const next = [...rows]
                        next[idx] = { ...row, template: e.target.value }
                        commitRows(next)
                      }}
                    />
                  </div>
                  <div className='flex gap-1'>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={idx === 0}
                      onClick={() => {
                        if (idx === 0) return
                        const next = [...rows]
                        const tmp = next[idx - 1]
                        next[idx - 1] = next[idx]
                        next[idx] = tmp
                        commitRows(next)
                      }}
                      title={t('Move up')}
                    >
                      <ArrowUp className='h-4 w-4' />
                    </Button>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={idx === rows.length - 1}
                      onClick={() => {
                        if (idx === rows.length - 1) return
                        const next = [...rows]
                        const tmp = next[idx + 1]
                        next[idx + 1] = next[idx]
                        next[idx] = tmp
                        commitRows(next)
                      }}
                      title={t('Move down')}
                    >
                      <ArrowDown className='h-4 w-4' />
                    </Button>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() => {
                        const next = rows.filter((_, i) => i !== idx)
                        commitRows(next)
                      }}
                      title={t('Remove')}
                    >
                      <Trash2 className='h-4 w-4' />
                    </Button>
                  </div>
                </div>
              ))}
              <div>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() =>
                    commitRows([...rows, { label: '', template: '' }])
                  }
                >
                  <Plus className='mr-1 h-4 w-4' />
                  {t('Add copy item')}
                </Button>
              </div>
            </div>
          </FormItem>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
