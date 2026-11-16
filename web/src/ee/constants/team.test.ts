import {
  GroupPermission,
  GroupPermissionName,
  Permission,
  getResourcePermissionActions,
  hasResourceOwnerPermission,
  hasResourcePermission,
} from '@/ee/constants/team';

describe('resource permission masks', () => {
  test('maps each resource action to its bit', () => {
    expect(Permission).toMatchObject({
      Read: 1,
      Write: 2,
      Manage: 4,
      Owner: 8,
    });
  });

  test('displays a combined write grant as one permission level', () => {
    expect(
      getResourcePermissionActions(Permission.Read | Permission.Write),
    ).toEqual([Permission.Write]);
    expect(
      getResourcePermissionActions(
        Permission.Read | Permission.Write | Permission.Manage,
      ),
    ).toEqual([Permission.Manage]);
  });

  test('treats owner as its own permission and detects it in a grant map', () => {
    expect(getResourcePermissionActions(Permission.Owner)).toEqual([
      Permission.Owner,
    ]);
    expect(hasResourceOwnerPermission({ resource: Permission.Read })).toBe(
      false,
    );
    expect(
      hasResourceOwnerPermission({
        resource: Permission.Read | Permission.Owner,
      }),
    ).toBe(true);
  });

  test('checks individual actions by bit rather than exact numeric equality', () => {
    const mask = Permission.Read | Permission.Manage;
    expect(hasResourcePermission(mask, Permission.Read)).toBe(true);
    expect(hasResourcePermission(mask, Permission.Manage)).toBe(true);
    expect(hasResourcePermission(mask, Permission.Write)).toBe(false);
  });
});

describe('group permission masks', () => {
  test('mirrors the backend bit values', () => {
    expect(GroupPermission).toMatchObject({
      AddSubgroup: 1,
      DeleteSubgroup: 2,
      AddMember: 4,
      RemoveMember: 8,
      ConfigureMemberPermissions: 16,
      Owner: 32,
    });
  });

  test('exposes the wire names the API accepts', () => {
    expect(GroupPermissionName).toMatchObject({
      AddSubgroup: 'add_subgroup',
      DeleteSubgroup: 'delete_subgroup',
      AddMember: 'add_member',
      RemoveMember: 'remove_member',
      ConfigureMemberPermissions: 'configure_member_permissions',
      Owner: 'owner',
    });
  });

  test('checks a group permission by bit', () => {
    const mask = GroupPermission.AddMember | GroupPermission.Owner;
    expect(hasResourcePermission(mask, GroupPermission.AddMember)).toBe(true);
    expect(hasResourcePermission(mask, GroupPermission.RemoveMember)).toBe(
      false,
    );
  });
});
