import { hasManagePermissionPermission } from '@/ee/utils/permission-util';

export function useCanManageDocument(operatorPermission?: number) {
  return hasManagePermissionPermission(operatorPermission ?? 0);
}
