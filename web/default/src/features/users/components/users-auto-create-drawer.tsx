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
import { useCallback, useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { RefreshCw, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'
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
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { createUser, getAutoCreatePreview } from '../api'
import { useUsers } from './users-provider'

const autoCreateSchema = z.object({
  username: z.string().min(1, 'Username is required'),
  password: z.string().min(8, 'Password must be at least 8 characters'),
  group: z.string().min(1, 'Group is required'),
  /**
   * Held in the display unit (dollars/tokens depending on the QuotaDisplayType
   * system option). We translate to raw quota units on submit so the backend
   * sees the same number it would have if entered through the standard
   * "Add User" flow.
   *
   * Plain `z.number()` (not `coerce.number()`) — the <Input> below already
   * does `parseFloat` in its onChange, and `coerce.number()` here would split
   * Zod's input/output types and break react-hook-form's `Control<TFieldValues>` inference.
   */
  quota_dollars: z.number().min(0, 'Quota cannot be negative'),
})

type AutoCreateFormValues = z.infer<typeof autoCreateSchema>

type UsersAutoCreateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UsersAutoCreateDrawer({
  open,
  onOpenChange,
}: UsersAutoCreateDrawerProps) {
  const { t } = useTranslation()
  const { setOpen, setCredentials, triggerRefresh } = useUsers()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const currencyLabel = getCurrencyLabel()

  const form = useForm<AutoCreateFormValues>({
    resolver: zodResolver(autoCreateSchema),
    defaultValues: {
      username: '',
      password: '',
      group: 'default',
      quota_dollars: 0,
    },
  })

  /**
   * Server-driven suggestion. Refetched on every drawer open AND on demand via
   * the "Refresh suggestion" button.
   */
  const {
    data: preview,
    isFetching: isPreviewLoading,
    refetch: refetchPreview,
    error: previewError,
  } = useQuery({
    queryKey: ['auto-create-user-preview'],
    queryFn: getAutoCreatePreview,
    enabled: open,
    refetchOnWindowFocus: false,
    staleTime: 0, // always fresh — each open gives the admin a NEW suggestion
    gcTime: 0,
  })

  // Populate the form whenever a fresh preview arrives.
  useEffect(() => {
    if (!preview) return
    if (!preview.success || !preview.data) {
      // Surface the translated server message in the form area so the admin
      // sees WHY the suggestion didn't arrive (e.g. 5x collision).
      const msg = preview.message || t('Failed to fetch suggestion')
      toast.error(msg)
      return
    }
    const data = preview.data
    form.reset({
      username: data.username,
      password: data.password,
      group: data.group,
      quota_dollars: quotaUnitsToDollars(data.quota),
    })
  }, [preview, form, t])

  useEffect(() => {
    if (previewError) {
      toast.error(t('Failed to fetch suggestion'))
    }
  }, [previewError, t])

  const handleRefresh = useCallback(() => {
    refetchPreview()
  }, [refetchPreview])

  const onSubmit = async (values: AutoCreateFormValues) => {
    setIsSubmitting(true)
    try {
      const quotaUnits = parseQuotaFromDollars(values.quota_dollars)
      const result = await createUser({
        username: values.username.trim(),
        password: values.password,
        display_name: values.username.trim(),
        role: 1, // Common user — matches the existing "Add User" default.
        group: values.group,
        quota: quotaUnits,
      })
      if (!result.success) {
        toast.error(result.message || t('Failed to create user'))
        return
      }
      // Stash the credentials so the next dialog can render the copy template.
      setCredentials({
        username: values.username.trim(),
        password: values.password,
        group: values.group,
        quota: quotaUnits,
      })
      triggerRefresh()
      onOpenChange(false)
      setOpen('credentials')
      toast.success(t('User created'))
    } catch {
      toast.error(t('Failed to create user'))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) form.reset()
      }}
    >
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[600px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle className='flex items-center gap-2'>
            <Sparkles className='h-4 w-4' />
            {t('Auto Create User')}
          </SheetTitle>
          <SheetDescription>
            {t(
              'A username, password, group and quota are pre-filled using your auto-create settings. Edit anything before confirming.'
            )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='auto-create-user-form'
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            <SideDrawerSection>
              <div className='flex items-center justify-between'>
                <h3 className='text-sm font-medium'>{t('Suggestion')}</h3>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  onClick={handleRefresh}
                  disabled={isPreviewLoading || isSubmitting}
                >
                  <RefreshCw
                    className={
                      'mr-1 h-4 w-4 ' +
                      (isPreviewLoading ? 'animate-spin' : '')
                    }
                  />
                  {t('Refresh suggestion')}
                </Button>
              </div>

              <FormField
                control={form.control}
                name='username'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Username')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder={t('Enter username')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='password'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Password')}</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        placeholder={t('Enter password (min 8 characters)')}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Visible to you only on the next screen. Make sure to copy it before closing.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='group'
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
                name='quota_dollars'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Default quota ({{currency}})', {
                        currency: currencyLabel,
                      })}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='any'
                        min={0}
                        value={field.value ?? 0}
                        onChange={(e) =>
                          field.onChange(parseFloat(e.target.value || '0'))
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button
            form='auto-create-user-form'
            type='submit'
            disabled={isSubmitting || isPreviewLoading}
          >
            {isSubmitting ? t('Saving...') : t('Confirm and create')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
