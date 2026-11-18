import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useModelGroupOptions } from '../use-model-group-options';

const mockList = jest.fn();
const mockMembers = jest.fn();
jest.mock('@/ee/services/model-group-service', () => ({
  __esModule: true,
  default: {
    list: () => mockList(),
    members: (groupName: string) => mockMembers(groupName),
  },
}));
const goodGroup = {
  id: '0123456789abcdef0123456789abcdef',
  tenant_id: 'tenant-1',
  group_name: 'good',
  status: 'active',
};
const brokenGroup = {
  ...goodGroup,
  id: 'ffffffffffffffffffffffffffffffff',
  group_name: 'broken',
};
const member = {
  id: 'map-1',
  model_id: 'model-1',
  position: 0,
  provider: 'OpenAI',
  instance: 'default',
  model: 'gpt-4',
  model_type: ['chat'],
  status: 'active',
  group_name: 'good',
  group_id: goodGroup.id,
};
const directModel = {
  model_id: 'model-1',
  status: 'active',
  instance_status: 'active',
  model_type: ['chat'],
  name: 'gpt-4',
  provider_id: 'provider-1',
  provider_name: 'OpenAI',
  instance_id: 'instance-1',
  instance_name: 'default',
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
describe('useModelGroupOptions', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockList.mockResolvedValue({
      data: { code: 0, data: [goodGroup, brokenGroup] },
    });
    mockMembers.mockImplementation((groupName: string) => {
      if (groupName === 'broken') return Promise.reject(new Error('failed'));
      return Promise.resolve({ data: { code: 0, data: [member] } });
    });
  });
  it('fails closed per group and freshly resolves gp- valid ids', async () => {
    const { wrapper, queryClient } = createWrapper();
    const { result } = renderHook(
      () => useModelGroupOptions(['chat'], [directModel]),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isFetched).toBe(true));
    expect(result.current.compatibleGroupIds).toEqual(new Set([goodGroup.id]));
    expect(mockList).toHaveBeenCalledTimes(1);
    expect(mockMembers).toHaveBeenCalledTimes(2);

    mockMembers.mockImplementation((groupName: string) => {
      if (groupName === brokenGroup.group_name) {
        return Promise.reject(new Error('failed'));
      }
      return Promise.resolve({
        data: { code: 0, data: [{ ...member, status: 'inactive' }] },
      });
    });

    await act(async () => {
      await expect(
        result.current.resolveValidIds([directModel]),
      ).resolves.toEqual(new Set());
    });
    expect(mockList).toHaveBeenCalledTimes(2);
    expect(mockMembers).toHaveBeenCalledTimes(4);
    expect(
      mockMembers.mock.calls.filter(
        ([groupName]) => groupName === goodGroup.group_name,
      ),
    ).toHaveLength(2);
    expect(
      mockMembers.mock.calls.filter(
        ([groupName]) => groupName === brokenGroup.group_name,
      ),
    ).toHaveLength(2);
    queryClient.clear();
  });
  it('fails closed reactively when a background list refetch fails', async () => {
    const { wrapper, queryClient } = createWrapper();
    const { result } = renderHook(
      () => useModelGroupOptions(['chat'], [directModel]),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isFetched).toBe(true));
    expect(result.current.compatibleGroupIds).toEqual(new Set([goodGroup.id]));

    mockList.mockRejectedValueOnce(new Error('list failed'));
    await act(async () => {
      await result.current.refetch();
    });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.groups).toEqual([goodGroup, brokenGroup]);
    expect(result.current.isFetched).toBe(true);
    expect(result.current.compatibleGroupIds).toEqual(new Set());
    expect(mockList).toHaveBeenCalledTimes(2);
    queryClient.clear();
  });
  it('does not request groups when disabled', () => {
    const { wrapper, queryClient } = createWrapper();
    renderHook(() => useModelGroupOptions(['chat'], [directModel], false), {
      wrapper,
    });
    expect(mockList).not.toHaveBeenCalled();
    expect(mockMembers).not.toHaveBeenCalled();
    queryClient.clear();
  });
});
