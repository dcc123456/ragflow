import { act, renderHook } from '@testing-library/react';
import { useAdditionalModelValidIds } from '../use-additional-model-valid-ids';

const mockUseModelGroupOptions = jest.fn();
const mockResolveGroupValidIds = jest.fn();
const mockUseFetchTenantInfo = jest.fn();
const mockRefetchTenantInfo = jest.fn();
const mockT = jest.fn((key: string) => key);

jest.mock('@/ee/hooks/use-model-group-options', () => ({
  useModelGroupOptions: (...args: unknown[]) =>
    mockUseModelGroupOptions(...args),
}));

jest.mock('@/hooks/use-user-setting-request', () => ({
  useFetchTenantInfo: (enabled?: boolean) => mockUseFetchTenantInfo(enabled),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT }),
}));

const groupId = '0123456789abcdef0123456789abcdef';
const groupRef = `gp-${groupId}`;
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

function mockGroupOptions({
  compatible = false,
  isFetched = false,
}: { compatible?: boolean; isFetched?: boolean } = {}) {
  mockUseModelGroupOptions.mockReturnValue({
    compatibleGroupIds: compatible ? new Set([groupId]) : new Set(),
    isFetched,
    resolveValidIds: mockResolveGroupValidIds,
  });
}

describe('useAdditionalModelValidIds', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockGroupOptions();
    mockResolveGroupValidIds.mockResolvedValue(new Set([groupRef]));
    mockRefetchTenantInfo.mockResolvedValue({
      data: { tenant_id: 'tenant-1' },
      isError: false,
    });
    mockUseFetchTenantInfo.mockReturnValue({
      data: { tenant_id: 'tenant-1' },
      loading: false,
      isFetched: true,
      refetch: mockRefetchTenantInfo,
    });
  });

  it('gates group work on direct readiness and settles only after groups load', () => {
    const { result, rerender } = renderHook(
      ({ modelsFetched }) =>
        useAdditionalModelValidIds({
          modelTypes: ['chat'],
          models: [directModel],
          modelsFetched,
          enabled: true,
        }),
      { initialProps: { modelsFetched: false } },
    );

    expect(mockUseModelGroupOptions).toHaveBeenLastCalledWith(
      ['chat'],
      [directModel],
      false,
    );
    expect(mockUseFetchTenantInfo).toHaveBeenLastCalledWith(false);
    expect(result.current.isSettled).toBe(false);
    expect(result.current.getPendingMessage(groupRef)).toBe(
      'setting.modelGroupSelector.pending',
    );
    expect(result.current.getPendingMessage('model-1')).toBeUndefined();
    const pendingMessage = result.current.getPendingMessage;
    rerender({ modelsFetched: false });
    expect(result.current.getPendingMessage).toBe(pendingMessage);

    mockGroupOptions({ isFetched: false });
    rerender({ modelsFetched: true });
    expect(mockUseModelGroupOptions).toHaveBeenLastCalledWith(
      ['chat'],
      [directModel],
      true,
    );
    expect(result.current.isSettled).toBe(false);
    expect(result.current.getPendingMessage(groupRef)).toBe(
      'setting.modelGroupSelector.pending',
    );

    mockGroupOptions({ compatible: true, isFetched: true });
    rerender({ modelsFetched: true });
    expect(result.current.isSettled).toBe(true);
    expect(result.current.validIds).toEqual(new Set([groupRef]));
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();
  });

  it('opens same-tenant groups only after tenant access settles and closes them cross-tenant', () => {
    mockGroupOptions({ compatible: true, isFetched: true });
    mockUseFetchTenantInfo.mockReturnValue({
      data: {},
      loading: true,
      isFetched: false,
      refetch: mockRefetchTenantInfo,
    });
    const { result, rerender } = renderHook(
      ({ ownerTenantId }) =>
        useAdditionalModelValidIds({
          modelTypes: ['chat'],
          models: [directModel],
          modelsFetched: true,
          enabled: true,
          ownerTenantId,
        }),
      { initialProps: { ownerTenantId: 'tenant-1' } },
    );

    expect(mockUseModelGroupOptions).toHaveBeenLastCalledWith(
      ['chat'],
      [directModel],
      false,
    );
    expect(result.current.validIds).toEqual(new Set());
    expect(result.current.isSettled).toBe(false);
    expect(result.current.getPendingMessage(groupRef)).toBe(
      'setting.modelGroupSelector.pending',
    );

    mockUseFetchTenantInfo.mockReturnValue({
      data: { tenant_id: 'tenant-1' },
      loading: false,
      isFetched: true,
      refetch: mockRefetchTenantInfo,
    });
    rerender({ ownerTenantId: 'tenant-1' });
    expect(mockUseModelGroupOptions).toHaveBeenLastCalledWith(
      ['chat'],
      [directModel],
      true,
    );
    expect(result.current.validIds).toEqual(new Set([groupRef]));
    expect(result.current.isSettled).toBe(true);
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();

    rerender({ ownerTenantId: 'owner-1' });
    expect(mockUseModelGroupOptions).toHaveBeenLastCalledWith(
      ['chat'],
      [directModel],
      false,
    );
    expect(result.current.validIds).toEqual(new Set());
    expect(result.current.isSettled).toBe(true);
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();
  });

  it('closes retained same-tenant groups after a failed tenant refresh', () => {
    mockGroupOptions({ compatible: true, isFetched: true });
    mockUseFetchTenantInfo.mockReturnValue({
      data: { tenant_id: 'tenant-1' },
      loading: false,
      isFetched: true,
      isError: true,
      refetch: mockRefetchTenantInfo,
    });
    const { result } = renderHook(() =>
      useAdditionalModelValidIds({
        modelTypes: ['chat'],
        models: [directModel],
        modelsFetched: true,
        enabled: true,
        ownerTenantId: 'tenant-1',
      }),
    );

    expect(mockUseModelGroupOptions).toHaveBeenLastCalledWith(
      ['chat'],
      [directModel],
      false,
    );
    expect(result.current.validIds).toEqual(new Set());
    expect(result.current.isSettled).toBe(true);
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();
  });

  it('imperatively resolves tenant access before fresh groups and fails closed cross-tenant or on failure', async () => {
    mockUseFetchTenantInfo.mockReturnValue({
      data: {},
      loading: true,
      isFetched: false,
      refetch: mockRefetchTenantInfo,
    });
    const { result, rerender } = renderHook(
      ({ ownerTenantId }) =>
        useAdditionalModelValidIds({
          modelTypes: ['chat'],
          models: [directModel],
          modelsFetched: true,
          enabled: true,
          ownerTenantId,
        }),
      { initialProps: { ownerTenantId: 'tenant-1' } },
    );

    await act(async () => {
      await expect(
        result.current.resolveValidIds([directModel]),
      ).resolves.toEqual(new Set([groupRef]));
    });
    expect(mockRefetchTenantInfo).toHaveBeenCalledTimes(1);
    expect(mockResolveGroupValidIds).toHaveBeenCalledTimes(1);

    rerender({ ownerTenantId: 'owner-1' });
    await act(async () => {
      await expect(
        result.current.resolveValidIds([directModel]),
      ).resolves.toEqual(new Set());
    });
    expect(mockRefetchTenantInfo).toHaveBeenCalledTimes(2);
    expect(mockResolveGroupValidIds).toHaveBeenCalledTimes(1);

    mockRefetchTenantInfo.mockResolvedValue({
      data: {},
      isError: true,
    });
    rerender({ ownerTenantId: 'tenant-1' });
    await act(async () => {
      await expect(
        result.current.resolveValidIds([directModel]),
      ).resolves.toEqual(new Set());
    });
    expect(mockRefetchTenantInfo).toHaveBeenCalledTimes(3);
    expect(mockResolveGroupValidIds).toHaveBeenCalledTimes(1);
  });

  it('freshly resolves enabled groups and hides stale ids when disabled', async () => {
    mockGroupOptions({ compatible: true, isFetched: true });
    const { result, rerender } = renderHook(
      ({ enabled }) =>
        useAdditionalModelValidIds({
          modelTypes: ['chat'],
          models: [directModel],
          modelsFetched: true,
          enabled,
        }),
      { initialProps: { enabled: true } },
    );

    await act(async () => {
      await expect(
        result.current.resolveValidIds([directModel]),
      ).resolves.toEqual(new Set([groupRef]));
    });
    expect(mockResolveGroupValidIds).toHaveBeenCalledWith([directModel]);
    expect(mockRefetchTenantInfo).not.toHaveBeenCalled();

    rerender({ enabled: false });
    expect(mockUseModelGroupOptions).toHaveBeenLastCalledWith(
      ['chat'],
      [directModel],
      false,
    );
    expect(result.current.isSettled).toBe(true);
    expect(result.current.validIds).toEqual(new Set());
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();

    await act(async () => {
      await expect(
        result.current.resolveValidIds([directModel]),
      ).resolves.toEqual(new Set());
    });
    expect(mockResolveGroupValidIds).toHaveBeenCalledTimes(1);
  });

  it('follows direct readiness while disabled without exposing group state', () => {
    mockGroupOptions({ compatible: true, isFetched: false });
    const { result, rerender } = renderHook(
      ({ modelsFetched }) =>
        useAdditionalModelValidIds({
          modelTypes: ['chat'],
          models: [directModel],
          modelsFetched,
          enabled: false,
          ownerTenantId: 'tenant-1',
        }),
      { initialProps: { modelsFetched: false } },
    );

    expect(mockUseFetchTenantInfo).toHaveBeenLastCalledWith(false);
    expect(result.current.isSettled).toBe(false);
    expect(result.current.validIds).toEqual(new Set());
    expect(result.current.getPendingMessage(groupRef)).toBeUndefined();

    rerender({ modelsFetched: true });
    expect(result.current.isSettled).toBe(true);
    expect(result.current.validIds).toEqual(new Set());
    expect(mockUseModelGroupOptions).toHaveBeenLastCalledWith(
      ['chat'],
      [directModel],
      false,
    );
  });
});
