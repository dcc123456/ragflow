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
import { IModalProps } from '@/interfaces/common';
import { useTranslation } from 'react-i18next';
import { Input } from '../ui/input';

export default function DuplicateForm({
  id,
  hideModal,
  onOk,
  initialValues,
}: IModalProps<string> & { id?: string }) {
  const { t } = useTranslation();

  const schema = z.object({
    name: z
      .string()
      .min(1, { message: t('common.namePlaceholder') })
      .trim(),
  });

  type FormSchema = z.infer<typeof schema>;

  const form = useForm<FormSchema>({
    resolver: zodResolver(schema),
    defaultValues: { name: initialValues },
  });

  async function onSubmit(data: FormSchema) {
    const ret = await onOk?.(data.name);

    if (ret) {
      hideModal?.();
    }
  }

  return (
    <Form {...form}>
      <form
        id={id}
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6"
      >
        <FormField
          control={form.control}
          name="name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('common.name')}</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </form>
    </Form>
  );
}
