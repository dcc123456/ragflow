/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

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
import { adminStorage } from '@/utils/authorization-util';
import { convertTheKeysOfTheObjectToSnake } from '@/utils/common-util';
import { ResultCode, RetcodeMessage } from '@/utils/request';
import {
  adaptServiceDetail,
  adaptServiceList,
  GoServiceStatus,
} from '@/services/admin-service-adapter';

import { LLMFactory } from '@/constants/llm';
import type {
  EmailLoginParams,
  LDAPLoginParams,
} from '@/ee/hooks/use-login-request';
import {
  IFactory,
  IMyLlmValue,
  IThirdOAIModelCollection,
} from '@/interfaces/database/llm';
import { isLocalLlmFactory } from '@/pages/user-setting/utils';
import api from '@/utils/api';
import { keyBy } from 'lodash';

function redirectToLogin() {
  adminStorage.removeAll();
  history.push(Routes.Admin);
  window.location.reload();
}

function injectAuthorizationToRequest(config: InternalAxiosRequestConfig) {
  const data = convertTheKeysOfTheObjectToSnake(config.data);
  const params = convertTheKeysOfTheObjectToSnake(config.params) as any;

  const newConfig = { ...config, data, params };

  // @ts-ignore
  if (!newConfig.skipToken) {
    newConfig.headers.set(Authorization, adminStorage.getAuthorization());
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
      message.error(data?.message, { position: 'top-right' });
    } else if (data?.code === 401) {
      message.error(data?.message, {
        description: data?.message,
        position: 'top-right',
      });

      redirectToLogin();
    } else if (data?.code && data.code !== 0) {
      message.error(`${i18n.t('message.hint')}: ${data?.code}`, {
        description: data?.message,
        position: 'top-right',
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
  total?: number;
};

export const login = (params: EmailLoginParams | LDAPLoginParams) =>
  request.post<ResponseData<AdminServiceEe.LoginData>>(API.adminLogin, params);

export const logout = () => request.get<ResponseData<boolean>>(API.adminLogout);

export const listUsers = (params?: {
  keyword?: string;
  sort?: 'asc' | 'desc';
  order?: string;
  page?: number;
  page_size?: number;
  role?: string;
  status?: string;
  plan?: string;
}) =>
  request.get<ResponseData<AdminServiceEe.ListUsersItem[]>>(
    API.adminListUsers,
    {
      params,
    },
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
  request.get<ResponseData<[AdminServiceEe.UserDetail]>>(
    API.adminGetUserDetails(email),
  );

export const listUserDatasets = (email: string) =>
  request.get<ResponseData<AdminServiceEe.ListUserDatasetItem[]>>(
    API.adminListUserDatasets(email),
  );

export const listUserAgents = (email: string) =>
  request.get<ResponseData<AdminServiceEe.ListUserAgentItem[]>>(
    API.adminListUserAgents(email),
  );

export const updateUserStatus = (email: string, status: 'on' | 'off') =>
  request.put(API.adminUpdateUserStatus(email), { activate_status: status });

export const updateUserPassword = (email: string, password: string) =>
  request.put(API.adminUpdateUserPassword(email), { new_password: password });

export const deleteUser = (email: string) =>
  request.delete(API.adminDeleteUser(email));

export const listServices = async () => {
  const { data } = await request.get<ResponseData<GoServiceStatus[]>>(
    API.adminListServices,
  );
  return data.code === 0 ? adaptServiceList(data.data) : [];
};
export const showServiceDetails = async (serviceId: number | string) => {
  const { data } = await request.get<ResponseData<GoServiceStatus>>(
    API.adminShowServiceDetails(encodeURIComponent(String(serviceId))),
  );
  return data.code === 0 ? adaptServiceDetail(data.data) : undefined;
};

export const createRole = (params: {
  roleName: string;
  description?: string;
}) =>
  request.post<ResponseData<AdminServiceEe.RoleDetail>>(API.adminCreateRole, {
    role_name: params.roleName,
    description: params.description,
  });

export const updateRoleDescription = (role: string, description: string) =>
  request.put<ResponseData<AdminServiceEe.RoleDetail>>(
    API.adminUpdateRoleDescription(role),
    { description },
  );

export const deleteRole = (role: string) =>
  request.delete<ResponseData<ResponseData<never>>>(API.adminDeleteRole(role));

export const listRoles = () =>
  request.get<ResponseData<AdminServiceEe.ListRoleItem[]>>(API.adminListRoles);

export const listRolesWithPermission = () =>
  request.get<ResponseData<AdminServiceEe.ListRoleItemWithPermission[]>>(
    API.adminListRolesWithPermission,
  );

export const getRolePermissions = (role: string) =>
  request.get<ResponseData<AdminServiceEe.RoleDetailWithPermission>>(
    API.adminGetRolePermissions(role),
  );

export const assignRolePermissions = (
  role: string,
  permissions: Partial<AdminServiceEe.AssignRolePermissionsInput>,
) => {
  const updates = Object.entries(permissions).flatMap(([resource, values]) => {
    const actions = Object.entries(values ?? {})
      .filter(([, enabled]) => enabled)
      .map(([action]) => action);
    return actions.length > 0 ? [{ resource, actions }] : [];
  });

  return Promise.all(
    updates.map(({ resource, actions }) =>
      request.post<ResponseData<never>>(API.adminAssignRolePermissions(role), {
        actions,
        resource,
      }),
    ),
  );
};

export const revokeRolePermissions = (
  role: string,
  permissions: Partial<AdminServiceEe.RevokeRolePermissionInput>,
) => {
  const updates = Object.entries(permissions).flatMap(([resource, values]) => {
    const actions = Object.entries(values ?? {})
      .filter(([, enabled]) => enabled === false)
      .map(([action]) => action);
    return actions.length > 0 ? [{ resource, actions }] : [];
  });

  return Promise.all(
    updates.map(({ resource, actions }) =>
      request.delete<ResponseData<never>>(
        API.adminRevokeRolePermissions(role),
        {
          data: { actions, resource },
        },
      ),
    ),
  );
};

export const updateUserRole = (username: string, role: string) =>
  request.put<ResponseData<never>>(API.adminUpdateUserRole(username), {
    role_name: role,
  });

export const getUserPermissions = (username: string) =>
  request.get<ResponseData<AdminServiceEe.UserDetailWithPermission>>(
    API.adminGetUserPermissions(username),
  );

export const listResources = () =>
  request.get<ResponseData<AdminServiceEe.ResourceType>>(
    API.adminListResources,
  );

export const listWhitelist = () =>
  request.get<
    ResponseData<{
      total: number;
      white_list: AdminServiceEe.ListWhitelistItem[];
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
        data_type: AdminServiceEe.SystemVariables.Types.DataType;
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
    ResponseData<AdminServiceEe.SystemVariables>
  >;
};

export const getVariable = (name: string) =>
  request.get<ValueOf<ResponseData<AdminServiceEe.SystemVariables>>>(
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

export const uploadSensitiveWordFilter = (file: File) => {
  const fd = new FormData();
  fd.append('file', file);
  return request.post<
    ResponseData<{ size: number; word_count: number; filename: string }>
  >(API.adminUploadSensitiveWordFilter, fd);
};

export const downloadSensitiveWordFilter = () => {
  return request.get<Blob>(API.adminDownloadSensitiveWordFilter, {
    responseType: 'blob',
  });
};

export const deleteLdapServer = (serverId: string) => {
  return request.delete<ResponseData<never>>(API.adminVariables, {
    data: {
      source: `ldap|${serverId}`,
    },
  });
};

export const testSMTPConnection = (
  params: AdminServiceEe.TestSMTPConnectionInput,
) =>
  requestSilent.post<ResponseData<boolean>>(
    API.adminTestSMTPConnection,
    params,
  );

export const getRoleDefaultModels = (role: string) =>
  request.get<ResponseData<AdminServiceEe.RoleDefaultModelList>>(
    API.adminRoleDefaultModels(role),
  );

export const setRoleDefaultModel = (
  role: string,
  input: AdminServiceEe.SetRoleDefaultModelInput,
) =>
  request.patch<ResponseData<boolean>>(API.adminRoleDefaultModels(role), input);

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

export const addFactory = (inputs: AdminServiceEe.AddLlmFactoryInput) =>
  request.post<ResponseData<never>>(
    isLocalLlmFactory(inputs.llm_factory)
      ? API.adminAddLlmFactory
      : API.adminSetLlmApiKey,
    inputs,
  );

export const listSandboxProviders = () =>
  request.get<ResponseData<AdminServiceEe.SandboxProvider[]>>(
    API.adminListSandboxProviders,
  );

// export const getSandboxConfig = () =>
//   request.get<ResponseData<AdminServiceEe.SandboxConfig>>(
//     API.adminGetSandboxConfig,
//   );

export const getSandboxConfig = (providerType?: string) =>
  request.get<ResponseData<AdminServiceEe.SandboxConfig>>(
    API.adminGetSandboxConfig,
    {
      params: { provider_type: providerType },
    },
  );

export const setSandboxConfig = (
  params: AdminServiceEe.SetSandboxConfigInput,
) => request.post<ResponseData<never>>(API.adminSetSandboxConfig, params);

export const getSandboxProviderSchema = (providerId: string) =>
  request.get<ResponseData<AdminServiceEe.SandboxProviderSchema>>(
    API.adminGetSandboxProviderSchema(providerId),
  );

export const testSandboxConnection = (
  params: AdminServiceEe.TestSandboxConnectionInput,
) =>
  request.post<ResponseData<AdminServiceEe.SandboxTestResult>>(
    API.adminTestSandboxConnection,
    params,
  );

// LLM Trace Statistics
export const getLlmTraceSummary = (
  params: AdminServiceEe.LlmTraceByOrgParams,
) =>
  request.get<ResponseData<AdminServiceEe.LlmTraceSummary>>(
    API.adminLlmTraceSummary,
    { params },
  );

export const getLlmTraceByOrg = (
  params: AdminServiceEe.LlmTraceByOrgParams & AdminServiceEe.PageParams,
) =>
  request.get<ResponseData<AdminServiceEe.LlmTraceByOrgItem[]>>(
    API.adminLlmTraceByOrg,
    { params },
  );
export const getLlmTraceRecent = (
  params: AdminServiceEe.LlmTraceByOrgParams & AdminServiceEe.PageParams,
) =>
  request.get<ResponseData<AdminServiceEe.LlmTraceRecentItem[]>>(
    API.adminLlmTraceRecent,
    { params },
  );

export const getLlmTraceTopConsumers = (
  params: AdminServiceEe.LlmTraceTopConsumersParams & AdminServiceEe.PageParams,
) =>
  request.get<ResponseData<AdminServiceEe.LlmTraceTopConsumer[]>>(
    API.adminLlmTraceTopConsumers,
    { params },
  );

export const getLlmTraceTrends = (
  params: AdminServiceEe.LlmTraceTrendsParams,
) =>
  request.get<ResponseData<AdminServiceEe.LlmTraceTrend[]>>(
    API.adminLlmTraceTrends,
    { params },
  );

export const getLlmTraceByModel = (
  params: AdminServiceEe.LlmTraceByModelParams,
) =>
  request.get<ResponseData<AdminServiceEe.LlmTraceByModelItem[]>>(
    API.adminLlmTraceByModel,
    { params },
  );
// License APIs
export const getLicense = () =>
  request.get<ResponseData<AdminServiceEe.LicenseInfo>>(API.adminGetLicense);

export const setLicense = (license: string) =>
  request.post<ResponseData<AdminServiceEe.LicenseInfo>>(API.adminSetLicense, {
    license,
  });

export const checkLicense = () =>
  request.get<ResponseData<string>>(`${API.adminGetLicense}?check=true`);

export const getFingerprint = () =>
  request.get<ResponseData<AdminServiceEe.FingerprintResponse>>(
    API.adminGetFingerprint,
  );
