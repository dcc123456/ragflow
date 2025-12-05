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
import { Input } from '@/components/ui/input';
import { TagRenameId } from '@/constants/knowledge';
import { IModalProps } from '@/interfaces/common';
import { IDepartment } from '@/interfaces/database/team';
import {
  getBase64FromFileList,
  transformBase64ToFileWithPreview,
} from '@/utils/file-util';
import { omit } from 'lodash';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { AvatarUploader } from '../../avatar-uploader';

export function CreateDepartmentForm({
  hideModal,
  onOk,
  initialValues,
}: IModalProps<any> & {
  initialValues?: Partial<IDepartment>;
}) {
  const { t } = useTranslation();
  const FormSchema = z.object({
    name: z
      .string()
      .min(1, {
        message: t('common.namePlaceholder'),
      })
      .trim(),
    description: z.string().trim().optional(),
    avatar: z.array(z.instanceof(File)).optional(),
  });

  const form = useForm<z.infer<typeof FormSchema>>({
    resolver: zodResolver(FormSchema),
    defaultValues: { name: '' },
  });

  async function onSubmit(data: z.infer<typeof FormSchema>) {
    const avatarStr = await getBase64FromFileList(data.avatar);
    const ret = await onOk?.({ ...data, avatar: avatarStr });
    if (ret) {
      hideModal?.();
    }
  }

  useEffect(() => {
    if (initialValues) {
      const nextValues: z.infer<typeof FormSchema> = omit(
        initialValues as IDepartment,
        'avatar',
      );
      const avatar = initialValues.avatar;
      if (avatar) {
        const file = transformBase64ToFileWithPreview(avatar);
        nextValues.avatar = [file];
      }
      form.reset(nextValues);
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
        <AvatarUploader></AvatarUploader>
        <FormField
          control={form.control}
          name="description"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('permission.description')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  placeholder={t('permission.descriptionPlaceholder')}
                  autoComplete="off"
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
