'use client';

import { zodResolver } from '@hookform/resolvers/zod';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { RAGFlowSelect, RAGFlowSelectOptionType } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { TagRenameId } from '@/constants/knowledge';
import { TenantIdContext } from '@/contexts/teant-context';
import { IModalProps } from '@/interfaces/common';
import { IGroup } from '@/interfaces/database/team';
import { useContext } from 'react';
import { useTranslation } from 'react-i18next';
import { useSelectTenantUserOptions } from '../use-select-tenant-user-options';

export function TransferOwnerForm({
  hideModal,
  onOk,
}: IModalProps<any> & { initialValues?: Partial<IGroup> }) {
  const { t } = useTranslation();

  const tenantId = useContext(TenantIdContext);
  const options: RAGFlowSelectOptionType[] = useSelectTenantUserOptions(
    tenantId,
    true,
  );

  const FormSchema = z.object({
    new_owner_id: z
      .string()
      .min(1, {
        message: t('common.namePlaceholder'),
      })
      .trim(),
    remain_admin: z.boolean(),
  });

  const form = useForm<z.infer<typeof FormSchema>>({
    resolver: zodResolver(FormSchema),
    defaultValues: { remain_admin: true },
  });

  async function onSubmit(data: z.infer<typeof FormSchema>) {
    const ret = await onOk?.(data);
    if (ret) {
      hideModal?.();
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6"
        id={TagRenameId}
      >
        <FormField
          control={form.control}
          name="new_owner_id"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('permission.transferTo')}</FormLabel>
              <FormControl>
                <RAGFlowSelect {...field} options={options}></RAGFlowSelect>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="remain_admin"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t('permission.keepAdministratorPrivileges')}
              </FormLabel>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </form>
    </Form>
  );
}
