import message from '@/components/ui/message';
import { LanguageTranslationMap } from '@/constants/common';
import { TenantRole } from '@/constants/team';
import { ResponseGetType } from '@/interfaces/database/base';
import { IToken } from '@/interfaces/database/chat';
import { ITenantInfo } from '@/interfaces/database/knowledge';
import { ILangfuseConfig } from '@/interfaces/database/system';
import {
  ISystemStatus,
  ITenant,
  ITenantUser,
  IUserInfo,
} from '@/interfaces/database/user-setting';
import { ISetLangfuseConfigRequestBody } from '@/interfaces/request/system';
import userService, {
  addTenantUser,
  agreeTenant,
  deleteTenantUser,
  listTenant,
  listTenantUser,
} from '@/services/user-service';
import { history } from '@/utils/simple-history-util';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import DOMPurify from 'dompurify';
import { isEmpty } from 'lodash';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { history } from 'umi';
import {
  useFetchEnableAdmin,
  useFetchIsAdmin,
} from './use-private-llm-request';

export const enum UserSettingApiAction {
  UserInfo = 'userInfo',
  TenantInfo = 'tenantInfo',
  SaveSetting = 'saveSetting',
  FetchManualSystemTokenList = 'fetchManualSystemTokenList',
  FetchSystemTokenList = 'fetchSystemTokenList',
  RemoveSystemToken = 'removeSystemToken',
  CreateSystemToken = 'createSystemToken',
  ListTenantUser = 'listTenantUser',
  AddTenantUser = 'addTenantUser',
  DeleteTenantUser = 'deleteTenantUser',
  ListTenant = 'listTenant',
  AgreeTenant = 'agreeTenant',
  SetLangfuseConfig = 'setLangfuseConfig',
  DeleteLangfuseConfig = 'deleteLangfuseConfig',
  FetchLangfuseConfig = 'fetchLangfuseConfig',
}

export const useFetchUserInfo = (): ResponseGetType<IUserInfo> => {
  const { i18n } = useTranslation();

  const { data, isFetching: loading } = useQuery({
    queryKey: [UserSettingApiAction.UserInfo],
    initialData: {},
    gcTime: 0,
    queryFn: async () => {
      const { data } = await userService.user_info();
      if (data.code === 0) {
        i18n.changeLanguage(
          LanguageTranslationMap[
            data.data.language as keyof typeof LanguageTranslationMap
          ],
        );
      }
      return data?.data ?? {};
    },
  });

  return { data, loading };
};

export const useFetchTenantInfo = (
  showEmptyModelWarn = false,
): ResponseGetType<ITenantInfo> => {
  const { t } = useTranslation();
  const { data: isAdmin, loading: isAdminLoading } = useFetchIsAdmin();
  const { data: enableAdmin, loading: enableAdminLoading } =
    useFetchEnableAdmin();

  const { data, isFetching: loading } = useQuery({
    queryKey: [UserSettingApiAction.TenantInfo, showEmptyModelWarn],
    initialData: {},
    gcTime: 0,
    queryFn: async () => {
      const { data: res = {} } = await userService.get_tenant_info();
      if (res.code === 0) {
        // llm_id is chat_id
        // asr_id is speech2txt
        const { data } = res;
        data.chat_id = data.llm_id;
        data.speech2text_id = data.asr_id;

        return data ?? {};
      }

      return res ?? {};
    },
  });

  const run = useCallback(() => {
    if (
      !isAdminLoading &&
      !enableAdminLoading &&
      !loading &&
      data &&
      (isEmpty(data.embd_id) || isEmpty(data.llm_id)) &&
      showEmptyModelWarn
    ) {
      if (enableAdmin && !isAdmin) {
        toast.warning(t('setting.requestAdminAddModel'), {
          position: 'top-center',
          closeButton: false,
          duration: 5000,
          id: 'model-providers-warn', // Add a unique ID to prevent duplicate toasts
        });
      } else {
        toast.warning(t('common.warn'), {
          position: 'top-center',
          closeButton: false,
          description: (
            <div
              dangerouslySetInnerHTML={{
                __html: DOMPurify.sanitize(t('setting.modelProvidersWarn')),
              }}
            ></div>
          ),
          action: {
            label: t('common.confirm'),
            onClick: () => history.push('/user-setting/model'),
          },
          duration: Infinity,
          id: 'model-providers-warn-admin', // Add a unique ID to prevent duplicate toasts
        });
      }
    }
  }, [
    data,
    enableAdmin,
    enableAdminLoading,
    isAdmin,
    isAdminLoading,
    loading,
    showEmptyModelWarn,
    t,
  ]);

  useEffect(() => {
    run();
  }, [run]);

  return { data, loading };
};

