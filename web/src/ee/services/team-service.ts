import {
  IDeleteDepartmentMemberRequestBody,
  IFetchDepartmentRequestParams,
  ITransferGroupOwnerRequestBody,
  IUpdateDepartmentRequestBody,
  IUpdateGroupRequestBody,
} from '@/ee/interfaces/request/team';
import api from '@/ee/utils/private-api';
import registerServer from '@/utils/register-server';
import request, { drop } from '@/utils/request';

const {
  createDepartment,
  createGroup,
  moveDepartment,
  updatePermission,
  transferResourceOwner,
  listPermission,
  listPermissionByTarget,
} = api;

const methods = {
  createDepartment: {
    url: createDepartment,
    method: 'post',
  },
  moveDepartment: {
    url: moveDepartment,
    method: 'put',
  },
  createGroup: {
    url: createGroup,
    method: 'post',
  },
  updatePermission: {
    url: updatePermission,
    method: 'put',
  },
  transferResourceOwner: {
    url: transferResourceOwner,
    method: 'put',
  },
  listPermission: {
    url: listPermission,
    method: 'post',
  },
  listPermissionByTarget: {
    url: listPermissionByTarget,
    method: 'post',
  },
} as const;

const teamService = {
  ...registerServer<keyof typeof methods>(methods, request),
  createDepartmentMember: (tenantId: string, body: unknown) =>
    request.post(api.createDepartmentMember(tenantId), { data: body }),
  createGroupMember: (tenantId: string, body: unknown) =>
    request.post(api.createGroupMember(tenantId), { data: body }),
  deleteGroupMember: (tenantId: string, body: unknown) =>
    drop(api.deleteGroupMember(tenantId), body),
};

export default teamService;

export const deleteDepartment = (id: string) => drop(api.deleteDepartment(id));

export const deleteGroup = (tenantId: string, id: string) =>
  drop(api.deleteGroup(tenantId, id));

export const updateGroup = (tenantId: string, body: IUpdateGroupRequestBody) =>
  request.put(api.updateGroup(tenantId), { data: body });

export const updateDepartment = (
  tenantId: string,
  body: IUpdateDepartmentRequestBody,
) => request.put(api.updateDepartment(tenantId), { data: body });

export const listDepartmentMember = (tenantId: string, id: string) =>
  request.get(api.listDepartmentMember(tenantId, id));

export const listDepartment = (
  tenantId: string,
  { parentId, all }: IFetchDepartmentRequestParams,
) =>
  request.get(api.listDepartment(tenantId), {
    params: { parent_id: parentId, all },
  });

export const listGroup = (tenantId: string) =>
  request.get(api.listGroup(tenantId));

export const deleteDepartmentMember = (
  tenantId: string,
  body: IDeleteDepartmentMemberRequestBody,
) => drop(api.deleteDepartmentMember(tenantId), body);

export const listGroupMember = (tenantId: string, id: string) =>
  request.get(api.listGroupMember(tenantId, id));

export const transferGroupOwner = (
  tenantId: string,
  params: ITransferGroupOwnerRequestBody,
) => request.put(api.transferGroupOwner(tenantId), { data: params });
