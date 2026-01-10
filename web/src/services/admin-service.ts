import { history } from '@/utils/simple-history-util';
import { message, notification } from 'antd';
import axios from 'axios';

import { Authorization } from '@/constants/authorization';
import i18n from '@/locales/config';
import { Routes } from '@/routes';
import API from '@/utils/api';
import authorizationUtil, {
  getAuthorization,
} from '@/utils/authorization-util';
import { convertTheKeysOfTheObjectToSnake } from '@/utils/common-util';
import { ResultCode, RetcodeMessage } from '@/utils/request';

import type {
  EmailLoginParams,
  LDAPLoginParams,
} from '@/hooks/use-login-request';

const request = axios.create({
  timeout: 300000,
});

request.interceptors.request.use((config) => {
  const data = convertTheKeysOfTheObjectToSnake(config.data);
  const params = convertTheKeysOfTheObjectToSnake(config.params) as any;

  const newConfig = { ...config, data, params };

  // @ts-ignore
  if (!newConfig.skipToken) {
    newConfig.headers.set(Authorization, getAuthorization());
  }

  return newConfig;
});

request.interceptors.response.use(
  (response) => {
    if (response.config.responseType === 'blob') {
      return response;
    }

    const { data } = response ?? {};

    if (data?.code === 100) {
      message.error(data?.message);
    } else if (data?.code === 401) {
      notification.error({
        message: data?.message,
        description: data?.message,
        duration: 3,
      });

      authorizationUtil.removeAll();
      history.push(Routes.Admin);
    } else if (data?.code && data.code !== 0) {
      notification.error({
        message: `${i18n.t('message.hint')}: ${data?.code}`,
        description: data?.message,
        duration: 3,
      });
    }

    return response;
  },
  (error) => {
    const { response, message } = error;
    const { data } = response ?? {};

    if (error.message === 'Failed to fetch') {
      notification.error({
        description: i18n.t('message.networkAnomalyDescription'),
        message: i18n.t('message.networkAnomaly'),
      });
    } else if (data?.code === 100) {
      message.error(data?.message);
    } else if (response.status === 401 || data?.code === 401) {
      notification.error({
        message: data?.message || response.statusText,
        description:
          data?.message || RetcodeMessage[response?.status as ResultCode],
        duration: 3,
      });

      authorizationUtil.removeAll();
      history.push(Routes.Admin);
    } else if (data?.code && data.code !== 0) {
      notification.error({
        message: `${i18n.t('message.hint')}: ${data?.code}`,
        description: data?.message,
        duration: 3,
      });
    } else if (response.status) {
      notification.error({
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

export const listVariables = () =>
  request.get<ResponseData<AdminService.VariableRaw[]>>(API.adminVariables);

export const getVariable = (name: string) =>
  request.get<ResponseData<AdminService.VariableRaw>>(API.adminVariables, {
    params: {
      var_name: name,
    },
  });

export const setVariable = (name: string, value: any) =>
  request.put<ResponseData<never>>(API.adminVariables, {
    var_name: name,
    var_value: value,
  });
