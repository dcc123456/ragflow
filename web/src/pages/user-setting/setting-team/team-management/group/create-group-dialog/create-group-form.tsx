'use client';

import { zodResolver } from '@hookform/resolvers/zod';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { AvatarUpload } from '@/components/avatar-upload';
import { RAGFlowFormItem } from '@/components/ragflow-form';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { TagRenameId } from '@/constants/knowledge';
import { IModalProps } from '@/interfaces/common';
import { IGroup } from '@/interfaces/database/team';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';

export function CreateGroupForm({
  initialValues,
  hideModal,
  onOk,
}: IModalProps<any> & { initialValues?: IGroup }) {
  const { t } = useTranslation();
  const FormSchema = z.object({
    name: z
      .string()
      .min(1, {
        message: t('common.namePlaceholder'),
      })
      .trim(),
    avatar: z.string().nullish(),
  });

  const form = useForm<z.infer<typeof FormSchema>>({
    resolver: zodResolver(FormSchema),
    defaultValues: { name: '' },
  });

  async function onSubmit(data: z.infer<typeof FormSchema>) {
    const nextData = { ...data, group_id: initialValues?.group_id };
    const ret = await onOk?.(nextData);
    if (ret) {
      hideModal?.();
    }
  }

  useEffect(() => {
    if (initialValues) {
      form.reset(initialValues);
    }
  }, [form, initialValues]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6"
        id={TagRenameId}
      >
        <FormField
          control={form.control}
          name="name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('common.name')}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t('common.namePlaceholder')}
                  {...field}
                  autoComplete="off"
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <RAGFlowFormItem name="avatar" label={t('permission.avatar')}>
          <AvatarUpload></AvatarUpload>
        </RAGFlowFormItem>
      </form>
    </Form>
  );
}
