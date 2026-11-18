const mockRequests = {
  create: jest.fn(),
  list: jest.fn(),
  members: jest.fn(),
  remove: jest.fn(),
  addMember: jest.fn(),
  removeMember: jest.fn(),
};
const mockRegisterNextServer = jest.fn((_methods: unknown) => {
  void _methods;
  return mockRequests;
});

jest.mock('@/utils/register-server', () => ({
  registerNextServer: (methods: unknown) => mockRegisterNextServer(methods),
}));

import api from '@/utils/api';

const modelGroupService = jest.requireActual<
  typeof import('../model-group-service')
>('../model-group-service').default;

describe('modelGroupService', () => {
  beforeEach(() => jest.clearAllMocks());

  it('encodes every dynamic group name', () => {
    expect(api.modelGroup('a/b ?')).toBe('/api/v1/models/groups/a%2Fb%20%3F');
    expect(api.modelGroupMember('a/b ?')).toBe(
      '/api/v1/models/groups/a%2Fb%20%3F/model',
    );
  });

  it('uses model_id when adding a member', () => {
    modelGroupService.addMember('team one', { model_id: 'model-1' });

    expect(mockRequests.addMember).toHaveBeenCalledWith(
      {
        url: '/api/v1/models/groups/team%20one/model',
        data: { model_id: 'model-1' },
      },
      true,
    );
  });

  it('uses composite_model_name when removing a member', () => {
    const body = { composite_model_name: 'chat@default@OpenAI' };
    modelGroupService.removeMember('team one', body);

    expect(mockRequests.removeMember).toHaveBeenCalledWith(
      {
        url: '/api/v1/models/groups/team%20one/model',
        data: body,
      },
      true,
    );
  });
});
