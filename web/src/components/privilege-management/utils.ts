import { PermissionResourceType, TeamRole } from '@/constants/team';
import { CollaboratorItem } from './interface';

export function filterCollaboratorByTeamRole(
  list: CollaboratorItem[],
  role: TeamRole,
) {
  return list.filter((x) => x.role === role).map((x) => x.id);
}

export function hidePermissionDropdownButton(
  resourceType?: PermissionResourceType,
) {
  return (
    (resourceType || PermissionResourceType.KnowledgeBase) ===
    PermissionResourceType.LLM
  );
}

export function hideEditPermissionDropdownItem(
  resourceType?: PermissionResourceType,
) {
  return resourceType === PermissionResourceType.Dialog;
}

export function mapCollaboratorId(list: CollaboratorItem[]) {
  return list.map((x) => x.id);
}
