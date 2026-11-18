import { render, screen } from '@testing-library/react';
import { ModelReferenceLabel } from './model-reference-label';

const mockUseModelGroupList = jest.fn();
const mockLLMLabel = jest.fn();
const mockUseFetchTenantInfo = jest.fn();

jest.mock('@/ee/hooks/use-model-group-request', () => ({
  useModelGroupList: (enabled: boolean) => mockUseModelGroupList(enabled),
}));

jest.mock('@/hooks/use-user-setting-request', () => ({
  useFetchTenantInfo: (enabled?: boolean) => mockUseFetchTenantInfo(enabled),
}));

jest.mock('@/components/llm-select/llm-label', () => ({
  __esModule: true,
  default: (props: { value?: string; ownerTenantId?: string }) =>
    mockLLMLabel(props),
}));

const groupId = '0123456789abcdef0123456789abcdef';
const groupRef = `gp-${groupId}`;

describe('ModelReferenceLabel', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockUseFetchTenantInfo.mockReturnValue({
      data: { tenant_id: 'tenant-1' },
      loading: false,
      isFetched: true,
    });
    mockUseModelGroupList.mockReturnValue({
      data: [
        {
          id: groupId,
          tenant_id: 'tenant-1',
          group_name: 'Support chat group',
          status: 'active',
        },
      ],
    });
    mockLLMLabel.mockImplementation(({ value, ownerTenantId }) => (
      <span data-testid="direct-label" data-owner={ownerTenantId}>
        {value}
      </span>
    ));
  });

  it('resolves a group reference to its group name', () => {
    render(<ModelReferenceLabel value={groupRef} />);

    const label = screen.getByText('Support chat group');
    expect(label).toHaveAttribute('title', 'Support chat group');
    expect(label).toHaveClass('block', 'min-w-0', 'truncate', 'font-medium');
    expect(mockUseModelGroupList).toHaveBeenCalledWith(true);
    expect(mockLLMLabel).not.toHaveBeenCalled();
  });

  it('falls back to the raw group reference when unresolved', () => {
    mockUseModelGroupList.mockReturnValue({ data: [] });
    render(<ModelReferenceLabel value={groupRef} />);

    expect(screen.getByText(groupRef)).toHaveAttribute('title', groupRef);
    expect(mockUseModelGroupList).toHaveBeenCalledWith(true);
  });

  it('delegates direct models without enabling the group list', () => {
    render(<ModelReferenceLabel value="model-1" />);

    expect(screen.getByTestId('direct-label')).toHaveTextContent('model-1');
    expect(mockUseModelGroupList).toHaveBeenCalledWith(false);
    expect(mockLLMLabel.mock.calls[0][0]).toEqual({
      value: 'model-1',
      ownerTenantId: undefined,
    });
  });

  it('resolves a group reference owned by the active tenant', () => {
    render(<ModelReferenceLabel value={groupRef} ownerTenantId="tenant-1" />);

    expect(screen.getByText('Support chat group')).toHaveAttribute(
      'title',
      'Support chat group',
    );
    expect(mockUseModelGroupList).toHaveBeenCalledWith(true);
    expect(mockLLMLabel).not.toHaveBeenCalled();
  });

  it('treats a group-shaped cross-tenant model as a direct reference', () => {
    render(<ModelReferenceLabel value={groupRef} ownerTenantId="owner-1" />);

    expect(screen.getByTestId('direct-label')).toHaveTextContent(groupRef);
    expect(screen.getByTestId('direct-label')).toHaveAttribute(
      'data-owner',
      'owner-1',
    );
    expect(mockUseModelGroupList).toHaveBeenCalledWith(false);
  });
});
