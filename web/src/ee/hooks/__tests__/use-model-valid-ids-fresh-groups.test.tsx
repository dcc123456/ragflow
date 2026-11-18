import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useModelValidIds } from '../../../hooks/use-llm-request';

const mockListAllAddedModels = jest.fn();
const mockListModelGroups = jest.fn();
const mockListModelGroupMembers = jest.fn();
const mockGetTenantInfo = jest.fn();

jest.mock('@/services/llm-service', () => ({
  __esModule: true,
  default: {
    listAllAddedModels: (...args: unknown[]) => mockListAllAddedModels(...args),
  },
}));

jest.mock('@/ee/services/model-group-service', () => ({
  __esModule: true,
  default: {
    list: () => mockListModelGroups(),
    members: (groupName: string) => mockListModelGroupMembers(groupName),
  },
}));

jest.mock('@/services/user-service', () => ({
  __esModule: true,
  default: {
    getTenantInfo: () => mockGetTenantInfo(),
  },
}));

jest.mock('react-i18next', () => ({
  ...jest.requireActual('react-i18next'),
  useTranslation: () => ({ t: (key: string) => key }),
}));

const groupId = '0123456789abcdef0123456789abcdef';
const groupRef = `gp-${groupId}`;
const freshDirectModel = {
  model_id: 'model-2',
  status: 'active',
  instance_status: 'active',
  model_type: ['chat'],
  name: 'gpt-4o',
  provider_id: 'provider-1',
  provider_name: 'OpenAI',
  instance_id: 'instance-1',
  instance_name: 'default',
};
const activeGroup = {
  id: groupId,
  tenant_id: 'tenant-1',
  group_name: 'chat-group',
  status: 'active',
};
const activeGroupMember = {
  id: 'mapping-1',
  model_id: freshDirectModel.model_id,
  position: 0,
  provider: freshDirectModel.provider_name,
  instance: freshDirectModel.instance_name,
  model: freshDirectModel.name,
  model_type: ['chat'],
  status: 'active',
  group_name: activeGroup.group_name,
  group_id: activeGroup.id,
};

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return {
    queryClient,
    wrapper: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  };
}

