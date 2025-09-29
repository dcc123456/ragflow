import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { PropsWithChildren } from 'react';
import TenantTable from './tenant-table';

export function TeamInvitationReminderDialog({ children }: PropsWithChildren) {
  return (
    <Dialog>
      <DialogTrigger asChild>{children}</DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Team invitation reminder</DialogTitle>
        </DialogHeader>
        <TenantTable></TenantTable>
      </DialogContent>
    </Dialog>
  );
}
