import { Permission } from '@/constants/team';

export function hasPreviewPermission(operatorPermission: number) {
  return operatorPermission === Permission.Read;
}

export function hasEditPermission(operatorPermission: number) {
  return operatorPermission === Permission.Write;
}

export function hasManagementPermission(operatorPermission: number) {
  return operatorPermission === Permission.Manage;
}

export function hasOwnerPermission(operatorPermission: number) {
  return operatorPermission === Permission.Owner;
}

export function showEditButton(operatorPermission: number) {
  return !hasPreviewPermission(operatorPermission);
}

export function disableEditButton(operatorPermission: number) {
  return hasPreviewPermission(operatorPermission);
}

export function hasManagePermissionPermission(operatorPermission: number) {
  return (
    operatorPermission === Permission.Owner ||
    operatorPermission === Permission.Manage
  );
}
