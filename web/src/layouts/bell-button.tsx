import { Button } from '@/components/ui/button';
import { useListTenant } from '@/hooks/use-user-setting-request';
import { TenantRole } from '@/pages/user-setting/constants';
import { BellRing } from 'lucide-react';
import { useMemo } from 'react';
import { TeamInvitationReminderDialog } from './components/team-invitation-reminder-dialog';

export function BellButton() {
  const { data } = useListTenant();

  const showBell = useMemo(() => {
    return data.some((x) => x.role === TenantRole.Invite);
  }, [data]);

  return showBell ? (
    <TeamInvitationReminderDialog>
      <Button variant={'ghost'}>
        <div className="relative">
          <BellRing className="size-4 " />
          <span className="absolute size-1 rounded -right-1 -top-1 bg-red-600"></span>
        </div>
      </Button>
    </TeamInvitationReminderDialog>
  ) : null;
}
