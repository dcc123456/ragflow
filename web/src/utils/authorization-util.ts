import {
  Authorization,
  Token,
  UserInfo,
  AdminAuthorization,
  AdminToken,
  AdminUserInfo,
} from '@/constants/authorization';
import { getSearchValue } from './common-util';
const KeySet = [Authorization, Token, UserInfo];
const AdminKeySet = [AdminAuthorization, AdminToken, AdminUserInfo];

const storage = {
  getAuthorization: () => {
    return localStorage.getItem(Authorization);
  },
  getToken: () => {
    return localStorage.getItem(Token);
  },
  getUserInfo: () => {
    return localStorage.getItem(UserInfo);
  },
  getUserInfoObject: () => {
    const userInfoStr = localStorage.getItem(UserInfo);
    return userInfoStr ? JSON.parse(userInfoStr) : null;
  },
  setAuthorization: (value: string) => {
    localStorage.setItem(Authorization, value);
  },
  setToken: (value: string) => {
    localStorage.setItem(Token, value);
  },
  setUserInfo: (value: string | Record<string, unknown>) => {
    const valueStr = typeof value !== 'string' ? JSON.stringify(value) : value;
    localStorage.setItem(UserInfo, valueStr);
  },
  setItems: (pairs: Record<string, string>) => {
    Object.entries(pairs).forEach(([key, value]) => {
      localStorage.setItem(key, value);
    });
  },
  removeAuthorization: () => {
    localStorage.removeItem(Authorization);
  },
  removeAll: () => {
    KeySet.forEach((x) => {
      localStorage.removeItem(x);
    });
  },
  setLanguage: (lng: string) => {
    localStorage.setItem('lng', lng);
  },
  getLanguage: (): string => {
    return localStorage.getItem('lng') as string;
  },
};

/**
 * Storage utility for admin user authentication.
 * Uses separate localStorage keys to ensure isolation from regular user sessions.
 * This prevents logout on one side from affecting the other.
 */
const adminStorage = {
  /**
   * Get the admin authorization token from localStorage.
   * @returns The authorization token string or null if not found
   */
  getAuthorization: () => {
    return localStorage.getItem(AdminAuthorization);
  },

  /**
   * Get the admin access token from localStorage.
   * @returns The access token string or null if not found
   */
  getToken: () => {
    return localStorage.getItem(AdminToken);
  },

  /**
   * Get the admin user info string from localStorage.
   * @returns The user info JSON string or null if not found
   */
  getUserInfo: () => {
    return localStorage.getItem(AdminUserInfo);
  },

  /**
   * Get the admin user info object from localStorage.
   * @returns The parsed user info object or null if not found
   */
  getUserInfoObject: () => {
    const userInfoStr = localStorage.getItem(AdminUserInfo);
    return userInfoStr ? JSON.parse(userInfoStr) : null;
  },

  /**
   * Set the admin authorization token in localStorage.
   * @param value - The authorization token string
   */
  setAuthorization: (value: string) => {
    localStorage.setItem(AdminAuthorization, value);
  },

  /**
   * Set the admin access token in localStorage.
   * @param value - The access token string
   */
  setToken: (value: string) => {
    localStorage.setItem(AdminToken, value);
  },

  /**
   * Set the admin user info in localStorage.
   * @param value - The user info as string or object (will be stringified if object)
   */
  setUserInfo: (value: string | Record<string, unknown>) => {
    const valueStr = typeof value !== 'string' ? JSON.stringify(value) : value;
    localStorage.setItem(AdminUserInfo, valueStr);
  },

  /**
   * Set multiple key-value pairs in localStorage.
   * Automatically maps standard keys to admin-specific keys for isolation.
   * @param pairs - Object containing key-value pairs to store
   *
   * @example
   * // Standard keys are automatically mapped to admin keys
   * adminStorage.setItems({
   *   Authorization: 'Bearer xxx',  // Stored as AdminAuthorization
   *   Token: 'xxx',                 // Stored as AdminToken
   *   userInfo: '{"name":"admin"}'  // Stored as AdminUserInfo
   * });
   */
  setItems: (pairs: Record<string, string>) => {
    Object.entries(pairs).forEach(([key, value]) => {
      if (key === Authorization) {
        localStorage.setItem(AdminAuthorization, value);
      } else if (key === Token) {
        localStorage.setItem(AdminToken, value);
      } else if (key === UserInfo) {
        localStorage.setItem(AdminUserInfo, value);
      } else {
        localStorage.setItem(key, value);
      }
    });
  },

  /**
   * Remove the admin authorization token from localStorage.
   */
  removeAuthorization: () => {
    localStorage.removeItem(AdminAuthorization);
  },

  /**
   * Remove all admin authentication-related items from localStorage.
   * This clears AdminAuthorization, AdminToken, and AdminUserInfo keys.
   */
  removeAll: () => {
    AdminKeySet.forEach((x) => {
      localStorage.removeItem(x);
    });
  },
};

/**
 * Get the authorization token for regular users.
 * Checks URL parameters first (for OAuth flow), then falls back to localStorage.
 * @returns The authorization token string (empty string if not found)
 */
export const getAuthorization = () => {
  const jwtAuth = getSearchValue('jwt_auth');
  if (jwtAuth) {
    return jwtAuth;
  }

  const auth = getSearchValue('auth');
  const authorization = auth
    ? 'Bearer ' + auth
    : storage.getAuthorization() || '';

  return authorization;
};

/**
 * Get the authorization token for admin users.
 * @returns The admin authorization token string (empty string if not found)
 */
export const getAdminAuthorization = () => {
  return adminStorage.getAuthorization() || '';
};

export default storage;
export { adminStorage };

/**
 * Redirect to the regular user login page.
 */
export function redirectToLogin() {
  window.location.href = location.origin + `/login`;
}

/**
 * Redirect to the admin login page.
 */
export function redirectToAdminLogin() {
  window.location.href = location.origin + `/admin`;
}
