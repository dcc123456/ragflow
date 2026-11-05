import { Permission, hasResourcePermission } from '@/ee/constants/team';

export function hasPreviewPermission(operatorPermission: number) {
  return (
    hasResourcePermission(operatorPermission, Permission.Read) &&
    !hasResourcePermission(operatorPermission, Permission.Write) &&
    !hasResourcePermission(operatorPermission, Permission.Manage) &&
    !hasResourcePermission(operatorPermission, Permission.Owner)
  );
}

export function hasEditPermission(operatorPermission: number) {
  return (
    hasResourcePermission(operatorPermission, Permission.Write) ||
    hasResourcePermission(operatorPermission, Permission.Owner)
  );
}

export function hasManagementPermission(operatorPermission: number) {
  return hasResourcePermission(operatorPermission, Permission.Manage);
}

export function hasOwnerPermission(operatorPermission: number) {
  return hasResourcePermission(operatorPermission, Permission.Owner);
}

export function showEditButton(operatorPermission: number) {
  return hasEditPermission(operatorPermission);
}

export function disableEditButton(operatorPermission: number) {
  return !hasEditPermission(operatorPermission);
}

export function hasManagePermissionPermission(operatorPermission: number) {
  return (
    hasManagementPermission(operatorPermission) ||
    hasOwnerPermission(operatorPermission)
  );
}
