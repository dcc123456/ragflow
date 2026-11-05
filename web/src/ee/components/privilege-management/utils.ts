import { TeamRole } from '@/ee/constants/team';
import { CollaboratorItem } from '@/ee/components/privilege-management/interface';

export function filterCollaboratorByTeamRole(
  list: CollaboratorItem[],
  role: TeamRole,
) {
  return list.filter((x) => x.role === role).map((x) => x.id);
}

export function mapCollaboratorId(list: CollaboratorItem[]) {
  return list.map((x) => x.id);
}
