import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

import { Button } from '@/components/ui/button';
import { IModalProps } from '@/interfaces/common';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { SyncFileForm } from './sync-file-form';

export function SyncFileDialog({
  visible,
  hideModal,
  onOk,
  loading,
}: IModalProps<any>) {
  const formId = useId();
  const { t } = useTranslation();

  return (
    <Dialog
      modal
      open={visible}
      onOpenChange={(value) => {
        if (!loading && !value) {
          hideModal?.();
        }
      }}
    >
      <DialogContent
        closeDisabled={loading}
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>{t('fileManager.importData')}</DialogTitle>
        </DialogHeader>

        <SyncFileForm
          id={formId}
          loading={loading}
          hideModal={hideModal}
          onOk={async (data) => {
            if (await onOk?.(data)) {
              hideModal?.();
            }
          }}
        />

        <DialogFooter>
          <Button variant="transparent" onClick={hideModal} disabled={loading}>
            {t('common.cancel')}
          </Button>

          <Button type="submit" form={formId} loading={loading}>
            {t('common.sync')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
