import {
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

  test('returns all actions enabled by a combined mask', () => {
    expect(
      getResourcePermissionActions(Permission.Read | Permission.Write),
    ).toEqual([Permission.Read, Permission.Write]);
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
