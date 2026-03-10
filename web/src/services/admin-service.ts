import { history } from '@/utils/simple-history-util';
import axios, {
  type AxiosResponse,
  type InternalAxiosRequestConfig,
} from 'axios';

import message from '@/components/ui/message';
import { Authorization } from '@/constants/authorization';
import i18n from '@/locales/config';
import { Routes } from '@/routes';
import API from '@/utils/api';
import authorizationUtil, {
  getAuthorization,
} from '@/utils/authorization-util';
import { convertTheKeysOfTheObjectToSnake } from '@/utils/common-util';
import { ResultCode, RetcodeMessage } from '@/utils/request';

import { LLMFactory } from '@/constants/llm';
import type {
  EmailLoginParams,
  LDAPLoginParams,
} from '@/hooks/use-login-request';
import {
  IFactory,
  IMyLlmValue,
  IThirdOAIModelCollection,
} from '@/interfaces/database/llm';
import { isLocalLlmFactory } from '@/pages/user-setting/utils';
import api from '@/utils/api';
import { keyBy } from 'lodash';

function redirectToLogin() {
  authorizationUtil.removeAll();
  history.push(Routes.Admin);
  window.location.reload();
}

function injectAuthorizationToRequest(config: InternalAxiosRequestConfig) {
  const data = convertTheKeysOfTheObjectToSnake(config.data);
  const params = convertTheKeysOfTheObjectToSnake(config.params) as any;

  const newConfig = { ...config, data, params };

  // @ts-ignore
  if (!newConfig.skipToken) {
    newConfig.headers.set(Authorization, getAuthorization());
  }

  return newConfig;
}

const request = axios.create({
  timeout: 300000,
});

request.interceptors.request.use(injectAuthorizationToRequest);
request.interceptors.response.use(
  (response) => {
    if (response.config.responseType === 'blob') {
      return response;
    }

    const { data } = response ?? {};

    if (data?.code === 100) {
      message.error(data?.message);
    } else if (data?.code === 401) {
      message.error(data?.message, {
        description: data?.message,
      });

      redirectToLogin();
    } else if (data?.code && data.code !== 0) {
      message.error(`${i18n.t('message.hint')}: ${data?.code}`, {
        description: data?.message,
      });
    }

    return response;
  },
  (error) => {
    const { response } = error;
    const { data } = response ?? {};

    if (error.message === 'Failed to fetch') {
      message.error({
        description: i18n.t('message.networkAnomalyDescription'),
        message: i18n.t('message.networkAnomaly'),
      });
    } else if (data?.code === 100) {
      message.error(data?.message);
    } else if (response.status === 401 || data?.code === 401) {
      message.error({
        message: data?.message || response.statusText,
        description:
          data?.message || RetcodeMessage[response?.status as ResultCode],
        duration: 3,
      });

      redirectToLogin();
    } else if (data?.code && data.code !== 0) {
      message.error({
        message: `${i18n.t('message.hint')}: ${data?.code}`,
        description: data?.message,
        duration: 3,
      });
    } else if (response.status) {
      message.error({
        message: `${i18n.t('message.requestError')} ${response.status}: ${response.config.url}`,
        description:
          RetcodeMessage[response.status as ResultCode] || response.statusText,
      });
    } else if (response.status === 413 || response?.status === 504) {
      message.error(RetcodeMessage[response?.status as ResultCode]);
    }

    throw error;
  },
);

const requestSilent = axios.create({
  timeout: 300000,
});

requestSilent.interceptors.request.use(injectAuthorizationToRequest);
requestSilent.interceptors.response.use((response) => {
  if (response.config.responseType === 'blob') {
    return response;
  }

  const { data } = response ?? {};

  if (data?.code === 401) {
    redirectToLogin();
  }

  return response;
});

type ResponseData<D = NonNullable<unknown>> = {
  code: number;
  message: string;
  data: D;
};

export const login = (params: EmailLoginParams | LDAPLoginParams) =>
  request.post<ResponseData<AdminService.LoginData>>(API.adminLogin, params);

export const logout = () => request.get<ResponseData<boolean>>(API.adminLogout);

export const listUsers = () =>
  request.get<ResponseData<AdminService.ListUsersItem[]>>(
    API.adminListUsers,
    {},
  );

