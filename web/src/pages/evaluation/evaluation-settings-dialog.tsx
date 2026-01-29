'use client';

import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useTranslation } from 'react-i18next';
import { EvaluationSettingsForm } from './evaluation-settings-form';

interface EvaluationSettingsDialogProps {
  hideModal: () => void;
}

export function EvaluationSettingsDialog({
  hideModal,
}: EvaluationSettingsDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[600px]">
        <DialogHeader>
          <DialogTitle>{t('evaluation.settings')}</DialogTitle>
        </DialogHeader>
        <EvaluationSettingsForm />
        <DialogFooter>
          <Button type="button" onClick={hideModal}>
            {t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
