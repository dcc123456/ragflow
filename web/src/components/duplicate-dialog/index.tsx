import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { IModalProps } from '@/interfaces/common';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, ButtonLoading } from '../ui/button';
import DuplicateForm from './duplicate-form';

export default function DuplicateDialog({
  hideModal,
  initialName,
  onOk,
  loading,
}: IModalProps<any> & { initialName?: string }) {
  const formId = useId();
  const { t } = useTranslation();

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{t('knowledgeList.duplicateModal.title')}</DialogTitle>
        </DialogHeader>

        <DialogDescription>
          {t('knowledgeList.duplicateModal.description')}
        </DialogDescription>

        <DuplicateForm
          id={formId}
          initialValues={initialName}
          hideModal={hideModal}
          onOk={onOk}
        />

        <DialogFooter>
          <Button type="button" variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>

          <ButtonLoading type="submit" form={formId} loading={loading}>
            {t('common.confirm')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
