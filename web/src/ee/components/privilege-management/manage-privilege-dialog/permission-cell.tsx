import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  LabelMap,
  PermissionResourceType,
  getResourcePermissionActions,
  getAssignableResourcePermissionOptions,
} from '@/ee/constants/team';
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

type PermissionCellProps = {
  permissions: Record<string, number>;
  resourceIds: string[];
  resourceType: PermissionResourceType;
  editable: boolean;
  canGrantManage: boolean;
  onChange: (permission: number) => void;
};

export function PermissionCell({
  permissions,
  resourceIds,
  resourceType,
  editable,
  canGrantManage,
  onChange,
}: PermissionCellProps) {
  const { t } = useTranslation();

  const permissionActions = useMemo(
    () =>
      resourceIds.map(
        (resourceId) =>
          getResourcePermissionActions(permissions[resourceId] ?? 0)[0],
      ),
    [permissions, resourceIds],
  );
  const uniqueActions = useMemo(
    () => [...new Set(permissionActions)],
    [permissionActions],
  );
  const selectedAction = uniqueActions.length === 1 ? uniqueActions[0] : null;
  const label = selectedAction
    ? t(`permission.${LabelMap[selectedAction]}`)
    : t('permission.mixedPermission');

  const handleSelectPermission = useCallback(
    (permission: number) => () => onChange(permission),
    [onChange],
  );

  if (!editable) {
    return (
      <Button
        type="button"
        variant="ghost"
        className="h-8 w-fit max-w-[140px] justify-start truncate px-2"
        disabled
      >
        {label}
      </Button>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          className="h-8 w-fit max-w-[140px] justify-start truncate px-2"
        >
          {label}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        {getAssignableResourcePermissionOptions(
          resourceType,
          canGrantManage,
        ).map(({ action, value }) => {
          return (
            <DropdownMenuItem
              key={action}
              onClick={handleSelectPermission(value)}
            >
              {t(`permission.${LabelMap[action]}`)}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
