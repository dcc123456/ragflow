import { Badge } from '@/components/ui/badge';
import { LabelMap, PermissionResourceType } from '@/constants/team';
import { IPermission } from '@/interfaces/database/team';
import { cn } from '@/lib/utils';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { getPermission } from '../utils';

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
    if (p === 1) {
      return 'bg-sky-500/10 text-sky-700 dark:text-sky-300';
    }
    if (p === 2) {
      return 'bg-amber-500/10 text-amber-700 dark:text-amber-300';
    }
    if (p === 4) {
      return 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
    }
    if (p === 7) {
      return 'bg-violet-500/10 text-violet-700 dark:text-violet-300';
    }
    return 'bg-bg-card text-text-secondary';
  }, []);

  const renderLabel = useCallback(
    (p: number) => {
      return (
        <Badge
          variant="secondary"
          className={cn(
            'h-7 rounded-full border-0 px-3 font-medium shadow-none',
            getPermissionClassName(p),
          )}
        >
          {t(`permission.${LabelMap[p as keyof typeof LabelMap]}Permission`)}
        </Badge>
      );
    },
    [getPermissionClassName, t],
  );

  if (resourceType === PermissionResourceType.Document) {
    return (
      <section className="flex w-full flex-wrap items-center justify-center gap-2 text-center">
        {Object.entries(buildDocumentPermissions(permissions)).map(
          ([permission, resourceIds]) => (
            <div key={permission} className="flex items-center gap-2">
              {renderLabel(Number(permission))}
              <span className="inline-flex min-w-6 items-center justify-center rounded-full bg-bg-card px-2 py-1 text-xs font-medium text-text-secondary">
                {resourceIds.length}
              </span>
            </div>
          ),
        )}
      </section>
    );
  }

  const permission = getPermission(permissions);
  return permission !== undefined ? renderLabel(permission) : null;
}