export const useSelectParserList = (): Array<{
  value: string;
  label: string;
}> => {
  const { data: tenantInfo } = useFetchTenantInfo(true);

  const parserList = useMemo(() => {
    const parserArray: Array<string> = tenantInfo?.parser_ids?.split(',') ?? [];
    return parserArray.map((x) => {
      const arr = x.split(':');
      return { value: arr[0], label: arr[1] };
    });
  }, [tenantInfo]);

  return parserList;
};

export const useSaveSetting = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.SaveSetting],
    mutationFn: async (
      userInfo: { new_password: string } | Partial<IUserInfo>,
    ) => {
      const { data } = await userService.setting(userInfo);
      if (data.code === 0) {
        message.success(t('message.modified'));
        queryClient.invalidateQueries({ queryKey: ['userInfo'] });
      }
      return data?.code;
    },
  });

  return { data, loading, saveSetting: mutateAsync };
};

export const useFetchSystemVersion = () => {
  const [version, setVersion] = useState('');
  const [loading, setLoading] = useState(false);

  const fetchSystemVersion = useCallback(async () => {
    try {
      setLoading(true);
      const { data } = await userService.getSystemVersion();
      if (data.code === 0) {
        setVersion(data.data);
        setLoading(false);
      }
    } catch (error) {
      setLoading(false);
    }
  }, []);

  return { fetchSystemVersion, version, loading };
};

export const useFetchSystemStatus = () => {
  const [systemStatus, setSystemStatus] = useState<ISystemStatus>(
    {} as ISystemStatus,
  );
  const [loading, setLoading] = useState(false);

  const fetchSystemStatus = useCallback(async () => {
    setLoading(true);
    const { data } = await userService.getSystemStatus();
    if (data.code === 0) {
      setSystemStatus(data.data);
      setLoading(false);
    }
  }, []);

  return {
    systemStatus,
    fetchSystemStatus,
    loading,
  };
};

export const useFetchManualSystemTokenList = () => {
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.FetchManualSystemTokenList],
    mutationFn: async () => {
      const { data } = await userService.listToken();

      return data?.data ?? [];
    },
  });

  return { data, loading, fetchSystemTokenList: mutateAsync };
};

export const useFetchSystemTokenList = () => {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IToken[]>({
    queryKey: [UserSettingApiAction.FetchSystemTokenList],
    initialData: [],
    gcTime: 0,
    queryFn: async () => {
      const { data } = await userService.listToken();

      return data?.data ?? [];
    },
  });

  return { data, loading, refetch };
};

export const useRemoveSystemToken = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.RemoveSystemToken],
    mutationFn: async (token: string) => {
      const { data } = await userService.removeToken({}, token);
      if (data.code === 0) {
        message.success(t('message.deleted'));
        queryClient.invalidateQueries({
          queryKey: [UserSettingApiAction.FetchSystemTokenList],
        });
      }
      return data?.data ?? [];
    },
  });

  return { data, loading, removeToken: mutateAsync };
};

export const useCreateSystemToken = () => {
  const queryClient = useQueryClient();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.CreateSystemToken],
    mutationFn: async (params: Record<string, any>) => {
      const { data } = await userService.createToken(params);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [UserSettingApiAction.FetchSystemTokenList],
        });
      }
      return data?.data ?? [];
    },
  });

  return { data, loading, createToken: mutateAsync };
};

