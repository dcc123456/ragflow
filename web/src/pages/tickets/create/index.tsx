import { FileUploader } from '@/components/file-uploader';
import { SelectWithSearch } from '@/components/originui/select-with-search';
import { RAGFlowFormItem } from '@/components/ragflow-form';
import { Button } from '@/components/ui/button';
import { Form } from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { useEnterpriseNavigate } from '@/hooks/use-enterprise-navigate';
import {
  useCreateTicket,
  useFetchTicketGroups,
} from '@/hooks/use-ticket-request';
import { useFetchUserInfo } from '@/hooks/use-user-setting-request';
import { fileToBase64 } from '@/utils/file-util';
import { zodResolver } from '@hookform/resolvers/zod';
import { ArrowLeft } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

export default function CreateTicketPage() {
  const { t } = useTranslation();
  const { navigateToTickets } = useEnterpriseNavigate();
  const { createTicket, loading } = useCreateTicket();
  const { data: userInfo } = useFetchUserInfo();
  const { groups: ticketGroups } = useFetchTicketGroups();
  const [files, setFiles] = useState<File[]>([]);

  const CreateTicketFormSchema = z.object({
    title: z.string().min(1).trim(),
    group: z.string().min(1).trim(),
    customerName: z.string().min(1).trim(),
    customer: z.string().min(1).trim(),
    articleSubject: z.string().trim(),
    articleBody: z.string().min(1).trim(),
  });

  type CreateTicketForm = z.infer<typeof CreateTicketFormSchema>;

  const form = useForm<CreateTicketForm>({
    resolver: zodResolver(CreateTicketFormSchema),
    defaultValues: {
      title: '',
      group: '',
      customerName: '',
      customer: '',
      articleSubject: '',
      articleBody: '',
    },
  });

  useEffect(() => {
    if (userInfo?.email) {
      form.setValue('customer', userInfo.email);
    }
    if (userInfo?.nickname) {
      form.setValue('customerName', userInfo.nickname);
    }
  }, [userInfo?.email, userInfo?.nickname, form]);

  useEffect(() => {
    if (ticketGroups.length > 0) {
      const defaultGroup =
        ticketGroups.find((g) => g.name === 'Users') ?? ticketGroups[0];
      if (defaultGroup) {
        form.setValue('group', defaultGroup.name);
      }
    }
  }, [ticketGroups, form]);

  const onSubmit = async (values: CreateTicketForm) => {
    const attachments = await Promise.all(
      files.map(async (file) => ({
        filename: file.name,
        data: await fileToBase64(file),
        'mime-type': file.type || 'application/octet-stream',
      })),
    );

    await createTicket({
      title: values.title,
      group: values.group,
      customer: values.customer,
      article: {
        subject: values.articleSubject,
        body: values.articleBody,
        type: 'web',
        internal: false,
      },
      attachments: attachments.length > 0 ? attachments : undefined,
    });
    navigateToTickets();
  };

  return (
    <section className="p-8 w-full h-full flex flex-col">
      <div className="flex items-center gap-2 mb-6">
        <Button variant="ghost" size="sm" onClick={navigateToTickets}>
          <ArrowLeft className="size-4" />
        </Button>
        <h1 className="text-2xl font-bold">{t('tickets.createTicket')}</h1>
      </div>

      <Form {...form}>
        <form
          id="create-ticket-form"
          onSubmit={form.handleSubmit(onSubmit)}
          className="space-y-6 flex-1 overflow-auto"
        >
          <RAGFlowFormItem
            name="title"
            label={t('tickets.form.title')}
            required
          >
            <Input placeholder={t('tickets.form.titlePlaceholder')} />
          </RAGFlowFormItem>

          <RAGFlowFormItem
            name="group"
            label={t('tickets.form.group')}
            required
          >
            {(field) => (
              <SelectWithSearch
                options={ticketGroups.map((g) => ({
                  label: g.name,
                  value: g.name,
                }))}
                value={field.value}
                onChange={field.onChange}
                placeholder={t('tickets.form.group')}
              />
            )}
          </RAGFlowFormItem>

          <RAGFlowFormItem
            name="customerName"
            label={t('tickets.form.customerName')}
            required
          >
            <Input
              disabled
              placeholder={t('tickets.form.customerNamePlaceholder')}
            />
          </RAGFlowFormItem>

          <RAGFlowFormItem
            name="customer"
            label={t('tickets.form.customer')}
            required
          >
            <Input
              disabled
              placeholder={t('tickets.form.customerPlaceholder')}
            />
          </RAGFlowFormItem>

          <RAGFlowFormItem
            name="articleSubject"
            label={t('tickets.form.articleSubject')}
          >
            <Input placeholder={t('tickets.form.articleSubjectPlaceholder')} />
          </RAGFlowFormItem>

          <RAGFlowFormItem
            name="articleBody"
            label={t('tickets.form.articleBody')}
            required
          >
            <Textarea
              className="min-h-[120px]"
              placeholder={t('tickets.form.articleBodyPlaceholder')}
            />
          </RAGFlowFormItem>

          <div>
            <label className="text-sm font-medium mb-1 block">
              {t('tickets.form.attachments')}
            </label>
            <FileUploader
              value={files}
              onValueChange={setFiles}
              maxFileCount={5}
              accept={{ '*/*': [] }}
              title={t('tickets.form.uploadAttachment')}
              // className="h-32"
            />
          </div>
        </form>
      </Form>
      <div className="flex justify-end gap-3 pt-4">
        <Button type="button" variant="outline" onClick={navigateToTickets}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" form="create-ticket-form" loading={loading}>
          {t('common.submit')}
        </Button>
      </div>
    </section>
  );
}