export const createUser = (email: string, password: string) =>
  request.post<ResponseData<boolean>>(API.adminCreateUser, {
    username: email,
    password,
  });

export const grantSuperuser = (email: string) =>
  request.put<ResponseData<void>>(api.adminSetSuperuser(email));

export const revokeSuperuser = (email: string) =>
  request.delete<ResponseData<void>>(api.adminSetSuperuser(email));

export const getUserDetails = (email: string) =>
  request.get<ResponseData<[AdminService.UserDetail]>>(
    API.adminGetUserDetails(email),
  );

export const listUserDatasets = (email: string) =>
  request.get<ResponseData<AdminService.ListUserDatasetItem[]>>(
    API.adminListUserDatasets(email),
  );

export const listUserAgents = (email: string) =>
  request.get<ResponseData<AdminService.ListUserAgentItem[]>>(
    API.adminListUserAgents(email),
  );

export const updateUserStatus = (email: string, status: 'on' | 'off') =>
  request.put(API.adminUpdateUserStatus(email), { activate_status: status });

export const updateUserPassword = (email: string, password: string) =>
  request.put(API.adminUpdateUserPassword(email), { new_password: password });

export const deleteUser = (email: string) =>
  request.delete(API.adminDeleteUser(email));

export const listServices = () =>
  request.get<ResponseData<AdminService.ListServicesItem[]>>(
    API.adminListServices,
  );

export const showServiceDetails = (serviceId: number) =>
  request.get<ResponseData<AdminService.ServiceDetail>>(
    API.adminShowServiceDetails(String(serviceId)),
  );

export const createRole = (params: {
  roleName: string;
  description?: string;
}) =>
  request.post<ResponseData<AdminService.RoleDetail>>(
    API.adminCreateRole,
    params,
  );

export const updateRoleDescription = (role: string, description: string) =>
  request.put<ResponseData<AdminService.RoleDetail>>(
    API.adminUpdateRoleDescription(role),
    { description },
  );

export const deleteRole = (role: string) =>
  request.delete<ResponseData<ResponseData<never>>>(API.adminDeleteRole(role));

export const listRoles = () =>
  request.get<
    ResponseData<{ roles: AdminService.ListRoleItem[]; total: number }>
  >(API.adminListRoles);

export const listRolesWithPermission = () =>
  request.get<
    ResponseData<{
      roles: AdminService.ListRoleItemWithPermission[];
      total: number;
    }>
  >(API.adminListRolesWithPermission);

export const getRolePermissions = (role: string) =>
  request.get<ResponseData<AdminService.RoleDetailWithPermission>>(
    API.adminGetRolePermissions(role),
  );

export const assignRolePermissions = (
  role: string,
  permissions: Partial<AdminService.AssignRolePermissionsInput>,
) =>
  request.post<ResponseData<never>>(API.adminAssignRolePermissions(role), {
    new_permissions: permissions,
  });

export const revokeRolePermissions = (
  role: string,
  permissions: Partial<AdminService.RevokeRolePermissionInput>,
) =>
  request.delete<ResponseData<never>>(API.adminRevokeRolePermissions(role), {
    data: { revoke_permissions: permissions },
  });

export const updateUserRole = (username: string, role: string) =>
  request.put<ResponseData<never>>(API.adminUpdateUserRole(username), {
    role_name: role,
  });

export const getUserPermissions = (username: string) =>
  request.get<ResponseData<AdminService.UserDetailWithPermission>>(
    API.adminGetUserPermissions(username),
  );

export const listResources = () =>
  request.get<ResponseData<AdminService.ResourceType>>(API.adminListResources);

export const listWhitelist = () =>
  request.get<
    ResponseData<{
      total: number;
      white_list: AdminService.ListWhitelistItem[];
    }>
  >(API.adminListWhitelist);

export const createWhitelistEntry = (email: string) =>
  request.post<ResponseData<never>>(API.adminCreateWhitelistEntry, { email });

export const updateWhitelistEntry = (id: number, email: string) =>
  request.put<ResponseData<never>>(API.adminUpdateWhitelistEntry(id), {
    email,
  });

export const deleteWhitelistEntry = (email: string) =>
  request.delete<ResponseData<never>>(API.adminDeleteWhitelistEntry(email));

