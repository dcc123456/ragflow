import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { PropsWithChildren } from 'react';
import { useTranslation } from 'react-i18next';
import TenantTable from '@/ee/layouts/components/team-invitation-reminder-dialog/tenant-table';

export function TeamInvitationReminderDialog({ children }: PropsWithChildren) {
  const { t } = useTranslation();

  return (
    <Dialog>
      <DialogTrigger asChild>{children}</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('permission.teamInvitation')}</DialogTitle>
        </DialogHeader>
        <TenantTable></TenantTable>
      </DialogContent>
    </Dialog>
  );
}
