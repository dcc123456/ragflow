import { useAuth } from '@/hooks/auth-hooks';
import { redirectToLogin, redirectToSpecifiedPage } from '@/utils/private-util';
import { Outlet } from 'react-router';

export default () => {
  const { isLogin, redirectUrl } = useAuth();

  if (isLogin === true) {
    if (redirectUrl) {
      redirectToSpecifiedPage(redirectUrl);
    }

    return <Outlet />;
  } else if (isLogin === false) {
    redirectToLogin();
  }

  return <></>;
};
