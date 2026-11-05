import { hasPreviewPermission } from '@/ee/utils/permission-util';

export function useMcpReadOnly(operatorPermission?: number) {
  return hasPreviewPermission(operatorPermission ?? 0);
}
