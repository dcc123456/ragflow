import { LabelMap, PermissionResourceType } from '@/constants/team';
import { IPermission } from '@/interfaces/database/team';
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

  const renderLabel = useCallback(
    (p: number) => {
      return (
        <span className="text-text-secondary">
          {t(`permission.${LabelMap[p as keyof typeof LabelMap]}Permission`)}
        </span>
      );
    },
    [t],
  );

  if (resourceType === PermissionResourceType.Document) {
    return (
      <section className="flex gap-2 items-center">
        {Object.entries(buildDocumentPermissions(permissions)).map(
          ([permission, resourceIds]) => (
            <div key={permission}>
              {renderLabel(Number(permission))}
              <span className="ml-2 rounded bg-border-button text-text-secondary p-0.5 min-w-5 inline-block text-center text-xs">
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
