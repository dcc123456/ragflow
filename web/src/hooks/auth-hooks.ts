import message from '@/components/ui/message';
import authorizationUtil from '@/utils/authorization-util';
import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';

export const useLoginWithGithub = () => {
  const [currentQueryParameters, setSearchParams] = useSearchParams();
  const error = currentQueryParameters.get('error');
  const newQueryParameters: URLSearchParams = useMemo(
    () => new URLSearchParams(currentQueryParameters.toString()),
    [currentQueryParameters],
  );
  const navigate = useNavigate();

  if (error) {
    message.error(error);
    navigate('/login');
    newQueryParameters.delete('error');
    setSearchParams(newQueryParameters);
    return;
  }

  const auth = currentQueryParameters.get('auth');

  const redirectUrl = currentQueryParameters.get('redirect_url');

  if (auth) {
    authorizationUtil.setAuthorization(auth);
    newQueryParameters.delete('auth');
    if (redirectUrl) {
      newQueryParameters.delete('redirect_url');
    }
    setSearchParams(newQueryParameters);
  }

  return { auth, redirectUrl };
};

export const useAuth = () => {
  const value = useLoginWithGithub();

  const auth = value?.auth;
  const redirectUrl = value?.redirectUrl;

  const [isLogin, setIsLogin] = useState<Nullable<boolean>>(null);

  useEffect(() => {
    setIsLogin(!!authorizationUtil.getAuthorization() || !!auth);
  }, [auth]);

  return { isLogin, redirectUrl };
};
