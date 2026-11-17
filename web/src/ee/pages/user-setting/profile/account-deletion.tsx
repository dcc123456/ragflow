import { useFetchUserInfo } from '@/hooks/use-user-setting-request';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ChangeEvent, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useDeleteAccount } from './use-delete-account';

export default function AccountDeletion() {
  const { t } = useTranslation();
  const { data: user } = useFetchUserInfo();
  const email = user.email || '';
  const [open, setOpen] = useState(false);
  const [confirmation, setConfirmation] = useState('');
  const confirmationId = useId();
  const { mutate, isPending, error } = useDeleteAccount();

  function handleOpenChange(nextOpen: boolean) {
    if (!isPending) {
      setOpen(nextOpen);
      setConfirmation('');
    }
  }

  function handleOpen() {
    handleOpenChange(true);
  }

  function handleCancel() {
    handleOpenChange(false);
  }

  function handleConfirmation(event: ChangeEvent<HTMLInputElement>) {
    setConfirmation(event.target.value);
  }

  function handleDelete() {
    if (email !== '' && confirmation === email && !isPending) {
      mutate(confirmation);
    }
  }

  return (
    <section className="space-y-3 border-t border-border-button pt-6">
      <h3 className="text-sm font-medium">{t('setting.deleteAccount')}</h3>
      <p className="text-sm text-text-secondary break-words">
        {t('setting.deleteAccountDescription')}
      </p>
      <Button variant="destructive" onClick={handleOpen} disabled={isPending}>
        {t('setting.deleteAccount')}
      </Button>
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('setting.deleteAccount')}</DialogTitle>
            <DialogDescription>
              {t('setting.deleteAccountDescription')}
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-text-secondary break-words">
            {t('setting.deleteAccountWarning')}
          </p>
          <Label htmlFor={confirmationId}>
            {t('setting.deleteAccountConfirmation', { email })}
          </Label>
          <Input
            id={confirmationId}
            value={confirmation}
            onChange={handleConfirmation}
            autoComplete="off"
            disabled={isPending}
          />
          {error && (
            <p role="alert" className="text-sm text-state-error">
              {error.message}
            </p>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={handleCancel}
              disabled={isPending}
            >
              {t('common.cancel')}
            </Button>
            <Button
              variant="destructive"
              loading={isPending}
              disabled={!email || confirmation !== email}
              onClick={handleDelete}
            >
              {t('setting.deleteAccount')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
