import { renderHook } from '@testing-library/react';
import { useVisibleIntegrationChildren } from '../use-visible-integration-children';

const mockUseFetchTenantInfo = jest.fn();
const mockUseTenantMyPermissions = jest.fn();

jest.mock('@/hooks/use-user-setting-request', () => ({
  useFetchTenantInfo: () => mockUseFetchTenantInfo(),
}));

jest.mock('@/ee/pages/user-setting/team/use-member-role-query', () => ({
  useTenantMyPermissions: (tenantId: string) =>
    mockUseTenantMyPermissions(tenantId),
}));

const children = [
  { key: 'model', path: '/integrations/model', labelKey: 'sidebar.models' },
  {
    key: 'data-source',
    path: '/integrations/data-source',
    labelKey: 'setting.dataSources',
  },
];

function mockModelPermission(enable: boolean) {
  mockUseTenantMyPermissions.mockReturnValue({
    data: { permissions: { model_provider: { enable } } },
  });
}

describe('useVisibleIntegrationChildren', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockUseFetchTenantInfo.mockReturnValue({
      data: { tenant_id: 'tenant-1' },
    });
    mockUseTenantMyPermissions.mockReturnValue({ data: undefined });
  });

  it('adds Model Group below Model when model provider access is granted', () => {
    mockModelPermission(true);

    const { result } = renderHook(() =>
      useVisibleIntegrationChildren(children),
    );

    expect(result.current.map((child) => child.key)).toEqual([
      'model',
      'model-group',
      'data-source',
    ]);
    expect(mockUseTenantMyPermissions).toHaveBeenCalledWith('tenant-1');
  });

  it('returns the original navigation when model provider access is denied', () => {
    mockModelPermission(false);

    const { result } = renderHook(() =>
      useVisibleIntegrationChildren(children),
    );

    expect(result.current).toBe(children);
  });

  it('does not add Model Group when the permission field is missing', () => {
    mockUseTenantMyPermissions.mockReturnValue({
      data: { permissions: {} },
    });

    const { result } = renderHook(() =>
      useVisibleIntegrationChildren(children),
    );

    expect(result.current.map((child) => child.key)).not.toContain(
      'model-group',
    );
  });

  it('does not add Model Group while permissions are loading', () => {
    const { result } = renderHook(() =>
      useVisibleIntegrationChildren(children),
    );

    expect(result.current.map((child) => child.key)).not.toContain(
      'model-group',
    );
  });

  it('does not add Model Group when the permission request fails', () => {
    mockUseTenantMyPermissions.mockReturnValue({
      data: undefined,
      isError: true,
    });

    const { result } = renderHook(() =>
      useVisibleIntegrationChildren(children),
    );

    expect(result.current.map((child) => child.key)).not.toContain(
      'model-group',
    );
  });
});
