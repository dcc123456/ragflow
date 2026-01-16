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
    <span className="px-2 rounded text-accent-primary">
      {t(
        `permission.${LabelMap[permission as keyof typeof LabelMap]}Permission`,
      )}
    </span>
  ) : (
    <span></span>
  );
}

export function UserTypeLabel({ role }: { role: string }) {
  const { t } = useTranslation();
  return (
    <span
      className={cn('text-white rounded text-xs px-2 py-1', {
        'bg-team-member': role === TeamRole.Member,
        'bg-team-group': role === TeamRole.Group,
        'bg-team-department': role === TeamRole.Department,
      })}
    >
      {t(`permission.${role}`)}
    </span>
  );
}