export const useListTenantUser = (
  tenantId: string,
  excludePendingInvitations = false,
) => {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<ITenantUser[]>({
    queryKey: [UserSettingApiAction.ListTenantUser, tenantId],
    initialData: [],
    gcTime: 0,
    enabled: !!tenantId,
    select: (data) =>
      excludePendingInvitations
        ? data.filter((x) => x.role !== TenantRole.Invite)
        : data,
    queryFn: async () => {
      const { data } = await listTenantUser(tenantId);

      return data?.data ?? [];
    },
  });

  return { data, loading, refetch };
};

export const useAddTenantUser = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const queryClient = useQueryClient();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.AddTenantUser],
    mutationFn: async (email: string) => {
      const { data } = await addTenantUser(tenantInfo.tenant_id, email);
      if (data.code === 0) {
        queryClient.invalidateQueries({
          queryKey: [UserSettingApiAction.ListTenantUser],
        });
      }
      return data?.code;
    },
  });

  return { data, loading, addTenantUser: mutateAsync };
};

export const useDeleteTenantUser = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.DeleteTenantUser],
    mutationFn: async ({
      userId,
      tenantId,
    }: {
      userId: string;
      tenantId?: string;
    }) => {
      const { data } = await deleteTenantUser({
        tenantId: tenantId ?? tenantInfo.tenant_id,
        userId,
      });
      if (data.code === 0) {
        message.success(t('message.deleted'));
        queryClient.invalidateQueries({
          queryKey: [UserSettingApiAction.ListTenantUser],
        });
        queryClient.invalidateQueries({
          queryKey: [UserSettingApiAction.ListTenant],
        });
      }
      return data?.data ?? [];
    },
  });

  return { data, loading, deleteTenantUser: mutateAsync };
};

export const useListTenant = () => {
  const { data: tenantInfo } = useFetchTenantInfo();
  const tenantId = tenantInfo.tenant_id;
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<ITenant[]>({
    queryKey: [UserSettingApiAction.ListTenant, tenantId],
    initialData: [],
    gcTime: 0,
    enabled: !!tenantId,
    queryFn: async () => {
      const { data } = await listTenant();

      return data?.data ?? [];
    },
  });

  return { data, loading, refetch };
};

export const useAgreeTenant = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.AgreeTenant],
    mutationFn: async (tenantId: string) => {
      const { data } = await agreeTenant(tenantId);
      if (data.code === 0) {
        message.success(t('message.operated'));
        queryClient.invalidateQueries({
          queryKey: [UserSettingApiAction.ListTenant],
        });
      }
      return data?.data ?? [];
    },
  });

  return { data, loading, agreeTenant: mutateAsync };
};

export const useSetLangfuseConfig = () => {
  const { t } = useTranslation();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.SetLangfuseConfig],
    mutationFn: async (params: ISetLangfuseConfigRequestBody) => {
      const { data } = await userService.setLangfuseConfig(params);
      if (data.code === 0) {
        message.success(t('message.operated'));
      }
      return data?.code;
    },
  });

  return { data, loading, setLangfuseConfig: mutateAsync };
};

export const useDeleteLangfuseConfig = () => {
  const { t } = useTranslation();
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [UserSettingApiAction.DeleteLangfuseConfig],
    mutationFn: async () => {
      const { data } = await userService.deleteLangfuseConfig();
      if (data.code === 0) {
        message.success(t('message.deleted'));
      }
      return data?.code;
    },
  });

  return { data, loading, deleteLangfuseConfig: mutateAsync };
};

export const useFetchLangfuseConfig = () => {
  const { data, isFetching: loading } = useQuery<ILangfuseConfig>({
    queryKey: [UserSettingApiAction.FetchLangfuseConfig],
    gcTime: 0,
    queryFn: async () => {
      const { data } = await userService.getLangfuseConfig();

      return data?.data;
    },
  });

  return { data, loading };
};