export const importWhitelistFromExcel = (file: File) => {
  const fd = new FormData();

  fd.append('file', file);

  return request.post<ResponseData<never>>(API.adminImportWhitelist, fd);
};

export const getSystemVersion = () =>
  request.get<ResponseData<{ version: string }>>(API.adminGetSystemVersion);

function castFromInt(value: string): number | null {
  const parsed = parseInt(value);
  return isNaN(parsed) ? null : parsed;
}

export const listVariables = async () => {
  const resp = await request.get<
    ResponseData<
      {
        name: string;
        value: string;
        data_type: AdminService.SystemVariables.Types.DataType;
        source: string;
      }[]
    >
  >(API.adminVariables);

  if (resp.data?.data) {
    const mappedData = resp.data.data.map((variable) => {
      switch (variable.data_type) {
        case 'bool':
          return {
            ...variable,
            value: variable.value === 'true' || variable.value === 'True',
          };
        case 'integer':
          return {
            ...variable,
            value: castFromInt(variable.value),
          };
        default:
          return variable;
      }
    });

    // In-place modification
    // @ts-ignore
    resp.data.data = keyBy(mappedData, 'name');
  }

  return resp as unknown as AxiosResponse<
    ResponseData<AdminService.SystemVariables>
  >;
};

export const getVariable = (name: string) =>
  request.get<ValueOf<ResponseData<AdminService.SystemVariables>>>(
    API.adminVariables,
    {
      params: {
        var_name: name,
      },
    },
  );

export const setVariable = (name: string, value: any) =>
  request.put<ResponseData<never>>(API.adminVariables, {
    var_name: name,
    var_value: value,
  });

export const deleteLdapServer = (serverId: string) => {
  return request.delete<ResponseData<never>>(API.adminVariables, {
    data: {
      source: `ldap|${serverId}`,
    },
  });
};

export const testSMTPConnection = (
  params: AdminService.TestSMTPConnectionInput,
) =>
  requestSilent.post<ResponseData<boolean>>(
    API.adminTestSMTPConnection,
    params,
  );

export const getRoleDefaultModels = (role: string) =>
  request.get<ResponseData<AdminService.RoleDefaultModelList>>(
    API.adminRoleDefaultModels(role),
  );

export const setRoleDefaultModel = (
  role: string,
  input: AdminService.SetRoleDefaultModelInput,
) =>
  request.put<ResponseData<boolean>>(API.adminRoleDefaultModels(role), input);

export const listMyLlm = () =>
  request.get<ResponseData<Record<LLMFactory, IMyLlmValue>>>(
    API.adminListMyLlm,
    {
      params: {
        include_details: true,
      },
    },
  );

export const listLlmFactories = () =>
  request.get<ResponseData<IFactory[]>>(API.adminListLlmFactories);

export const listAllFactoryLlms = () =>
  request.get<ResponseData<IThirdOAIModelCollection>>(
    API.adminListAllFactoryLlms,
  );

export const deleteFactory = (factoryName: string) =>
  request.post<ResponseData<never>>(API.adminDeleteLlmFactory, {
    llm_factory: factoryName,
  });

export const addFactory = (inputs: AdminService.AddLlmFactoryInput) =>
  request.post<ResponseData<never>>(
    isLocalLlmFactory(inputs.llm_factory)
      ? API.adminAddLlmFactory
      : API.adminSetLlmApiKey,
    inputs,
  );

export const listSandboxProviders = () =>
  request.get<ResponseData<AdminService.SandboxProvider[]>>(
    API.adminListSandboxProviders,
  );

export const getSandboxConfig = () =>
  request.get<ResponseData<AdminService.SandboxConfig>>(
    API.adminGetSandboxConfig,
  );

export const setSandboxConfig = (params: AdminService.SetSandboxConfigInput) =>
  request.put<ResponseData<never>>(API.adminSetSandboxConfig, params);

export const getSandboxProviderSchema = (providerId: string) =>
  request.get<ResponseData<AdminService.SandboxProviderSchema>>(
    API.adminGetSandboxProviderSchema(providerId),
  );

export const testSandboxConnection = (
  params: AdminService.TestSandboxConnectionInput,
) =>
  request.post<ResponseData<AdminService.SandboxTestResult>>(
    API.adminTestSandboxConnection,
    params,
  );
