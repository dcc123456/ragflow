import { useEffect, useState } from 'react';

import { adminStorage } from '@/utils/authorization-util';

/**
 * Custom hook for Admin authentication state management.
 *
 * This hook uses a separate storage (adminStorage) to check login status,
 * ensuring complete isolation from the regular user authentication system.
 * This prevents logout on one side from affecting the other.
 *
 * @returns {Object} Authentication state object
 * @returns {Nullable<boolean>} returns.isLogin - The current login status (null during initial check)
 *
 * @example
 * const { isLogin } = useAdminAuth();
 * if (isLogin) {
 *   // User is authenticated, render protected content
 * }
 */
export const useAdminAuth = () => {
  const [isLogin, setIsLogin] = useState<Nullable<boolean>>(null);

  useEffect(() => {
    setIsLogin(!!adminStorage.getAuthorization());
  }, []);

  return { isLogin };
};
