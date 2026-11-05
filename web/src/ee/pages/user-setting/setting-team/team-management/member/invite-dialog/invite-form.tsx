'use client';

import { zodResolver } from '@hookform/resolvers/zod';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { RAGFlowFormItem } from '@/components/ragflow-form';
import { Form } from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { TagRenameId } from '@/constants/knowledge';
import { IModalProps } from '@/interfaces/common';
import { useTranslation } from 'react-i18next';

export function InviteForm({ hideModal, onOk }: IModalProps<any>) {
  const { t } = useTranslation();
  const FormSchema = z.object({
    name: z
      .string()
      .email()
      .min(1, {
        message: t('common.namePlaceholder'),
      })
      .trim(),
  });

  const form = useForm<z.infer<typeof FormSchema>>({
    resolver: zodResolver(FormSchema),
    defaultValues: { name: '' },
  });

  async function onSubmit(data: z.infer<typeof FormSchema>) {
    const ret = await onOk?.(data.name);
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
        <RAGFlowFormItem name="name" label={t('login.emailLabel')}>
          <Input placeholder={t('login.emailPlaceholder')} autoComplete="off" />
        </RAGFlowFormItem>
      </form>
    </Form>
  );
}
