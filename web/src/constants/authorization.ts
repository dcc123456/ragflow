/**
 * LocalStorage key constants for authentication.
 * Regular users and admin users have separate key sets to ensure session isolation.
 */

// Regular user authentication keys
export const Authorization = 'Authorization';
export const Token = 'token';
export const UserInfo = 'userInfo';

// Admin user authentication keys (separate from regular user keys)
export const AdminAuthorization = 'AdminAuthorization';
export const AdminToken = 'AdminToken';
export const AdminUserInfo = 'AdminUserInfo';
