import { zodResolver } from '@hookform/resolvers/zod';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import { RAGFlowFormItem } from '@/components/ragflow-form';
import { Form } from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Modal } from '@/components/ui/modal/modal';

interface AddLicenseDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (licenseKey: string) => void;
  isSubmitting?: boolean;
}

export function AddLicenseDialog({
  open,
  onOpenChange,
  onConfirm,
  isSubmitting = false,
}: AddLicenseDialogProps) {
  const { t } = useTranslation();

  const formSchema = z.object({
    licenseKey: z.string().min(1, {
      message: t('license.licenseKeyRequired', 'License key is required'),
    }),
  });

  type FormValues = z.infer<typeof formSchema>;

  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      licenseKey: '',
    },
  });

  const handleConfirm = form.handleSubmit((values) => {
    onConfirm(values.licenseKey);
    form.reset();
  });

  const handleCancel = () => {
    form.reset();
    onOpenChange(false);
  };

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title={t('license.addLicense', 'Add license')}
      size="small"
      onOk={handleConfirm}
      onCancel={handleCancel}
      okText={t('common.confirm', 'Confirm')}
      cancelText={t('common.cancel', 'Cancel')}
      confirmLoading={isSubmitting}
      disabled={isSubmitting}
    >
      <Form {...form}>
        <form className="py-4">
          <RAGFlowFormItem
            name="licenseKey"
            label={t('license.licenseKey', 'License key')}
            required
          >
            <Input
              placeholder={t('license.licenseKeyPlaceholder', 'License key')}
            />
          </RAGFlowFormItem>
        </form>
      </Form>
    </Modal>
  );
}
