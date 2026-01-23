import message from '@/components/ui/message';
import { Authorization } from '@/constants/authorization';
import userService, {
  getLoginChannels,
  loginWithChannel,
  loginWithLdap,
  type LoginWithLdapInput,
} from '@/services/user-service';
import authorizationUtil, { redirectToLogin } from '@/utils/authorization-util';
import { useMutation, useQuery } from '@tanstack/react-query';
import { groupBy } from 'lodash';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

export interface ILoginRequestBody {
  email: string;
  password: string;
}

export interface IRegisterRequestBody extends ILoginRequestBody {
  nickname: string;
}

export interface ILoginChannel {
  channel: string;
  display_name: string;
  icon: string;
}

export const useLoginChannels = () => {
  const { data, isLoading } = useQuery({
    queryKey: ['loginChannels'],
    queryFn: async (): Promise<ILoginChannel[]> => {
      const { data: res = {} } = await getLoginChannels();
      return res.data || [];
    },
    initialData: [],
  });

  const typeGroupedChannels = useMemo(
    () =>
      groupBy(data, (item) =>
        item.channel.startsWith('ldap') ? 'ldap' : 'sso',
      ),
    [data],
  );

  const currentChannelType = useMemo((): 'sso' | 'ldap' | undefined => {
    if (typeGroupedChannels.sso?.length) {
      return 'sso';
    }

    if (typeGroupedChannels.ldap?.length) {
      return 'ldap';
    }

    return undefined;
  }, [typeGroupedChannels]);

  return {
    channels: data,
    currentChannelType,
    typeGroupedChannels,
    loading: isLoading,
  };
};

export const useLoginWithChannel = () => {
  const { isPending: loading, mutateAsync } = useMutation({
    mutationKey: ['loginWithChannel'],
    mutationFn: async (channel: string) => {
      loginWithChannel(channel);
      return Promise.resolve();
    },
  });

  const { mutateAsync: loginLdap, isPending: loginLdapLoading } = useMutation({
    mutationFn: (inputs: LoginWithLdapInput) => loginWithLdap(inputs),
  });

  return {
    loading: loading || loginLdapLoading,
    login: mutateAsync,
    loginLdap,
  };
};

export type EmailLoginParams = {
  email: string;
  password: string;
};

export type LDAPLoginParams = {
  username: string;
  password: string;
  ldap_server: string;
};

export const useLogin = () => {
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: ['login'],
    mutationFn: async (params: EmailLoginParams | LDAPLoginParams) => {
      const { data: res = {}, response } = await userService.login(params);
      if (res.code === 0) {
        const { data } = res;
        const authorization = response.headers.get(Authorization);
        const token = data.access_token;
        const userInfo = {
          avatar: data.avatar,
          name: data.nickname,
          email: data.email,
        };
        authorizationUtil.setItems({
          Authorization: authorization,
          userInfo: JSON.stringify(userInfo),
          Token: token,
        });
      }
      return res.code;
    },
  });

  return { data, loading, login: mutateAsync };
};

export const useRegister = () => {
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: ['register'],
    mutationFn: async (params: {
      email: string;
      password: string;
      nickname: string;
    }) => {
      const { data = {} } = await userService.register(params);
      if (data.code === 0) {
        message.success(t('message.registered'));
      } else if (
        data.message &&
        data.message.includes('registration is disabled')
      ) {
        message.error(
          t('message.registerDisabled') || 'User registration is disabled',
        );
      }
      return data.code;
    },
  });

  return { data, loading, register: mutateAsync };
};

export const useLogout = () => {
  const { t } = useTranslation();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: ['logout'],
    mutationFn: async () => {
      const { data = {} } = await userService.logout();
      if (data.code === 0) {
        message.success(t('message.logout'));
        authorizationUtil.removeAll();
        redirectToLogin();
      }
      return data.code;
    },
  });

  return { data, loading, logout: mutateAsync };
};
