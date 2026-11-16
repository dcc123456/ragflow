import { Badge } from '@/components/ui/badge';
import {
  LabelMap,
  Permission,
  TeamRole,
  getResourcePermissionActions,
} from '@/ee/constants/team';
import { IPermission } from '@/ee/interfaces/database/team';
import { cn } from '@/lib/utils';
import { useTranslation } from 'react-i18next';

type PrivilegeLabelProps = {
  permissions?: IPermission['permissions'];
};

export function PrivilegeLabel({ permissions }: PrivilegeLabelProps) {
  const { t } = useTranslation();
  const permissionMasks = [...new Set(Object.values(permissions ?? {}))];
  const actions = permissionMasks.flatMap(getResourcePermissionActions);
  const uniqueActions = [...new Set(actions)];
  if (permissionMasks.length === 0) return null;
  if (uniqueActions.length === 0) {
    return (
      <Badge variant="secondary" className="h-7 rounded-full border-0 px-3">
        {t('permission.unknownPermission', { value: permissionMasks[0] })}
      </Badge>
    );
  }

  return (
    <div className="flex flex-wrap gap-1">
      {uniqueActions.map((action) => (
        <Badge
          key={action}
          variant="secondary"
          className={cn('h-7 rounded-full border-0 px-3 shadow-none', {
            'bg-sky-500/10 text-sky-700 dark:text-sky-300':
              action === Permission.Read,
            'bg-amber-500/10 text-amber-700 dark:text-amber-300':
              action === Permission.Write,
            'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300':
              action === Permission.Manage,
            'bg-violet-500/10 text-violet-700 dark:text-violet-300':
              action === Permission.Owner,
          })}
        >
          {t(`permission.${LabelMap[action]}`)}
        </Badge>
      ))}
    </div>
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
