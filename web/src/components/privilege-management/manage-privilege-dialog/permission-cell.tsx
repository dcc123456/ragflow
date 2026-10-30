import { Badge } from '@/components/ui/badge';
import {
  LabelMap,
  Permission,
  PermissionResourceType,
  getResourcePermissionActions,
} from '@/constants/team';
import { IPermission } from '@/interfaces/database/team';
import { cn } from '@/lib/utils';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';

type PermissionCellProps = Pick<IPermission, 'permissions'> & {
  resourceType: PermissionResourceType;
};

function buildDocumentPermissions(permissions: IPermission['permissions']) {
  return Object.entries(permissions).reduce<Record<number, string[]>>(
    (pre, [resourceId, permission]) => {
      if (permission in pre) {
        pre[permission].push(resourceId);
      } else {
        pre[permission] = [resourceId];
      }

      return pre;
    },
    {},
  );
}

export function PermissionCell({
  resourceType,
  permissions,
}: PermissionCellProps) {
  const { t } = useTranslation();

  const getPermissionClassName = useCallback((p: number) => {
    if (p === Permission.Read) {
      return 'bg-sky-500/10 text-sky-700 dark:text-sky-300';
    }
    if (p === Permission.Write) {
      return 'bg-amber-500/10 text-amber-700 dark:text-amber-300';
    }
    if (p === Permission.Manage) {
      return 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
    }
    if (p === Permission.Owner) {
      return 'bg-violet-500/10 text-violet-700 dark:text-violet-300';
    }
    return 'bg-bg-card text-text-secondary';
  }, []);

  const renderLabels = useCallback(
    (permission: number) => {
      const actions = getResourcePermissionActions(permission);
      if (actions.length === 0) {
        return (
          <Badge variant="secondary" className="h-7 rounded-full border-0 px-3">
            {t('permission.unknownPermission', { value: permission })}
          </Badge>
        );
      }
      return actions.map((action) => (
        <Badge
          key={action}
          variant="secondary"
          className={cn(
            'h-7 rounded-full border-0 px-3 font-medium shadow-none',
            getPermissionClassName(action),
          )}
        >
          {t(`permission.${LabelMap[action]}Permission`)}
        </Badge>
      ));
    },
    [getPermissionClassName, t],
  );

  if (resourceType === PermissionResourceType.Document) {
    return (
      <section className="flex w-full flex-wrap items-center justify-center gap-2 text-center">
        {Object.entries(buildDocumentPermissions(permissions)).map(
          ([permission, resourceIds]) => (
            <div key={permission} className="flex items-center gap-2">
              <div className="flex flex-wrap gap-1">
                {renderLabels(Number(permission))}
              </div>
              <span className="inline-flex min-w-6 items-center justify-center rounded-full bg-bg-card px-2 py-1 text-xs font-medium text-text-secondary">
                {resourceIds.length}
              </span>
            </div>
          ),
        )}
      </section>
    );
  }

  const permissionMasks = [...new Set(Object.values(permissions))];
  return permissionMasks.length ? (
    <section className="flex w-full flex-wrap items-center justify-center gap-2 text-center">
      {permissionMasks.flatMap((permission) => renderLabels(permission))}
    </section>
  ) : null;
}
