import {
  IAddModelToGroupRequest,
  ICreateModelGroupRequest,
  IReorderModelGroupMembersRequest,
  IRemoveModelFromGroupRequest,
  IUpdateModelGroupMemberStatusRequest,
} from '@/ee/interfaces/request/model-group';
import api from '@/utils/api';
import { registerNextServer } from '@/utils/register-server';

const methods = {
  create: { url: api.modelGroups, method: 'post' },
  list: { url: api.modelGroups, method: 'get' },
  members: { url: api.modelGroup('__group_id__'), method: 'get' },
  remove: { url: api.modelGroup('__group_id__'), method: 'delete' },
  updateMemberStatus: {
    url: api.modelGroupMember('__group_id__'),
    method: 'patch',
  },
  addMember: { url: api.modelGroupMember('__group_id__'), method: 'post' },
  removeMember: {
    url: api.modelGroupMember('__group_id__'),
    method: 'delete',
  },
  reorderMembers: {
    url: api.modelGroupOrder('__group_id__'),
    method: 'patch',
  },
} as const;

const requests = registerNextServer<keyof typeof methods>(methods);

const modelGroupService = {
  create: (data: ICreateModelGroupRequest) => requests.create({ data }, true),
  list: (accessUserId?: string) =>
    requests.list(
      {
        params: accessUserId ? { access_user_id: accessUserId } : {},
      },
      true,
    ),
  members: (groupId: string) =>
    requests.members({ url: api.modelGroup(groupId) }, true),
  remove: (groupId: string) =>
    requests.remove({ url: api.modelGroup(groupId) }, true),
  updateMemberStatus: (
    groupId: string,
    data: IUpdateModelGroupMemberStatusRequest,
  ) =>
    requests.updateMemberStatus(
      { url: api.modelGroupMember(groupId), data },
      true,
    ),
  addMember: (groupId: string, data: IAddModelToGroupRequest) =>
    requests.addMember({ url: api.modelGroupMember(groupId), data }, true),
  removeMember: (groupId: string, data: IRemoveModelFromGroupRequest) =>
    requests.removeMember({ url: api.modelGroupMember(groupId), data }, true),
  reorderMembers: (groupId: string, data: IReorderModelGroupMembersRequest) =>
    requests.reorderMembers({ url: api.modelGroupOrder(groupId), data }, true),
};

export default modelGroupService;
