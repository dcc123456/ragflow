import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { LabelMap } from '@/constants/team';
import { useFetchUserInfo } from '@/hooks/use-user-setting-request';
import { ChevronDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useCanvasPresence } from './hooks/use-canvas-presence';

export function OnlineUsers() {
  const {
    users: onlineUsers,
    onlineUserCount,
    operatorPermission,
  } = useCanvasPresence();
  const { data: currentUser } = useFetchUserInfo();
  const { t } = useTranslation();

  const displayUser = {
    user_id: currentUser?.id,
    display_name: currentUser?.nickname,
    permission: operatorPermission,
  };

  return (
    <div className="flex items-center gap-3 rounded-md border border-border-button bg-bg-card">
      <DropdownMenu>
        <DropdownMenuTrigger className="flex items-center justify-between gap-2 rounded-sm px-3 py-1.5 text-sm outline-none hover:bg-accent min-w-60">
          <div className="text-xs font-medium text-text-secondary whitespace-nowrap pl-2">
            Online {onlineUserCount}
          </div>
          {displayUser?.display_name ? (
            <div className="flex items-center gap-2">
              <RAGFlowAvatar
                avatar={currentUser?.avatar}
                isPerson
                name={displayUser.display_name}
                className="h-5 w-5"
              />
              <span>{displayUser.display_name}</span>
              <span className="rounded bg-bg-base px-1.5 py-0.5 text-xs text-text-secondary">
                {t(
                  `permission.${LabelMap[displayUser.permission as keyof typeof LabelMap]}Permission`,
                )}
              </span>
            </div>
          ) : (
            <span className="text-text-secondary">{t('common.select')}</span>
          )}
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
              <span className="rounded bg-bg-base px-1.5 py-0.5 text-xs text-text-secondary">
                {t(
                  `permission.${LabelMap[user.permission as keyof typeof LabelMap]}Permission`,
                )}
              </span>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
