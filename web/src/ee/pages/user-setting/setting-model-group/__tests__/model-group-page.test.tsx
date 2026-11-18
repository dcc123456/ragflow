import { TooltipProvider } from '@/components/ui/tooltip';
import { fireEvent, render, screen } from '@testing-library/react';

import ModelGroupPage from '..';

const mockList = jest.fn();

jest.mock('@/ee/hooks/use-model-group-request', () => ({
  useModelGroupList: () => mockList(),
  useModelGroupMembers: () => ({
    data: [],
    loading: false,
    isError: false,
    refetch: jest.fn(),
  }),
  useCreateModelGroup: () => ({ loading: false, run: jest.fn() }),
  useDeleteModelGroup: () => ({ loading: false, run: jest.fn() }),
  useAddModelGroupMember: () => ({ loading: false, run: jest.fn() }),
  useRemoveModelGroupMember: () => ({ loading: false, run: jest.fn() }),
}));

jest.mock('@/components/model-tree-select', () => ({
  ModelTreeSelect: () => null,
  DirectModelTreeSelect: () => null,
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

describe('ModelGroupPage', () => {
  beforeEach(() => {
    mockList.mockReset();
  });

  it('renders the empty state when there are no model groups', () => {
    mockList.mockReturnValue({
      data: [],
      loading: false,
      isError: false,
      refetch: jest.fn(),
    });

    render(<ModelGroupPage />);

    expect(
      screen.getByText('setting.modelGroupPage.empty'),
    ).toBeInTheDocument();
  });

  it('retries only the failed model group list request', () => {
    const refetch = jest.fn();
    mockList.mockReturnValue({
      data: [],
      loading: false,
      isError: true,
      refetch,
    });

    render(<ModelGroupPage />);
    fireEvent.click(
      screen.getByRole('button', {
        name: 'setting.modelGroupPage.retry',
      }),
    );

    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('fills the available flex space in the populated state', () => {
    mockList.mockReturnValue({
      data: [
        {
          id: 'g1',
          tenant_id: 't1',
          group_name: 'release',
          status: 'active',
        },
      ],
      loading: false,
      isError: false,
      refetch: jest.fn(),
    });

    const { container } = render(
      <TooltipProvider>
        <ModelGroupPage />
      </TooltipProvider>,
    );

    expect(container.querySelector('section')).toHaveClass(
      'w-full',
      'flex-1',
      'min-w-0',
      'min-h-0',
    );
    expect(container.querySelector('header')).toHaveClass('shrink-0');
  });
});
