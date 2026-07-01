import { Badge } from '@/components/ui/badge';
import { LabelMap, TeamRole } from '@/constants/team';
import { IPermission } from '@/interfaces/database/team';
import { cn } from '@/lib/utils';
import { useTranslation } from 'react-i18next';
import { getPermission } from './utils';

type PrivilegeLabelProps = {
  permissions?: IPermission['permissions'];
};

export function PrivilegeLabel({ permissions }: PrivilegeLabelProps) {
  const { t } = useTranslation();
  const permission = getPermission(permissions);
  return typeof permission === 'number' && permission !== 0 ? (
    <Badge
      variant="secondary"
      className={cn('h-7 rounded-full border-0 px-3 shadow-none', {
        'bg-sky-500/10 text-sky-700 dark:text-sky-300': permission === 1,
        'bg-amber-500/10 text-amber-700 dark:text-amber-300': permission === 2,
        'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300':
          permission === 4,
        'bg-violet-500/10 text-violet-700 dark:text-violet-300':
          permission === 7,
      })}
    >
      {t(
        `permission.${LabelMap[permission as keyof typeof LabelMap]}Permission`,
      )}
    </Badge>
  ) : (
    <span></span>
  );
}

export function UserTypeLabel({ role }: { role: string }) {
  const { t } = useTranslation();
  return (
    <Badge
      className={cn(
        'h-7 rounded-full border-0 px-3 font-medium text-white shadow-none',
        {
          'bg-team-member': role === TeamRole.Member,
          'bg-team-group': role === TeamRole.Group,
          'bg-team-department': role === TeamRole.Department,
        },
      )}
    >
      {t(`permission.${role}`)}
    </Badge>
  );
}
