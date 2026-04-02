import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { LabelMap } from '@/constants/team';
import { ChevronDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useCanvasPresence } from './hooks/use-canvas-presence';

export function OnlineUsers() {
  const { users: onlineUsers } = useCanvasPresence();
  const { t } = useTranslation();

  return (
    <div className="flex items-center gap-3 rounded-md">
      <DropdownMenu>
        <DropdownMenuTrigger className="flex items-center gap-2 rounded-sm px-3 py-1.5 text-sm outline-none ">
          <span className="h-2 w-2 rounded-full bg-state-success" />
          Online
          <ChevronDown className="h-4 w-4 text-text-secondary" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="min-w-60">
          {onlineUsers.map((user) => (
            <DropdownMenuItem
              key={user.user_id}
              className="flex items-center gap-2 cursor-pointer justify-between"
            >
              <div className="flex items-center gap-2">
                <RAGFlowAvatar
                  avatar={undefined}
                  isPerson
                  name={user.display_name}
                  className="h-5 w-5"
                />
                <span>{user.display_name}</span>
              </div>
              <Button variant={'secondary'}>
                {t(
                  `permission.${LabelMap[user.permission as keyof typeof LabelMap]}Permission`,
                )}
              </Button>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
