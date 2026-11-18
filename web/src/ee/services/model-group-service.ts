import {
  IAddModelToGroupRequest,
  ICreateModelGroupRequest,
  IRemoveModelFromGroupRequest,
} from '@/ee/interfaces/request/model-group';
import api from '@/utils/api';
import { registerNextServer } from '@/utils/register-server';

const methods = {
  create: { url: api.modelGroups, method: 'post' },
  list: { url: api.modelGroups, method: 'get' },
  members: { url: api.modelGroup('__group_name__'), method: 'get' },
  remove: { url: api.modelGroup('__group_name__'), method: 'delete' },
  addMember: { url: api.modelGroupMember('__group_name__'), method: 'post' },
  removeMember: {
    url: api.modelGroupMember('__group_name__'),
    method: 'delete',
  },
} as const;

const requests = registerNextServer<keyof typeof methods>(methods);

const modelGroupService = {
  create: (data: ICreateModelGroupRequest) => requests.create({ data }, true),
  list: () => requests.list({ params: {} }, true),
  members: (groupName: string) =>
    requests.members({ url: api.modelGroup(groupName) }, true),
  remove: (groupName: string) =>
    requests.remove({ url: api.modelGroup(groupName) }, true),
  addMember: (groupName: string, data: IAddModelToGroupRequest) =>
    requests.addMember({ url: api.modelGroupMember(groupName), data }, true),
  removeMember: (groupName: string, data: IRemoveModelFromGroupRequest) =>
    requests.removeMember({ url: api.modelGroupMember(groupName), data }, true),
};

export default modelGroupService;
