import { showEditButton } from '@/ee/utils/permission-util';

export function canEditAgent(operatorPermission: number) {
  return showEditButton(operatorPermission);
}
