import { Button } from '@/components/ui/button';
import { BellRing } from 'lucide-react';
import { TeamInvitationReminderDialog } from './team-invitation-reminder-dialog';

export function BellButton() {
  return (
    <TeamInvitationReminderDialog>
      <Button variant="ghost" size="icon" className="group" dot>
        <BellRing className="size-4 animate-bell-shake group-hover:animate-none" />
      </Button>
    </TeamInvitationReminderDialog>
  );
}
