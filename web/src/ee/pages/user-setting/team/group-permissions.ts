import {
  GroupPermission,
  GroupPermissionName,
  hasResourcePermission,
} from '@/ee/constants/team';

export type GrantablePermission = {
  value: GroupPermissionName;
  mask: GroupPermission;
  labelKey: string;
};

/**
 * Permissions that can be granted to a group member. `owner` is bound to
 * ownership transfer and is never part of a direct grant.
 */
export const GrantableGroupPermissions: readonly GrantablePermission[] = [
  {
    value: GroupPermissionName.AddSubgroup,
    mask: GroupPermission.AddSubgroup,
    labelKey: 'permission.addSubgroup',
  },
  {
    value: GroupPermissionName.DeleteSubgroup,
    mask: GroupPermission.DeleteSubgroup,
    labelKey: 'permission.deleteSubgroup',
  },
  {
    value: GroupPermissionName.AddMember,
    mask: GroupPermission.AddMember,
    labelKey: 'permission.addMember',
  },
  {
    value: GroupPermissionName.RemoveMember,
    mask: GroupPermission.RemoveMember,
    labelKey: 'permission.removeMember',
  },
  {
    value: GroupPermissionName.ConfigureMemberPermissions,
    mask: GroupPermission.ConfigureMemberPermissions,
    labelKey: 'permission.configureMemberPermissions',
  },
];

export const grantableGroupPermissions = (
  permission: number | undefined,
): GrantablePermission[] =>
  GrantableGroupPermissions.filter((option) =>
    hasResourcePermission(permission ?? 0, option.mask),
  );
