import {
  hasEditPermission,
  hasManagePermissionPermission,
  hasManagementPermission,
  hasOwnerPermission,
  hasPreviewPermission,
  showEditButton,
} from '../permission-util';

describe('permission utility bitmasks', () => {
  test('recognizes read-only grants without treating combinations as read-only', () => {
    expect(hasPreviewPermission(1)).toBe(true);
    expect(hasPreviewPermission(1 | 4)).toBe(false);
  });

  test('checks write, manage, and owner bits in combined grants', () => {
    expect(hasEditPermission(1 | 2)).toBe(true);
    expect(hasManagementPermission(1 | 4)).toBe(true);
    expect(hasOwnerPermission(1 | 8)).toBe(true);
    expect(hasManagePermissionPermission(1 | 8)).toBe(true);
    expect(showEditButton(1 | 2)).toBe(true);
    expect(showEditButton(1)).toBe(false);
  });
});
