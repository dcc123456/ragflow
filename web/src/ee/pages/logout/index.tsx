import authorizationUtil from '@/utils/authorization-util';
import { useEffect } from 'react';
import { useNavigate } from 'react-router';

const LogoutPage = () => {
  const navigate = useNavigate();

  useEffect(() => {
    // Clear all authorization data
    authorizationUtil.removeAll();

    // Redirect to login page
    navigate('/login-next', { replace: true });
  }, [navigate]);

  return null;
};

export default LogoutPage;