describe('useModelValidIds enterprise groups', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockGetTenantInfo.mockResolvedValue({
      data: { code: 0, data: { tenant_id: 'tenant-1' } },
    });
  });

  it('freshly resolves real enterprise groups when called before the first direct fetch settles', async () => {
    mockListAllAddedModels
      .mockImplementationOnce(() => new Promise(() => {}))
      .mockResolvedValueOnce({
        data: { code: 0, data: [freshDirectModel] },
      });
    mockListModelGroups.mockResolvedValue({
      data: { code: 0, data: [activeGroup] },
    });
    mockListModelGroupMembers.mockResolvedValue({
      data: { code: 0, data: [activeGroupMember] },
    });
    const { wrapper, queryClient } = createWrapper();
    const { result } = renderHook(() => useModelValidIds(['chat']), {
      wrapper,
    });

    expect(result.current.isFetched).toBe(false);
    expect(mockListModelGroups).not.toHaveBeenCalled();
    expect(mockListModelGroupMembers).not.toHaveBeenCalled();

    await act(async () => {
      await expect(result.current.resolveValidIds()).resolves.toEqual(
        new Set(['model-2', 'gpt-4o@default@OpenAI', groupRef]),
      );
    });
    expect(mockListAllAddedModels).toHaveBeenCalledTimes(2);
    expect(mockListModelGroups).toHaveBeenCalled();
    expect(mockListModelGroupMembers).toHaveBeenCalledWith(
      activeGroup.group_name,
    );
    expect(mockGetTenantInfo).not.toHaveBeenCalled();
    queryClient.clear();
  });

  it('keeps same-tenant validation pending until tenant access and groups resolve', async () => {
    let resolveTenant!: (response: unknown) => void;
    mockGetTenantInfo.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveTenant = resolve;
        }),
    );
    mockListAllAddedModels.mockResolvedValue({
      data: { code: 0, data: [freshDirectModel] },
    });
    mockListModelGroups.mockResolvedValue({
      data: { code: 0, data: [activeGroup] },
    });
    mockListModelGroupMembers.mockResolvedValue({
      data: { code: 0, data: [activeGroupMember] },
    });
    const { wrapper, queryClient } = createWrapper();
    const { result } = renderHook(
      () => useModelValidIds(['chat'], 'tenant-1'),
      { wrapper },
    );

    await waitFor(() => expect(result.current.isFetched).toBe(true));
    expect(result.current.isSettled).toBe(false);
    expect(result.current.validIds).toEqual(
      new Set(['model-2', 'gpt-4o@default@OpenAI']),
    );
    expect(result.current.getPendingMessage(groupRef)).toBe(
      'setting.modelGroupSelector.pending',
    );
    expect(mockListModelGroups).not.toHaveBeenCalled();

    await act(async () => {
      resolveTenant({
        data: { code: 0, data: { tenant_id: 'tenant-1' } },
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.isSettled).toBe(true);
      expect(result.current.validIds).toEqual(
        new Set(['model-2', 'gpt-4o@default@OpenAI', groupRef]),
      );
    });
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();
    expect(mockListModelGroups).toHaveBeenCalled();
    queryClient.clear();
  });

  it('keeps cross-tenant validation direct-only without group queries', async () => {
    mockListAllAddedModels.mockResolvedValue({
      data: { code: 0, data: [freshDirectModel] },
    });
    const { wrapper, queryClient } = createWrapper();
    const { result } = renderHook(() => useModelValidIds(['chat'], 'owner-1'), {
      wrapper,
    });

    await waitFor(() => {
      expect(result.current.isFetched).toBe(true);
      expect(result.current.isSettled).toBe(true);
    });
    expect(result.current.validIds).toEqual(
      new Set(['model-2', 'gpt-4o@default@OpenAI']),
    );
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();
    expect(mockGetTenantInfo).toHaveBeenCalledTimes(1);
    expect(mockListModelGroups).not.toHaveBeenCalled();
    expect(mockListModelGroupMembers).not.toHaveBeenCalled();
    queryClient.clear();
  });

  it('settles direct-only when the active tenant query fails', async () => {
    mockGetTenantInfo.mockRejectedValue(new Error('tenant lookup failed'));
    mockListAllAddedModels.mockResolvedValue({
      data: { code: 0, data: [freshDirectModel] },
    });
    const { wrapper, queryClient } = createWrapper();
    const { result } = renderHook(
      () => useModelValidIds(['chat'], 'tenant-1'),
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.isFetched).toBe(true);
      expect(result.current.isSettled).toBe(true);
    });
    expect(result.current.validIds).toEqual(
      new Set(['model-2', 'gpt-4o@default@OpenAI']),
    );
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();
    expect(mockListModelGroups).not.toHaveBeenCalled();
    expect(mockListModelGroupMembers).not.toHaveBeenCalled();
    queryClient.clear();
  });

  it('freshly resolves same-tenant groups when tenant and direct queries are initially pending', async () => {
    mockListAllAddedModels
      .mockImplementationOnce(() => new Promise(() => {}))
      .mockResolvedValueOnce({
        data: { code: 0, data: [freshDirectModel] },
      });
    mockGetTenantInfo
      .mockImplementationOnce(() => new Promise(() => {}))
      .mockResolvedValueOnce({
        data: { code: 0, data: { tenant_id: 'tenant-1' } },
      });
    mockListModelGroups.mockResolvedValue({
      data: { code: 0, data: [activeGroup] },
    });
    mockListModelGroupMembers.mockResolvedValue({
      data: { code: 0, data: [activeGroupMember] },
    });
    const { wrapper, queryClient } = createWrapper();
    const { result } = renderHook(
      () => useModelValidIds(['chat'], 'tenant-1'),
      { wrapper },
    );

    expect(result.current.isFetched).toBe(false);
    expect(mockListModelGroups).not.toHaveBeenCalled();

    await act(async () => {
      await expect(result.current.resolveValidIds()).resolves.toEqual(
        new Set(['model-2', 'gpt-4o@default@OpenAI', groupRef]),
      );
    });
    expect(mockListAllAddedModels).toHaveBeenCalledTimes(2);
    expect(mockGetTenantInfo).toHaveBeenCalledTimes(2);
    expect(mockListModelGroups).toHaveBeenCalled();
    expect(mockListModelGroupMembers).toHaveBeenCalledWith(
      activeGroup.group_name,
    );
    queryClient.clear();
  });
});
